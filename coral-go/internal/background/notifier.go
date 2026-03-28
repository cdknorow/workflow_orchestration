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
	boardStore *board.Store
	runtime    AgentRuntime
	interval   time.Duration
	logger     *slog.Logger
	discoverFn func(ctx context.Context) ([]AgentInfo, error)
	isPausedFn func(project string) bool
	notifiedMu sync.Mutex
	notified   map[string]int // session_id -> unread count at last notification
}

// NewBoardNotifier creates a new BoardNotifier.
func NewBoardNotifier(boardStore *board.Store, runtime AgentRuntime, interval time.Duration) *BoardNotifier {
	return &BoardNotifier{
		boardStore: boardStore,
		runtime:    runtime,
		interval:   interval,
		logger:     slog.Default().With("service", "board_notifier"),
		notified:   make(map[string]int),
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
		}
	}
}

// RunOnce performs a single notification pass.
func (n *BoardNotifier) RunOnce(ctx context.Context) error {
	agents, err := n.discoverFn(ctx)
	if err != nil {
		return err
	}

	liveBoardIDs := make(map[string]bool)

	for _, a := range agents {
		if a.SessionID == "" {
			continue
		}

		subscriberID := naming.SubscriberID(a.DisplayName, a.AgentType)
		liveBoardIDs[subscriberID] = true

		sub, err := n.boardStore.GetSubscription(ctx, subscriberID)
		if err != nil || sub == nil {
			continue
		}
		// Skip remote subscribers
		if sub.OriginServer != nil && *sub.OriginServer != "" {
			continue
		}
		// Skip agents on paused/sleeping boards
		if n.isPausedFn != nil && sub.Project != "" && n.isPausedFn(sub.Project) {
			continue
		}

		unread, err := n.boardStore.CheckUnread(ctx, sub.Project, subscriberID)
		if err != nil {
			continue
		}

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
			continue
		}

		// Send nudge via tmux session name (routing identity, not board identity)
		plural := "s"
		if unread == 1 {
			plural = ""
		}
		nudge := fmt.Sprintf("You have %d unread message%s on the message board. Run 'coral-board read' to see them.", unread, plural)
		sessName := naming.SessionName(a.AgentType, a.SessionID)
		err = n.runtime.SendInput(ctx, sessName, nudge)
		if err != nil {
			n.logger.Warn("failed to nudge agent", "agent", a.AgentName, "error", err)
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
