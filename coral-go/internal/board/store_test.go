package board

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "board_test.db")
	s, err := NewStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

// sub is a test helper that calls Subscribe with subscriberID used as both subscriber and session name.
func sub(s *Store, ctx context.Context, project, subscriberID, jobTitle string) (*Subscriber, error) {
	return s.Subscribe(ctx, project, subscriberID, jobTitle, "tmux-"+subscriberID, nil, nil, "")
}

func TestSubscribeAndList(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	result, err := sub(s, ctx, "myproject", "agent-1", "Lead Dev")
	require.NoError(t, err)
	assert.Equal(t, "Lead Dev", result.JobTitle)
	assert.Equal(t, "myproject", result.Project)

	subs, err := s.ListSubscribers(ctx, "myproject")
	require.NoError(t, err)
	assert.Len(t, subs, 1)

	// Upsert same subscriber updates job_title
	sub2, err := sub(s, ctx, "myproject", "agent-1", "Senior Dev")
	require.NoError(t, err)
	assert.Equal(t, "Senior Dev", sub2.JobTitle)

	subs, _ = s.ListSubscribers(ctx, "myproject")
	assert.Len(t, subs, 1) // Still just one

	// Unsubscribe
	removed, err := s.Unsubscribe(ctx, "myproject", "agent-1")
	require.NoError(t, err)
	assert.True(t, removed)

	removed, _ = s.Unsubscribe(ctx, "myproject", "agent-1")
	assert.False(t, removed) // Already inactive
}

func TestGetSubscription(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	result, err := s.GetSubscription(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, result)

	sub(s, ctx, "proj", "agent-1", "Dev")
	result, err = s.GetSubscription(ctx, "agent-1")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "proj", result.Project)
}

func TestPostAndReadMessages(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sub(s, ctx, "proj", "agent-1", "Dev A")
	sub(s, ctx, "proj", "agent-2", "Dev B")

	// Agent-1 posts
	msg, err := s.PostMessage(ctx, "proj", "agent-1", "Hello team!", nil)
	require.NoError(t, err)
	assert.Equal(t, "Hello team!", msg.Content)

	// Agent-2 reads — should see agent-1's message
	messages, err := s.ReadMessages(ctx, "proj", "agent-2", 50)
	require.NoError(t, err)
	assert.Len(t, messages, 1)
	assert.Equal(t, "Hello team!", messages[0].Content)
	assert.Equal(t, "Dev A", messages[0].JobTitle)

	// Agent-1 reads — should NOT see own message
	messages, err = s.ReadMessages(ctx, "proj", "agent-1", 50)
	require.NoError(t, err)
	assert.Empty(t, messages)

	// Agent-2 reads again — cursor advanced, nothing new
	messages, err = s.ReadMessages(ctx, "proj", "agent-2", 50)
	require.NoError(t, err)
	assert.Empty(t, messages)
}

func TestListMessages(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sub(s, ctx, "proj", "agent-1", "Dev")
	s.PostMessage(ctx, "proj", "agent-1", "msg 1", nil)
	s.PostMessage(ctx, "proj", "agent-1", "msg 2", nil)

	messages, err := s.ListMessages(ctx, "proj", 100, 0, 0)
	require.NoError(t, err)
	assert.Len(t, messages, 2)

	count, err := s.CountMessages(ctx, "proj")
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestCheckUnread(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sub(s, ctx, "proj", "agent-1", "Dev A")
	sub(s, ctx, "proj", "agent-2", "Dev B")

	// Post without mention — agent-2 should have 0 unread
	s.PostMessage(ctx, "proj", "agent-1", "Just a regular message", nil)
	count, err := s.CheckUnread(ctx, "proj", "agent-2")
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Post with @all mention
	s.PostMessage(ctx, "proj", "agent-1", "@notify-all Important update!", nil)
	count, err = s.CheckUnread(ctx, "proj", "agent-2")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Post with @job_title mention
	s.PostMessage(ctx, "proj", "agent-1", "@Dev B please review this", nil)
	count, err = s.CheckUnread(ctx, "proj", "agent-2")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Own messages don't count
	count, _ = s.CheckUnread(ctx, "proj", "agent-1")
	assert.Equal(t, 0, count)
}

func TestGetAllUnreadCounts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sub(s, ctx, "proj", "agent-1", "Dev A")
	sub(s, ctx, "proj", "agent-2", "Dev B")

	s.PostMessage(ctx, "proj", "agent-1", "@all hello", nil)

	counts, err := s.GetAllUnreadCounts(ctx)
	require.NoError(t, err)
	// Counts are keyed by session_name (tmux-<subscriberID>)
	assert.Equal(t, 0, counts["tmux-agent-1"]) // Own message
	assert.Equal(t, 1, counts["tmux-agent-2"]) // Mentioned
}

