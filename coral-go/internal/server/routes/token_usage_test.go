package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cdknorow/coral/internal/store"
)

func setupTokenUsageTest(t *testing.T) (*store.TokenUsageStore, *TokenUsageHandler, *chi.Mux) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "token_test.db")
	db, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	ts := store.NewTokenUsageStore(db)
	h := NewTokenUsageHandler(db)
	r := chi.NewRouter()
	r.Get("/api/token-usage", h.ListUsage)
	r.Get("/api/token-usage/summary", h.UsageSummary)
	return ts, h, r
}

func TestListUsage_Empty(t *testing.T) {
	_, _, router := setupTokenUsageTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/token-usage", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	// records may be null/nil when empty
	if records, ok := body["records"].([]any); ok {
		assert.Len(t, records, 0)
	} else {
		assert.Nil(t, body["records"])
	}

	totals := body["totals"].(map[string]any)
	assert.Equal(t, float64(0), totals["total_tokens"])
	assert.Equal(t, float64(0), totals["cost_usd"])
	assert.Equal(t, float64(0), totals["num_sessions"])
}

func TestListUsage_WithRecords(t *testing.T) {
	ts, _, router := setupTokenUsageTest(t)
	ctx := t.Context()

	teamID := int64(5)
	board := "eng-team"
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s1", AgentName: "agent-a", AgentType: "claude",
		TeamID: &teamID, BoardName: &board,
		InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 100, CacheWriteTokens: 50,
		TotalTokens: 1650, CostUSD: 0.05, NumTurns: 3,
	}))
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s2", AgentName: "agent-b", AgentType: "gemini",
		InputTokens: 2000, OutputTokens: 1000, TotalTokens: 3000, CostUSD: 0.02,
	}))

	// Unfiltered
	req := httptest.NewRequest(http.MethodGet, "/api/token-usage", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	records := body["records"].([]any)
	assert.Len(t, records, 2)

	totals := body["totals"].(map[string]any)
	assert.Equal(t, float64(3000), totals["input_tokens"])
	assert.Equal(t, float64(1500), totals["output_tokens"])
	assert.Equal(t, float64(4650), totals["total_tokens"])
	assert.InDelta(t, 0.07, totals["cost_usd"].(float64), 0.001)
	assert.Equal(t, float64(2), totals["num_sessions"])
}

func TestListUsage_FilterBySessionID(t *testing.T) {
	ts, _, router := setupTokenUsageTest(t)
	ctx := t.Context()

	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s1", AgentName: "a1", TotalTokens: 100, CostUSD: 0.01,
	}))
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s2", AgentName: "a2", TotalTokens: 200, CostUSD: 0.02,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/token-usage?session_id=s1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	records := body["records"].([]any)
	assert.Len(t, records, 1)
	assert.Equal(t, "s1", records[0].(map[string]any)["session_id"])
}

func TestListUsage_FilterByTeamID(t *testing.T) {
	ts, _, router := setupTokenUsageTest(t)
	ctx := t.Context()

	teamID := int64(42)
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s1", AgentName: "a1", TeamID: &teamID, TotalTokens: 100,
	}))
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s2", AgentName: "a2", TotalTokens: 200,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/token-usage?team_id=42", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	records := body["records"].([]any)
	assert.Len(t, records, 1)
}

func TestListUsage_FilterByBoardName(t *testing.T) {
	ts, _, router := setupTokenUsageTest(t)
	ctx := t.Context()

	board := "my-board"
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s1", AgentName: "a1", BoardName: &board, TotalTokens: 100,
	}))
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s2", AgentName: "a2", TotalTokens: 200,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/token-usage?board_name=my-board", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	records := body["records"].([]any)
	assert.Len(t, records, 1)
}

