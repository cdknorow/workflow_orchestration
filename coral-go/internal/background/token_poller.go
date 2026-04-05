package background

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	at "github.com/cdknorow/coral/internal/agenttypes"
	"github.com/cdknorow/coral/internal/proxy"
	"github.com/cdknorow/coral/internal/store"
)

// TokenPoller periodically reads Codex (and future non-Claude) agent transcripts
// to extract token usage data and store it via the TokenUsageStore.
type TokenPoller struct {
	sessionStore *store.SessionStore
	usageStore   *store.TokenUsageStore
	interval     time.Duration
	logger       *slog.Logger

	// Track last-polled mtime per rollout file to skip unchanged files.
	lastMtime map[string]time.Time
	// Cache mapping session ID → rollout file path (resolved once per session).
	sessionPaths map[string]string
	// Track the last cumulative totals per session to compute deltas.
	lastCumulative map[string]cumulativeTokens
	// Track how many token_count entries we've already processed per session.
	lastEntryCount map[string]int
}

// NewTokenPoller creates a new TokenPoller.
func NewTokenPoller(ss *store.SessionStore, us *store.TokenUsageStore, interval time.Duration) *TokenPoller {
	return &TokenPoller{
		sessionStore:   ss,
		usageStore:     us,
		interval:       interval,
		logger:         slog.Default().With("service", "token_poller"),
		lastMtime:      make(map[string]time.Time),
		sessionPaths:   make(map[string]string),
		lastCumulative: make(map[string]cumulativeTokens),
		lastEntryCount: make(map[string]int),
	}
}

// Run starts the polling loop.
func (p *TokenPoller) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *TokenPoller) pollOnce(ctx context.Context) {
	sessions, err := p.sessionStore.GetAllLiveSessions(ctx)
	if err != nil {
		p.logger.Error("failed to list live sessions", "error", err)
		return
	}

	for _, ls := range sessions {
		if ls.AgentType != at.Codex {
			continue
		}
		if ls.IsSleeping == 1 {
			continue
		}
		p.pollCodexSession(ctx, &ls)
	}
}

func (p *TokenPoller) pollCodexSession(ctx context.Context, ls *store.LiveSession) {
	// Resolve the rollout file path (cached per session)
	rolloutPath, ok := p.sessionPaths[ls.SessionID]
	if !ok {
		rolloutPath = findCodexRollout(ls)
		if rolloutPath == "" {
			p.logger.Warn("no rollout file found for Codex session",
				"session_id", ls.SessionID[:8],
				"agent_name", ls.AgentName,
				"working_dir", ls.WorkingDir,
				"created_at", ls.CreatedAt)
			return
		}
		p.logger.Info("matched rollout file for Codex session",
			"session_id", ls.SessionID[:8],
			"path", filepath.Base(rolloutPath))
		p.sessionPaths[ls.SessionID] = rolloutPath
	}

	// Check mtime to skip unchanged files
	info, err := os.Stat(rolloutPath)
	if err != nil {
		return
	}
	if lastMtime, ok := p.lastMtime[rolloutPath]; ok && !info.ModTime().After(lastMtime) {
		return
	}
	p.lastMtime[rolloutPath] = info.ModTime()

	// Extract all cumulative token_count entries from the rollout file
	usage := extractCodexUsage(rolloutPath)
	if usage == nil || len(usage.Calls) == 0 {
		return
	}

	// Skip entries we've already processed
	alreadyProcessed := p.lastEntryCount[ls.SessionID]
	if len(usage.Calls) <= alreadyProcessed {
		return
	}
	newCalls := usage.Calls[alreadyProcessed:]

	agentName := ""
	if ls.DisplayName != nil {
		agentName = *ls.DisplayName
	}

	// Get the last cumulative totals for delta computation
	prev := p.lastCumulative[ls.SessionID]

	for _, call := range newCalls {
		// Compute delta from previous cumulative totals
		deltaInput := call.InputTokens - prev.Input
		deltaOutput := call.OutputTokens - prev.Output
		deltaCached := call.CachedInput - prev.Cached
		deltaTotal := call.TotalTokens - prev.Total

		// Skip zero-delta entries (e.g. first token_count with all zeros)
		if deltaInput == 0 && deltaOutput == 0 {
			prev = cumulativeTokens{
				Input: call.InputTokens, Output: call.OutputTokens,
				Cached: call.CachedInput, Total: call.TotalTokens,
			}
			continue
		}

		model := call.Model
		if model == "" {
			model = "gpt-5.4"
		}
		costUSD := estimateCost(model, deltaInput, deltaOutput, deltaCached)

		record := &store.TokenUsage{
			SessionID:       ls.SessionID,
			AgentName:       agentName,
			AgentType:       at.Codex,
			TeamID:          ls.TeamID,
			BoardName:       ls.BoardName,
			InputTokens:     deltaInput,
			OutputTokens:    deltaOutput,
			CacheReadTokens: deltaCached,
			TotalTokens:     deltaTotal,
			CostUSD:         costUSD,
			SessionStartAt:  usage.SessionStartAt,
			LastActivityAt:  call.Timestamp,
			RecordedAt:      call.Timestamp, // Use the actual event timestamp as the key
		}

		if err := p.usageStore.RecordUsage(ctx, record); err != nil {
			p.logger.Error("failed to record Codex usage", "session_id", ls.SessionID, "error", err)
			continue
		}

		p.logger.Debug("recorded Codex call",
			"session_id", ls.SessionID[:8],
			"model", model,
			"delta_input", deltaInput,
			"delta_output", deltaOutput,
			"cost_usd", costUSD,
			"timestamp", call.Timestamp)

		// Update cumulative tracking
		prev = cumulativeTokens{
			Input: call.InputTokens, Output: call.OutputTokens,
			Cached: call.CachedInput, Total: call.TotalTokens,
		}
	}

	p.lastCumulative[ls.SessionID] = prev
	p.lastEntryCount[ls.SessionID] = len(usage.Calls)
}

