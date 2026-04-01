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

// ── Task Tests ──────────────────────────────────────────────────────

func TestCreateTask(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Basic task creation
	task, err := s.CreateTask(ctx, "proj", "Implement feature X", "", "high", "alice")
	require.NoError(t, err)
	assert.Equal(t, "Implement feature X", task.Title)
	assert.Equal(t, "high", task.Priority)
	assert.Equal(t, "pending", task.Status)
	assert.Equal(t, "alice", task.CreatedBy)
	assert.Nil(t, task.AssignedTo)

	// Task with body
	task2, err := s.CreateTask(ctx, "proj", "With body", "Detailed instructions here", "medium", "alice")
	require.NoError(t, err)
	assert.Equal(t, "With body", task2.Title)
	require.NotNil(t, task2.Body)
	assert.Equal(t, "Detailed instructions here", *task2.Body)

	// Task with empty body
	task3, err := s.CreateTask(ctx, "proj", "No body", "", "medium", "alice")
	require.NoError(t, err)
	assert.Equal(t, "No body", task3.Title)

	// Default priority
	task4, err := s.CreateTask(ctx, "proj", "Default prio", "", "", "alice")
	require.NoError(t, err)
	assert.Equal(t, "medium", task4.Priority)
}

func TestListTasks(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "proj", "Low task", "", "low", "alice")
	s.CreateTask(ctx, "proj", "Critical task", "", "critical", "alice")
	s.CreateTask(ctx, "proj", "High task", "", "high", "alice")

	// All tasks returned, sorted by priority
	tasks, err := s.ListTasks(ctx, "proj")
	require.NoError(t, err)
	assert.Len(t, tasks, 3)
	assert.Equal(t, "critical", tasks[0].Priority)
	assert.Equal(t, "high", tasks[1].Priority)
	assert.Equal(t, "low", tasks[2].Priority)

	// Different project returns nothing
	tasks, err = s.ListTasks(ctx, "other")
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestClaimTask(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "proj", "Do work", "", "medium", "alice")

	// Claim succeeds — picks the only pending task
	claimed, err := s.ClaimTask(ctx, "proj", "bob")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, "in_progress", claimed.Status)
	require.NotNil(t, claimed.AssignedTo)
	assert.Equal(t, "bob", *claimed.AssignedTo)
	assert.NotNil(t, claimed.ClaimedAt)

	// No more tasks to claim
	task, err := s.ClaimTask(ctx, "proj", "charlie")
	require.NoError(t, err)
	assert.Nil(t, task)

	// Empty project returns nil
	task, err = s.ClaimTask(ctx, "empty", "bob")
	require.NoError(t, err)
	assert.Nil(t, task)
}

func TestClaimTask_PriorityOrder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "proj", "Low task", "", "low", "alice")
	s.CreateTask(ctx, "proj", "Critical task", "", "critical", "alice")
	s.CreateTask(ctx, "proj", "High task", "", "high", "alice")

	// Should claim critical first
	claimed, err := s.ClaimTask(ctx, "proj", "bob")
	require.NoError(t, err)
	assert.Equal(t, "Critical task", claimed.Title)

	// Then high
	claimed, err = s.ClaimTask(ctx, "proj", "bob")
	require.NoError(t, err)
	assert.Equal(t, "High task", claimed.Title)

	// Then low
	claimed, err = s.ClaimTask(ctx, "proj", "bob")
	require.NoError(t, err)
	assert.Equal(t, "Low task", claimed.Title)
}

func TestHasActiveTaskForAssignee(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	task1, err := s.CreateTask(ctx, "proj", "Task 1", "", "medium", "alice", "bob")
	require.NoError(t, err)
	_, err = s.ClaimTask(ctx, "proj", "bob")
	require.NoError(t, err)

	task2, err := s.CreateTask(ctx, "proj", "Task 2", "", "medium", "alice", "bob")
	require.NoError(t, err)

	hasActive, err := s.HasActiveTaskForAssignee(ctx, "proj", "bob", task2.ID)
	require.NoError(t, err)
	assert.True(t, hasActive)

	hasActive, err = s.HasActiveTaskForAssignee(ctx, "proj", "proj-other", task2.ID)
	require.NoError(t, err)
	assert.False(t, hasActive)

	hasActive, err = s.HasActiveTaskForAssignee(ctx, "proj", "bob", task1.ID)
	require.NoError(t, err)
	assert.False(t, hasActive)
}

func TestCompleteTask(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "proj", "Do work", "", "medium", "alice")
	claimed, _ := s.ClaimTask(ctx, "proj", "bob")

	msg := "All done"
	completed, err := s.CompleteTask(ctx, "proj", claimed.ID, "bob", &msg)
	require.NoError(t, err)
	assert.Equal(t, "completed", completed.Status)
	require.NotNil(t, completed.CompletedBy)
	assert.Equal(t, "bob", *completed.CompletedBy)
	require.NotNil(t, completed.CompletionMessage)
	assert.Equal(t, "All done", *completed.CompletionMessage)
	assert.NotNil(t, completed.CompletedAt)

	// Can't complete a task that's not in progress
	_, err = s.CompleteTask(ctx, "proj", claimed.ID, "bob", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be completed")
}

func TestTaskLifecycle_EndToEnd(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Create tasks with different priorities
	s.CreateTask(ctx, "proj", "Critical work", "", "critical", "alice")
	s.CreateTask(ctx, "proj", "Medium work", "", "medium", "alice")
	s.CreateTask(ctx, "proj", "Low work", "", "low", "alice")

	// All 3 present
	tasks, _ := s.ListTasks(ctx, "proj")
	assert.Len(t, tasks, 3)

	// Claim and complete all via ClaimTask (auto-selects by priority)
	for i := 0; i < 3; i++ {
		claimed, err := s.ClaimTask(ctx, "proj", "bob")
		require.NoError(t, err)
		require.NotNil(t, claimed)
		_, err = s.CompleteTask(ctx, "proj", claimed.ID, "bob", nil)
		require.NoError(t, err)
	}

	// Verify all completed
	tasks, _ = s.ListTasks(ctx, "proj")
	assert.Len(t, tasks, 3)
	for _, task := range tasks {
		assert.Equal(t, "completed", task.Status)
	}

	// No more to claim
	task, err := s.ClaimTask(ctx, "proj", "bob")
	require.NoError(t, err)
	assert.Nil(t, task)
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