func TestListUsage_InvalidTeamID(t *testing.T) {
	_, _, router := setupTokenUsageTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/token-usage?team_id=notanumber", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListUsage_SumsPerTurnDeltas(t *testing.T) {
	ts, _, router := setupTokenUsageTest(t)
	ctx := t.Context()

	// Record two per-turn delta records for the same session — should be summed
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s1", AgentName: "a1", InputTokens: 100, TotalTokens: 100, CostUSD: 0.01,
	}))
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s1", AgentName: "a1", InputTokens: 500, TotalTokens: 500, CostUSD: 0.05,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/token-usage", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	records := body["records"].([]any)
	// Per-turn deltas summed per session — one aggregated row
	assert.Len(t, records, 1)
	assert.Equal(t, float64(600), records[0].(map[string]any)["total_tokens"])

	totals := body["totals"].(map[string]any)
	assert.Equal(t, float64(600), totals["total_tokens"])
	assert.InDelta(t, 0.06, totals["cost_usd"].(float64), 0.001)
}

func TestUsageSummary_Empty(t *testing.T) {
	_, _, router := setupTokenUsageTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/token-usage/summary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	if byType, ok := body["by_agent_type"].([]any); ok {
		assert.Len(t, byType, 0)
	} else {
		assert.Nil(t, body["by_agent_type"])
	}

	totals := body["totals"].(map[string]any)
	assert.Equal(t, float64(0), totals["total_tokens"])
}

func TestUsageSummary_GroupsByAgentType(t *testing.T) {
	ts, _, router := setupTokenUsageTest(t)
	ctx := t.Context()

	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s1", AgentName: "a1", AgentType: "claude",
		InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500, CostUSD: 0.05,
	}))
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s2", AgentName: "a2", AgentType: "claude",
		InputTokens: 2000, OutputTokens: 800, TotalTokens: 2800, CostUSD: 0.08,
	}))
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s3", AgentName: "a3", AgentType: "gemini",
		InputTokens: 500, OutputTokens: 200, TotalTokens: 700, CostUSD: 0.01,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/token-usage/summary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	byType := body["by_agent_type"].([]any)
	assert.Len(t, byType, 2)

	totals := body["totals"].(map[string]any)
	assert.Equal(t, float64(3500), totals["input_tokens"])
	assert.Equal(t, float64(1500), totals["output_tokens"])
	assert.Equal(t, float64(5000), totals["total_tokens"])
	assert.InDelta(t, 0.14, totals["cost_usd"].(float64), 0.001)
	assert.Equal(t, float64(3), totals["num_sessions"])
}

func TestUsageSummary_SinceFilter(t *testing.T) {
	ts, _, router := setupTokenUsageTest(t)
	ctx := t.Context()

	// Old record
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s-old", AgentName: "a1", AgentType: "claude",
		TotalTokens: 1000, CostUSD: 0.05, RecordedAt: "2020-01-01T00:00:00Z",
	}))
	// Recent record
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s-new", AgentName: "a2", AgentType: "claude",
		TotalTokens: 2000, CostUSD: 0.10, RecordedAt: "2025-06-01T00:00:00Z",
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/token-usage/summary?since=2025-01-01T00:00:00Z", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	totals := body["totals"].(map[string]any)
	assert.Equal(t, float64(2000), totals["total_tokens"])
	assert.Equal(t, float64(1), totals["num_sessions"])
}

func TestUsageSummary_SumsPerTurnDeltas(t *testing.T) {
	ts, _, router := setupTokenUsageTest(t)
	ctx := t.Context()

	// Two per-turn delta records for the same session — should be summed
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s1", AgentName: "a1", AgentType: "claude",
		TotalTokens: 100, CostUSD: 0.01,
	}))
	require.NoError(t, ts.RecordUsage(ctx, &store.TokenUsage{
		SessionID: "s1", AgentName: "a1", AgentType: "claude",
		TotalTokens: 500, CostUSD: 0.05,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/token-usage/summary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	totals := body["totals"].(map[string]any)
	// Records are per-turn deltas — both should be summed
	assert.Equal(t, float64(600), totals["total_tokens"])
	assert.Equal(t, float64(1), totals["num_sessions"])
}

func TestTokenUsageHandler_StoreAccessor(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "accessor_test.db")
	db, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	h := NewTokenUsageHandler(db)
	assert.NotNil(t, h.Store())
}