// codexUsageData holds token usage extracted from a Codex rollout file.
// cumulativeTokens tracks the running cumulative totals for delta computation.
type cumulativeTokens struct {
	Input  int
	Output int
	Cached int
	Total  int
}

// codexCallData represents a single API call's token delta.
type codexCallData struct {
	InputTokens  int
	OutputTokens int
	CachedInput  int
	TotalTokens  int
	Timestamp    string
	Model        string
}

// codexUsageData holds all per-call deltas extracted from a rollout file.
type codexUsageData struct {
	Calls          []codexCallData
	SessionStartAt string
}

// extractCodexUsage reads a Codex rollout JSONL file and extracts ALL
// cumulative token_count entries. The caller computes deltas using tracked state.
func extractCodexUsage(path string) *codexUsageData {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}

	result := &codexUsageData{}
	var currentModel string

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var entry struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				Type string `json:"type"`
				Info struct {
					TotalTokenUsage struct {
						InputTokens           int `json:"input_tokens"`
						CachedInputTokens     int `json:"cached_input_tokens"`
						OutputTokens          int `json:"output_tokens"`
						ReasoningOutputTokens int `json:"reasoning_output_tokens"`
						TotalTokens           int `json:"total_tokens"`
					} `json:"total_token_usage"`
				} `json:"info"`
				Model string `json:"model"`
				CWD   string `json:"cwd"`
			} `json:"payload"`
		}

		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		if entry.Type == "session_meta" && entry.Timestamp != "" {
			result.SessionStartAt = entry.Timestamp
		}

		if entry.Type == "turn_context" && entry.Payload.Model != "" {
			currentModel = entry.Payload.Model
		}

		if entry.Type == "event_msg" && entry.Payload.Type == "token_count" {
			tu := entry.Payload.Info.TotalTokenUsage
			result.Calls = append(result.Calls, codexCallData{
				InputTokens:  tu.InputTokens,
				OutputTokens: tu.OutputTokens + tu.ReasoningOutputTokens,
				CachedInput:  tu.CachedInputTokens,
				TotalTokens:  tu.TotalTokens,
				Timestamp:    entry.Timestamp,
				Model:        currentModel,
			})
		}
	}

	if len(result.Calls) == 0 {
		return nil
	}
	return result
}

