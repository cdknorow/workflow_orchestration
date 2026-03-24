package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func registerTeam(t *testing.T, ss *SessionStore, ctx context.Context, board string) []string {
	t.Helper()
	sessions := []struct {
		id, agentType, name, display string
	}{
		{"sess-lead", "claude", "repo", "Lead Developer"},
		{"sess-fe", "claude", "repo", "Frontend Dev"},
		{"sess-qa", "codex", "repo", "QA Engineer"},
	}
	var ids []string
	for _, s := range sessions {
		dn := s.display
		err := ss.RegisterLiveSession(ctx, &LiveSession{
			SessionID:   s.id,
			AgentType:   s.agentType,
			AgentName:   s.name,
			WorkingDir:  "/tmp/repo",
			DisplayName: &dn,
			BoardName:   strPtr(board),
		})
		require.NoError(t, err)
		ids = append(ids, s.id)
	}
	return ids
}

// ── Sleep Tests ─────────────────────────────────────────────

func TestSleepTeam_SetsAllSleeping(t *testing.T) {
	db := openTestDB(t)
	ss := NewSessionStore(db)
	ctx := context.Background()

	registerTeam(t, ss, ctx, "test-board")

	n, err := ss.SetBoardSleeping(ctx, "test-board", true)
	require.NoError(t, err)
	assert.Equal(t, 3, n, "all 3 sessions should be marked sleeping")

	sessions, err := ss.GetAllLiveSessions(ctx)
	require.NoError(t, err)
	for _, s := range sessions {
		assert.Equal(t, 1, s.IsSleeping, "session %s should be sleeping", s.SessionID)
	}
}

func TestSleepTeam_GetSleepingBoardNames(t *testing.T) {
	db := openTestDB(t)
	ss := NewSessionStore(db)
	ctx := context.Background()

	registerTeam(t, ss, ctx, "test-board")
	ss.SetBoardSleeping(ctx, "test-board", true)

	names, err := ss.GetSleepingBoardNames(ctx)
	require.NoError(t, err)
	assert.Contains(t, names, "test-board")
}

// ── Wake Tests ──────────────────────────────────────────────

func TestWakeTeam_ClearsSleepingFlag(t *testing.T) {
	db := openTestDB(t)
	ss := NewSessionStore(db)
	ctx := context.Background()

	registerTeam(t, ss, ctx, "test-board")
	ss.SetBoardSleeping(ctx, "test-board", true)

	n, err := ss.SetBoardSleeping(ctx, "test-board", false)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	sessions, err := ss.GetAllLiveSessions(ctx)
	require.NoError(t, err)
	for _, s := range sessions {
		assert.Equal(t, 0, s.IsSleeping, "session %s should not be sleeping", s.SessionID)
	}
}

func TestWakeTeam_PreservesSessionIDs(t *testing.T) {
	db := openTestDB(t)
	ss := NewSessionStore(db)
	ctx := context.Background()

	ids := registerTeam(t, ss, ctx, "test-board")
	ss.SetBoardSleeping(ctx, "test-board", true)
	ss.SetBoardSleeping(ctx, "test-board", false)

	sessions, err := ss.GetAllLiveSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 3)

	foundIDs := make(map[string]bool)
	for _, s := range sessions {
		foundIDs[s.SessionID] = true
	}
	for _, id := range ids {
		assert.True(t, foundIDs[id], "session %s should be preserved after wake", id)
	}
}

func TestWakeTeam_NoNewRows(t *testing.T) {
	db := openTestDB(t)
	ss := NewSessionStore(db)
	ctx := context.Background()

	registerTeam(t, ss, ctx, "test-board")

	before, _ := ss.GetAllLiveSessions(ctx)
	beforeCount := len(before)

	ss.SetBoardSleeping(ctx, "test-board", true)
	ss.SetBoardSleeping(ctx, "test-board", false)

	after, _ := ss.GetAllLiveSessions(ctx)
	assert.Equal(t, beforeCount, len(after), "wake should not create new rows")
}