func TestDeleteMessage(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sub(s, ctx, "proj", "agent-1", "Dev")
	msg, _ := s.PostMessage(ctx, "proj", "agent-1", "delete me", nil)

	removed, err := s.DeleteMessage(ctx, msg.ID)
	require.NoError(t, err)
	assert.True(t, removed)

	count, _ := s.CountMessages(ctx, "proj")
	assert.Equal(t, 0, count)
}

func TestListProjects(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sub(s, ctx, "proj-a", "agent-1", "Dev")
	sub(s, ctx, "proj-b", "agent-2", "Dev")
	s.PostMessage(ctx, "proj-a", "agent-1", "hello", nil)

	projects, err := s.ListProjects(ctx)
	require.NoError(t, err)
	assert.Len(t, projects, 2)
}

func TestGetSubscription_MultipleBoards(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Subscribe the same subscriber_id ("Orchestrator") to two different boards.
	// This simulates the stale subscription bug: same role on multiple teams.
	_, err := s.Subscribe(ctx, "board-A", "Orchestrator", "Orchestrator", "tmux-session-A", nil, nil, "")
	require.NoError(t, err)
	_, err = s.Subscribe(ctx, "board-B", "Orchestrator", "Orchestrator", "tmux-session-B", nil, nil, "")
	require.NoError(t, err)

	// GetSubscription by subscriber_id returns *some* active subscription.
	// When timestamps collide (same second), which board is returned is not
	// guaranteed — this is exactly why GetSubscriptionBySessionName was added.
	result, err := s.GetSubscription(ctx, "Orchestrator")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Orchestrator", result.SubscriberID)
	assert.Contains(t, []string{"board-A", "board-B"}, result.Project,
		"GetSubscription should return one of the active subscriptions")

	// GetSubscriptionBySessionName is the precise lookup — always returns
	// the exact board for the given tmux session.
	resultA, err := s.GetSubscriptionBySessionName(ctx, "tmux-session-A")
	require.NoError(t, err)
	require.NotNil(t, resultA)
	assert.Equal(t, "board-A", resultA.Project)
	assert.Equal(t, "Orchestrator", resultA.SubscriberID)

	resultB, err := s.GetSubscriptionBySessionName(ctx, "tmux-session-B")
	require.NoError(t, err)
	require.NotNil(t, resultB)
	assert.Equal(t, "board-B", resultB.Project)
	assert.Equal(t, "Orchestrator", resultB.SubscriberID)

	// GetSubscriptionBySessionName for nonexistent session returns nil
	resultNone, err := s.GetSubscriptionBySessionName(ctx, "tmux-nonexistent")
	require.NoError(t, err)
	assert.Nil(t, resultNone)
}

func TestDeleteProject(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sub(s, ctx, "proj", "agent-1", "Dev")
	s.PostMessage(ctx, "proj", "agent-1", "hello", nil)

	err := s.DeleteProject(ctx, "proj")
	require.NoError(t, err)

	subs, _ := s.ListSubscribers(ctx, "proj")
	assert.Empty(t, subs)
	count, _ := s.CountMessages(ctx, "proj")
	assert.Equal(t, 0, count)
}