// findCodexRollout finds the Codex rollout file for a live session.
// Since Codex rollout UUIDs don't match Coral session IDs, we match by:
// 1. Session ID substring in filename (works when Coral controls the ID)
// 2. Most recently modified rollout file whose cwd matches the session's working dir
// 3. Most recently created rollout file near the session's creation time
func findCodexRollout(ls *store.LiveSession) string {
	home, _ := os.UserHomeDir()
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	basePath := filepath.Join(codexHome, "sessions")

	// Strategy 1: Try matching session ID in filename
	matches, err := filepath.Glob(filepath.Join(basePath, "*", "*", "*", "rollout-*"+ls.SessionID+"*.jsonl"))
	if err == nil && len(matches) > 0 {
		return matches[0]
	}

	// Strategy 2: Find recently modified rollout files and match by working dir
	type rolloutFile struct {
		path  string
		mtime time.Time
	}
	var candidates []rolloutFile

	// Parse session creation time (stored as RFC3339 with microseconds)
	sessionCreated, _ := time.Parse(time.RFC3339Nano, ls.CreatedAt)
	if sessionCreated.IsZero() {
		sessionCreated, _ = time.Parse(time.RFC3339, ls.CreatedAt)
	}
	if sessionCreated.IsZero() {
		sessionCreated, _ = time.Parse("2006-01-02T15:04:05Z", ls.CreatedAt)
	}
	if sessionCreated.IsZero() {
		sessionCreated, _ = time.Parse("2006-01-02 15:04:05", ls.CreatedAt)
	}

	_ = filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		// Only consider files modified in the last hour or created near session start
		if time.Since(info.ModTime()) < time.Hour {
			candidates = append(candidates, rolloutFile{path: path, mtime: info.ModTime()})
		} else if !sessionCreated.IsZero() && info.ModTime().After(sessionCreated.Add(-5*time.Minute)) {
			candidates = append(candidates, rolloutFile{path: path, mtime: info.ModTime()})
		}
		return nil
	})

	// Sort by most recently modified
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].mtime.After(candidates[j].mtime)
	})

	// Check each candidate's cwd against the session's working dir
	for _, c := range candidates {
		cwd := peekCodexCWD(c.path)
		if cwd != "" && cwd == ls.WorkingDir {
			return c.path
		}
	}

	// Fallback: if we have a session creation time, pick the rollout file
	// created closest to it
	if !sessionCreated.IsZero() && len(candidates) > 0 {
		var bestPath string
		var bestDelta time.Duration = time.Hour * 24
		for _, c := range candidates {
			// Parse timestamp from filename: rollout-YYYY-MM-DDTHH-MM-SS-{uuid}.jsonl
			base := filepath.Base(c.path)
			if !strings.HasPrefix(base, "rollout-") {
				continue
			}
			// Extract timestamp portion
			parts := strings.SplitN(strings.TrimPrefix(base, "rollout-"), "-", 7)
			if len(parts) >= 6 {
				ts := strings.Join(parts[:6], "-")
				// Format: 2026-03-29T00-03-11 → 2006-01-02T15-04-05
				if t, err := time.Parse("2006-01-02T15-04-05", ts); err == nil {
					delta := t.Sub(sessionCreated).Abs()
					if delta < bestDelta {
						bestDelta = delta
						bestPath = c.path
					}
				}
			}
		}
		if bestPath != "" && bestDelta < 5*time.Minute {
			return bestPath
		}
	}

	return ""
}

// peekCodexCWD reads the first few lines of a Codex rollout file to extract
// the working directory from a turn_context entry.
func peekCodexCWD(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	// Read first 32KB — turn_context is usually in the first few entries
	buf := make([]byte, 32*1024)
	n, _ := f.Read(buf)
	if n == 0 {
		return ""
	}

	for _, line := range strings.Split(string(buf[:n]), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry struct {
			Type    string `json:"type"`
			Payload struct {
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		// Both session_meta and turn_context have cwd
		if (entry.Type == "session_meta" || entry.Type == "turn_context") && entry.Payload.CWD != "" {
			return entry.Payload.CWD
		}
	}
	return ""
}

// estimateCost calculates estimated cost based on model pricing.
func estimateCost(model string, inputTokens, outputTokens, cachedTokens int) float64 {
	breakdown := proxy.CalculateCostBreakdown(model, proxy.TokenUsage{
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		CacheReadTokens: cachedTokens,
	})
	return breakdown.TotalCostUSD
}