func TestWakeTeam_PreservesDisplayNames(t *testing.T) {
	db := openTestDB(t)
	ss := NewSessionStore(db)
	ctx := context.Background()

	registerTeam(t, ss, ctx, "test-board")
	ss.SetBoardSleeping(ctx, "test-board", true)
	ss.SetBoardSleeping(ctx, "test-board", false)

	sessions, err := ss.GetAllLiveSessions(ctx)
	require.NoError(t, err)

	nameMap := make(map[string]string)
	for _, s := range sessions {
		if s.DisplayName != nil {
			nameMap[s.SessionID] = *s.DisplayName
		}
	}
	assert.Equal(t, "Lead Developer", nameMap["sess-lead"])
	assert.Equal(t, "Frontend Dev", nameMap["sess-fe"])
	assert.Equal(t, "QA Engineer", nameMap["sess-qa"])
}

func TestWakeTeam_PreservesBoardName(t *testing.T) {
	db := openTestDB(t)
	ss := NewSessionStore(db)
	ctx := context.Background()

	registerTeam(t, ss, ctx, "test-board")
	ss.SetBoardSleeping(ctx, "test-board", true)
	ss.SetBoardSleeping(ctx, "test-board", false)

	sessions, err := ss.GetAllLiveSessions(ctx)
	require.NoError(t, err)
	for _, s := range sessions {
		require.NotNil(t, s.BoardName)
		assert.Equal(t, "test-board", *s.BoardName, "board name should be preserved for %s", s.SessionID)
	}
}

// ── Double Sleep/Wake Tests ─────────────────────────────────

func TestDoubleSleepWake_NoDuplicates(t *testing.T) {
	db := openTestDB(t)
	ss := NewSessionStore(db)
	ctx := context.Background()

	registerTeam(t, ss, ctx, "test-board")

	// Sleep → Wake → Sleep → Wake
	ss.SetBoardSleeping(ctx, "test-board", true)
	ss.SetBoardSleeping(ctx, "test-board", false)
	ss.SetBoardSleeping(ctx, "test-board", true)
	ss.SetBoardSleeping(ctx, "test-board", false)

	sessions, err := ss.GetAllLiveSessions(ctx)
	require.NoError(t, err)
	assert.Len(t, sessions, 3, "double sleep/wake should not create duplicate rows")
}

func TestDoubleSleep_Idempotent(t *testing.T) {
	db := openTestDB(t)
	ss := NewSessionStore(db)
	ctx := context.Background()

	registerTeam(t, ss, ctx, "test-board")

	n1, _ := ss.SetBoardSleeping(ctx, "test-board", true)
	n2, _ := ss.SetBoardSleeping(ctx, "test-board", true)
	assert.Equal(t, 3, n1)
	assert.Equal(t, 3, n2, "double sleep should still affect all rows")

	sessions, _ := ss.GetAllLiveSessions(ctx)
	assert.Len(t, sessions, 3)
}

// ── Individual Session Sleep/Wake ───────────────────────────

func TestSetSessionSleeping_Individual(t *testing.T) {
	db := openTestDB(t)
	ss := NewSessionStore(db)
	ctx := context.Background()

	registerTeam(t, ss, ctx, "test-board")

	err := ss.SetSessionSleeping(ctx, "sess-lead", true)
	require.NoError(t, err)

	sessions, _ := ss.GetAllLiveSessions(ctx)
	sleepCount := 0
	for _, s := range sessions {
		if s.IsSleeping == 1 {
			sleepCount++
			assert.Equal(t, "sess-lead", s.SessionID)
		}
	}
	assert.Equal(t, 1, sleepCount, "only one session should be sleeping")
}

// ── Edge Cases ──────────────────────────────────────────────

func TestSleepNonexistentBoard(t *testing.T) {
	db := openTestDB(t)
	ss := NewSessionStore(db)
	ctx := context.Background()

	n, err := ss.SetBoardSleeping(ctx, "nonexistent", true)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestSleepNonexistentSession(t *testing.T) {
	db := openTestDB(t)
	ss := NewSessionStore(db)
	ctx := context.Background()

	err := ss.SetSessionSleeping(ctx, "nonexistent", true)
	require.NoError(t, err)
}
