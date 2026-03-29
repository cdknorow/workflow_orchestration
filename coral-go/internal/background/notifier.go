package background

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/cdknorow/coral/internal/board"
	"github.com/cdknorow/coral/internal/naming"
)

// BoardNotifier nudges agents when they have unread board messages.
type BoardNotifier struct {
	boardStore  *board.Store
	runtime     AgentRuntime
	interval    time.Duration
	logger      *slog.Logger
	discoverFn  func(ctx context.Context) ([]AgentInfo, error)
	isPausedFn  func(project string) bool
	notifiedMu  sync.Mutex
	notified    map[string]int // session_id -> unread count at last notification
	notifyNowCh chan struct{}
}

// NewBoardNotifier creates a new BoardNotifier.
func NewBoardNotifier(boardStore *board.Store, runtime AgentRuntime, interval time.Duration) *BoardNotifier {
	return &BoardNotifier{
		boardStore:  boardStore,
		runtime:     runtime,
		interval:    interval,
		logger:      slog.Default().With("service", "board_notifier"),
		notified:    make(map[string]int),
		notifyNowCh: make(chan struct{}, 1),
	}
}

// SetDiscoverFn sets a custom agent discovery function.
func (n *BoardNotifier) SetDiscoverFn(fn func(ctx context.Context) ([]AgentInfo, error)) {
	n.discoverFn = fn
}

// SetIsPausedFn sets a function to check if a board project is paused.
func (n *BoardNotifier) SetIsPausedFn(fn func(project string) bool) {
	n.isPausedFn = fn
}

// SeedFromDB populates the notified map with current unread counts from the database.
// Call once during startup before Run() to avoid re-notifying agents about
// pre-existing unread messages after a server restart.
func (n *BoardNotifier) SeedFromDB(ctx context.Context) {
	counts, err := n.boardStore.GetAllUnreadCounts(ctx)
	if err != nil {
		n.logger.Warn("failed to seed notifier state", "error", err)
		return
	}
	n.notifiedMu.Lock()
	defer n.notifiedMu.Unlock()
	for subscriberID, count := range counts {
		n.notified[subscriberID] = count
	}
	n.logger.Info("seeded notifier from DB", "subscribers", len(counts))
}

// NotifyNow triggers an immediate notification pass without waiting for the next tick.
// Safe to call from any goroutine. Non-blocking if a notification is already pending.
func (n *BoardNotifier) NotifyNow() {
	select {
	case n.notifyNowCh <- struct{}{}:
	default:
		// Already a pending notification, skip
	}
}

// Run starts the notification loop.
func (n *BoardNotifier) Run(ctx context.Context) error {
	ticker := time.NewTicker(n.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := n.RunOnce(ctx); err != nil {
				n.logger.Error("notification error", "error", err)
			}
		case <-n.notifyNowCh:
			if err := n.RunOnce(ctx); err != nil {
				n.logger.Error("notification error (event-driven)", "error", err)
			}
		}
	}
}

// RunOnce performs a single notification pass.
func (n *BoardNotifier) RunOnce(ctx context.Context) error {
	agents, err := n.discoverFn(ctx)
	if err != nil {
		n.logger.Error("discover agents failed", "error", err)
		return err
	}

	n.logger.Info("notification pass", "agent_count", len(agents))

	liveBoardIDs := make(map[string]bool)

	for _, a := range agents {
		if a.SessionID == "" {
			n.logger.Info("skipping agent with no session ID", "agent", a.AgentName)
			continue
		}

		subscriberID := naming.SubscriberID(a.DisplayName, a.AgentType)
		liveBoardIDs[subscriberID] = true

		// Look up by session name first (precise match for current session),
		// fall back to subscriber_id (for backwards compatibility).
		sessName := naming.SessionName(a.AgentType, a.SessionID)
		sub, err := n.boardStore.GetSubscriptionBySessionName(ctx, sessName)
		if err != nil || sub == nil {
			sub, err = n.boardStore.GetSubscription(ctx, subscriberID)
		}
		if err != nil || sub == nil {
			n.logger.Info("no subscription found", "subscriber_id", subscriberID, "session_name", sessName, "error", err)
			continue
		}
		// Skip remote subscribers
		if sub.OriginServer != nil && *sub.OriginServer != "" {
			n.logger.Info("skipping remote subscriber", "subscriber_id", subscriberID)
			continue
		}
		// Skip agents on paused/sleeping boards
		if n.isPausedFn != nil && sub.Project != "" && n.isPausedFn(sub.Project) {
			n.logger.Info("skipping paused board", "subscriber_id", subscriberID, "project", sub.Project)
			continue
		}

		unread, err := n.boardStore.CheckUnread(ctx, sub.Project, subscriberID)
		if err != nil {
			n.logger.Info("check unread failed", "subscriber_id", subscriberID, "error", err)
			continue
		}

		n.logger.Info("unread check", "subscriber_id", subscriberID, "unread", unread, "project", sub.Project)

		if unread == 0 {
			n.notifiedMu.Lock()
			delete(n.notified, subscriberID)
			n.notifiedMu.Unlock()
			continue
		}

		n.notifiedMu.Lock()
		alreadyNotified := n.notified[subscriberID] == unread
		n.notifiedMu.Unlock()
		if alreadyNotified {
			n.logger.Info("already notified", "subscriber_id", subscriberID, "unread", unread)
			continue
		}

		// Send nudge via tmux session name (routing identity, not board identity)
		plural := "s"
		if unread == 1 {
			plural = ""
		}
		nudge := fmt.Sprintf("You have %d unread message%s on the message board. Run 'coral-board read' to see them.", unread, plural)
		n.logger.Info("sending nudge", "subscriber_id", subscriberID, "session", sessName, "unread", unread)
		err = n.runtime.SendInput(ctx, sessName, nudge)
		if err != nil {
			n.logger.Warn("failed to nudge agent", "agent", a.AgentName, "session", sessName, "error", err)
			continue
		}

		n.notifiedMu.Lock()
		n.notified[subscriberID] = unread
		n.notifiedMu.Unlock()
	}

	// Clean up stale entries
	n.notifiedMu.Lock()
	for sid := range n.notified {
		if !liveBoardIDs[sid] {
			delete(n.notified, sid)
		}
	}
	n.notifiedMu.Unlock()

	return nil
}
