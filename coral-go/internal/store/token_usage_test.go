package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenUsageStore_RecordAndGet(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	err := s.RecordUsage(ctx, &TokenUsage{
		SessionID:    "sess-1",
		AgentName:    "my-agent",
		AgentType:    "claude",
		InputTokens:  1000,
		OutputTokens: 500,
		TotalTokens:  1500,
		CostUSD:      0.03,
		NumTurns:     5,
	})
	require.NoError(t, err)

	got, err := s.GetSessionUsage(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "sess-1", got.SessionID)
	assert.Equal(t, 1000, got.InputTokens)
	assert.Equal(t, 500, got.OutputTokens)
	assert.Equal(t, 1500, got.TotalTokens)
	assert.InDelta(t, 0.03, got.CostUSD, 0.001)
	assert.Equal(t, 1, got.NumTurns) // COUNT(*) of records
}

func TestTokenUsageStore_GetSessionUsage_Latest(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	// Record two snapshots for the same session
	err := s.RecordUsage(ctx, &TokenUsage{
		SessionID: "sess-1", AgentName: "a", InputTokens: 100, OutputTokens: 50, TotalTokens: 150, CostUSD: 0.01,
	})
	require.NoError(t, err)

	err = s.RecordUsage(ctx, &TokenUsage{
		SessionID: "sess-1", AgentName: "a", InputTokens: 200, OutputTokens: 100, TotalTokens: 300, CostUSD: 0.02,
	})
	require.NoError(t, err)

	// Should return the sum of all per-turn records
	got, err := s.GetSessionUsage(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 450, got.TotalTokens) // 150 + 300
}

func TestTokenUsageStore_GetSessionUsage_NotFound(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	got, err := s.GetSessionUsage(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTokenUsageStore_GetTeamUsage(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	teamID := int64(1)
	// Two sessions in the same team, each with 2 snapshots
	err := s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s1", AgentName: "a1", TeamID: &teamID, InputTokens: 100, TotalTokens: 150, CostUSD: 0.01,
	})
	require.NoError(t, err)
	err = s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s1", AgentName: "a1", TeamID: &teamID, InputTokens: 200, TotalTokens: 300, CostUSD: 0.02,
	})
	require.NoError(t, err)
	err = s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s2", AgentName: "a2", TeamID: &teamID, InputTokens: 500, TotalTokens: 800, CostUSD: 0.05,
	})
	require.NoError(t, err)

	summary, err := s.GetTeamUsage(ctx, teamID)
	require.NoError(t, err)
	// Should sum all per-turn records: s1(150+300) + s2(800) = 1250
	assert.Equal(t, int64(1250), summary.TotalTokens)
	assert.InDelta(t, 0.08, summary.CostUSD, 0.001)
	assert.Equal(t, 2, summary.NumSessions)
}

func TestTokenUsageStore_GetBoardUsage(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	board := "my-board"
	err := s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s1", AgentName: "a1", BoardName: &board, TotalTokens: 500, CostUSD: 0.03,
	})
	require.NoError(t, err)
	err = s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s2", AgentName: "a2", BoardName: &board, TotalTokens: 300, CostUSD: 0.02,
	})
	require.NoError(t, err)

	summary, err := s.GetBoardUsage(ctx, "my-board")
	require.NoError(t, err)
	assert.Equal(t, int64(800), summary.TotalTokens)
	assert.InDelta(t, 0.05, summary.CostUSD, 0.001)
}

func TestTokenUsageStore_GetUsageSummary(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	err := s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s1", AgentName: "a1", AgentType: "claude", TotalTokens: 1000, CostUSD: 0.05,
	})
	require.NoError(t, err)
	err = s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s2", AgentName: "a2", AgentType: "gemini", TotalTokens: 2000, CostUSD: 0.01,
	})
	require.NoError(t, err)

	summaries, err := s.GetUsageSummary(ctx, "")
	require.NoError(t, err)
	assert.Len(t, summaries, 2)

	// Find claude summary
	for _, s := range summaries {
		if s.AgentType == "claude" {
			assert.Equal(t, int64(1000), s.TotalTokens)
			assert.Equal(t, 1, s.NumSessions)
		}
		if s.AgentType == "gemini" {
			assert.Equal(t, int64(2000), s.TotalTokens)
		}
	}
}

func TestTokenUsageStore_ListUsage(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	teamID := int64(1)
	board := "b1"
	err := s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s1", AgentName: "a1", TeamID: &teamID, BoardName: &board, TotalTokens: 100,
	})
	require.NoError(t, err)
	err = s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s2", AgentName: "a2", TotalTokens: 200,
	})
	require.NoError(t, err)

	// List all
	results, err := s.ListUsage(ctx, UsageFilter{})
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Filter by session
	results, err = s.ListUsage(ctx, UsageFilter{SessionID: "s1"})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "s1", results[0].SessionID)

	// Filter by team
	results, err = s.ListUsage(ctx, UsageFilter{TeamID: &teamID})
	require.NoError(t, err)
	assert.Len(t, results, 1)

	// Filter by board
	results, err = s.ListUsage(ctx, UsageFilter{BoardName: "b1"})
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestTokenUsageStore_DefaultAgentType(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	err := s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s1", AgentName: "a1", TotalTokens: 100,
	})
	require.NoError(t, err)

	got, err := s.GetSessionUsage(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "claude", got.AgentType)
}

func TestTokenUsageStore_CacheTokens(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	err := s.RecordUsage(ctx, &TokenUsage{
		SessionID:        "s1",
		AgentName:        "a1",
		InputTokens:      1000,
		OutputTokens:     500,
		CacheReadTokens:  300,
		CacheWriteTokens: 100,
		TotalTokens:      1900,
		CostUSD:          0.04,
	})
	require.NoError(t, err)

	got, err := s.GetSessionUsage(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 300, got.CacheReadTokens)
	assert.Equal(t, 100, got.CacheWriteTokens)
	assert.Equal(t, 1900, got.TotalTokens)
}

func TestTokenUsageStore_GetLatestUsageBySessionIDs(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	// Record multiple snapshots for s1 and one for s2
	require.NoError(t, s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s1", AgentName: "a1", TotalTokens: 100, CostUSD: 0.01,
	}))
	require.NoError(t, s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s1", AgentName: "a1", TotalTokens: 500, CostUSD: 0.05,
	}))
	require.NoError(t, s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s2", AgentName: "a2", TotalTokens: 200, CostUSD: 0.02,
	}))

	result, err := s.GetLatestUsageBySessionIDs(ctx, []string{"s1", "s2", "s3"})
	require.NoError(t, err)
	assert.Len(t, result, 2)

	// s1 should have the sum of all per-turn records
	assert.Equal(t, 600, result["s1"].TotalTokens) // 100 + 500
	assert.InDelta(t, 0.06, result["s1"].CostUSD, 0.001)

	// s2 should have its only record
	assert.Equal(t, 200, result["s2"].TotalTokens)

	// s3 should not be present
	assert.Nil(t, result["s3"])
}

func TestTokenUsageStore_GetLatestUsageBySessionIDs_Empty(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	result, err := s.GetLatestUsageBySessionIDs(ctx, []string{})
	require.NoError(t, err)
	assert.Len(t, result, 0)
}

func TestTokenUsageStore_ListUsage_SinceFilter(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	require.NoError(t, s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s-old", AgentName: "a1", TotalTokens: 100, RecordedAt: "2020-01-01T00:00:00Z",
	}))
	require.NoError(t, s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s-new", AgentName: "a2", TotalTokens: 500, RecordedAt: "2025-06-01T00:00:00Z",
	}))

	results, err := s.ListUsage(ctx, UsageFilter{Since: "2025-01-01T00:00:00Z"})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "s-new", results[0].SessionID)
}

func TestTokenUsageStore_GetUsageSummary_SinceFilter(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	require.NoError(t, s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s-old", AgentName: "a1", AgentType: "claude",
		TotalTokens: 100, CostUSD: 0.01, RecordedAt: "2020-01-01T00:00:00Z",
	}))
	require.NoError(t, s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s-new", AgentName: "a2", AgentType: "claude",
		TotalTokens: 500, CostUSD: 0.05, RecordedAt: "2025-06-01T00:00:00Z",
	}))

	summaries, err := s.GetUsageSummary(ctx, "2025-01-01T00:00:00Z")
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, int64(500), summaries[0].TotalTokens)
	assert.Equal(t, 1, summaries[0].NumSessions)
}

func TestTokenUsageStore_RecordSetsID(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	u := &TokenUsage{SessionID: "s1", AgentName: "a1", TotalTokens: 100}
	require.NoError(t, s.RecordUsage(ctx, u))
	assert.Greater(t, u.ID, int64(0))
}

func TestTokenUsageStore_RecordDefaultsRecordedAt(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	u := &TokenUsage{SessionID: "s1", AgentName: "a1", TotalTokens: 100}
	require.NoError(t, s.RecordUsage(ctx, u))
	assert.NotEmpty(t, u.RecordedAt)
}

func TestTokenUsageStore_GetTeamUsage_CacheTokens(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	teamID := int64(1)
	require.NoError(t, s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s1", AgentName: "a1", TeamID: &teamID,
		InputTokens: 100, CacheReadTokens: 50, CacheWriteTokens: 20,
		TotalTokens: 170, CostUSD: 0.01,
	}))
	require.NoError(t, s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s2", AgentName: "a2", TeamID: &teamID,
		InputTokens: 200, CacheReadTokens: 30, CacheWriteTokens: 10,
		TotalTokens: 240, CostUSD: 0.02,
	}))

	summary, err := s.GetTeamUsage(ctx, teamID)
	require.NoError(t, err)
	assert.Equal(t, int64(80), summary.CacheReadTokens)
	assert.Equal(t, int64(30), summary.CacheWriteTokens)
}

func TestTokenUsageStore_GetBoardUsage_NoMatch(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	summary, err := s.GetBoardUsage(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Equal(t, int64(0), summary.TotalTokens)
	assert.Equal(t, 0, summary.NumSessions)
}

func TestTokenUsageStore_ListUsage_MultipleFilters(t *testing.T) {
	db := openTestDB(t)
	s := NewTokenUsageStore(db)
	ctx := context.Background()

	teamID := int64(1)
	board := "eng"
	require.NoError(t, s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s1", AgentName: "a1", TeamID: &teamID, BoardName: &board, TotalTokens: 100,
	}))
	require.NoError(t, s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s2", AgentName: "a2", TeamID: &teamID, TotalTokens: 200,
	}))
	require.NoError(t, s.RecordUsage(ctx, &TokenUsage{
		SessionID: "s3", AgentName: "a3", BoardName: &board, TotalTokens: 300,
	}))

	// Filter by both team AND board
	results, err := s.ListUsage(ctx, UsageFilter{TeamID: &teamID, BoardName: "eng"})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "s1", results[0].SessionID)
}
