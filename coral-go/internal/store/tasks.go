package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
)

// AgentTask represents a checklist item for an agent.
type AgentTask struct {
	ID        int64   `db:"id" json:"id"`
	AgentName string  `db:"agent_name" json:"agent_name"`
	SessionID *string `db:"session_id" json:"session_id,omitempty"`
	Title     string  `db:"title" json:"title"`
	Completed int     `db:"completed" json:"completed"`
	SortOrder int     `db:"sort_order" json:"sort_order"`
	CreatedAt string  `db:"created_at" json:"created_at"`
	UpdatedAt string  `db:"updated_at" json:"updated_at"`
}

// AgentNote represents a note for an agent.
type AgentNote struct {
	ID        int64   `db:"id" json:"id"`
	AgentName string  `db:"agent_name" json:"agent_name"`
	SessionID *string `db:"session_id" json:"session_id,omitempty"`
	Content   string  `db:"content" json:"content"`
	CreatedAt string  `db:"created_at" json:"created_at"`
	UpdatedAt string  `db:"updated_at" json:"updated_at"`
}

// AgentEvent represents an event recorded for an agent.
type AgentEvent struct {
	ID         int64   `db:"id" json:"id"`
	AgentName  string  `db:"agent_name" json:"agent_name"`
	SessionID  *string `db:"session_id" json:"session_id,omitempty"`
	EventType  string  `db:"event_type" json:"event_type"`
	ToolName   *string `db:"tool_name" json:"tool_name,omitempty"`
	Summary    string  `db:"summary" json:"summary"`
	DetailJSON *string `db:"detail_json" json:"detail_json,omitempty"`
	CreatedAt  string  `db:"created_at" json:"created_at"`
}

// ToolCount holds a tool name and its usage count.
type ToolCount struct {
	ToolName string `db:"tool_name" json:"tool_name"`
	Count    int    `db:"count" json:"count"`
}

// TaskStore provides agent tasks, notes, and events operations.
type TaskStore struct {
	db *DB
}

// NewTaskStore creates a new TaskStore.
func NewTaskStore(db *DB) *TaskStore {
	return &TaskStore{db: db}
}

// ── Agent Tasks ────────────────────────────────────────────────────────

// ListAgentTasks returns tasks for an agent, optionally scoped by session.
func (s *TaskStore) ListAgentTasks(ctx context.Context, agentName string, sessionID *string) ([]AgentTask, error) {
	filter, filterArgs := sessionFilter(sessionID)
	args := append([]interface{}{agentName}, filterArgs...)
	var tasks []AgentTask
	err := s.db.SelectContext(ctx, &tasks,
		`SELECT id, agent_name, title, completed, sort_order, created_at, updated_at
		 FROM agent_tasks WHERE agent_name = ?`+filter+` ORDER BY sort_order`,
		args...)
	return tasks, err
}

// CreateAgentTask creates a new task with auto-incrementing sort order.
func (s *TaskStore) CreateAgentTask(ctx context.Context, agentName, title string, sessionID *string) (*AgentTask, error) {
	now := nowUTC()

	// Get next sort order
	filter, filterArgs := sessionFilter(sessionID)
	var nextOrder int
	err := s.db.GetContext(ctx, &nextOrder,
		"SELECT COALESCE(MAX(sort_order), -1) + 1 FROM agent_tasks WHERE agent_name = ?"+filter,
		append([]interface{}{agentName}, filterArgs...)...)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_tasks (agent_name, session_id, title, sort_order, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		agentName, sessionID, title, nextOrder, now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get task insert ID: %w", err)
	}
	return &AgentTask{
		ID: id, AgentName: agentName, Title: title,
		Completed: 0, SortOrder: nextOrder,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// UpdateAgentTask updates task fields (title, completed, sort_order).
func (s *TaskStore) UpdateAgentTask(ctx context.Context, taskID int64, title *string, completed *int, sortOrder *int) error {
	now := nowUTC()
	sets := []string{"updated_at = ?"}
	args := []interface{}{now}
	if title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *title)
	}
	if completed != nil {
		sets = append(sets, "completed = ?")
		args = append(args, *completed)
	}
	if sortOrder != nil {
		sets = append(sets, "sort_order = ?")
		args = append(args, *sortOrder)
	}
	args = append(args, taskID)
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf("UPDATE agent_tasks SET %s WHERE id = ?", strings.Join(sets, ", ")),
		args...)
	return err
}

// CompleteAgentTaskByTitle marks a task as completed by title match.
func (s *TaskStore) CompleteAgentTaskByTitle(ctx context.Context, agentName, title string, sessionID *string) error {
	now := nowUTC()
	filter, filterArgs := sessionFilter(sessionID)
	args := append([]interface{}{now, agentName, title}, filterArgs...)
	_, err := s.db.ExecContext(ctx,
		`UPDATE agent_tasks SET completed = 1, updated_at = ?
		 WHERE agent_name = ? AND title = ?`+filter+` AND completed = 0`,
		args...)
	return err
}

// DeleteAgentTask deletes a task by ID.
func (s *TaskStore) DeleteAgentTask(ctx context.Context, taskID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM agent_tasks WHERE id = ?", taskID)
	return err
}

// ReorderAgentTasks sets sort_order based on the provided ID order.
func (s *TaskStore) ReorderAgentTasks(ctx context.Context, agentName string, taskIDs []int64) error {
	now := nowUTC()
	return s.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		for idx, tid := range taskIDs {
			if _, err := tx.ExecContext(ctx,
				"UPDATE agent_tasks SET sort_order = ?, updated_at = ? WHERE id = ? AND agent_name = ?",
				idx, now, tid, agentName); err != nil {
				return err
			}
		}
		return nil
	})
}

// ── Agent Notes ────────────────────────────────────────────────────────

// ListAgentNotes returns notes for an agent, optionally scoped by session.
func (s *TaskStore) ListAgentNotes(ctx context.Context, agentName string, sessionID *string) ([]AgentNote, error) {
	filter, filterArgs := sessionFilter(sessionID)
	args := append([]interface{}{agentName}, filterArgs...)
	var notes []AgentNote
	err := s.db.SelectContext(ctx, &notes,
		`SELECT id, agent_name, content, created_at, updated_at
		 FROM agent_notes WHERE agent_name = ?`+filter+` ORDER BY created_at DESC`,
		args...)
	return notes, err
}

// CreateAgentNote creates a new note.
func (s *TaskStore) CreateAgentNote(ctx context.Context, agentName, content string, sessionID *string) (*AgentNote, error) {
	now := nowUTC()
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_notes (agent_name, session_id, content, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		agentName, sessionID, content, now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get note insert ID: %w", err)
	}
	return &AgentNote{
		ID: id, AgentName: agentName, Content: content,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// UpdateAgentNote updates note content.
func (s *TaskStore) UpdateAgentNote(ctx context.Context, noteID int64, content string) error {
	now := nowUTC()
	_, err := s.db.ExecContext(ctx,
		"UPDATE agent_notes SET content = ?, updated_at = ? WHERE id = ?",
		content, now, noteID)
	return err
}

// DeleteAgentNote deletes a note by ID.
func (s *TaskStore) DeleteAgentNote(ctx context.Context, noteID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM agent_notes WHERE id = ?", noteID)
	return err
}

// ── Agent Events ──────────────────────────────────────────────────────

// InsertAgentEvent inserts an event and auto-prunes to 500 per agent.
func (s *TaskStore) InsertAgentEvent(ctx context.Context, event *AgentEvent) (*AgentEvent, error) {
	now := nowUTC()
	event.CreatedAt = now

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_events (agent_name, session_id, event_type, tool_name, summary, detail_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		event.AgentName, event.SessionID, event.EventType, event.ToolName,
		event.Summary, event.DetailJSON, now)
	if err != nil {
		return nil, err
	}
	if id, err := result.LastInsertId(); err != nil {
		log.Printf("[store] get event insert ID: %v", err)
	} else {
		event.ID = id
	}

	// Auto-prune to 500 events per agent (best-effort)
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM agent_events WHERE agent_name = ? AND id NOT IN
		 (SELECT id FROM agent_events WHERE agent_name = ? ORDER BY id DESC LIMIT 500)`,
		event.AgentName, event.AgentName); err != nil {
		log.Printf("[store] event prune failed for %s: %v", event.AgentName, err)
	}

	return event, nil
}

// ListAgentEvents returns recent events for an agent.
// If agentName is empty, events are not filtered by agent (useful for history queries by session).
func (s *TaskStore) ListAgentEvents(ctx context.Context, agentName string, limit int, sessionID *string) ([]AgentEvent, error) {
	var whereClauses []string
	var args []interface{}
	if agentName != "" {
		whereClauses = append(whereClauses, "agent_name = ?")
		args = append(args, agentName)
	}
	if sessionID != nil {
		whereClauses = append(whereClauses, "session_id = ?")
		args = append(args, *sessionID)
	}
	where := ""
	if len(whereClauses) > 0 {
		where = " WHERE " + strings.Join(whereClauses, " AND ")
	}
	args = append(args, limit)
	var events []AgentEvent
	err := s.db.SelectContext(ctx, &events,
		`SELECT id, agent_name, session_id, event_type, tool_name, summary, detail_json, created_at
		 FROM agent_events`+where+` ORDER BY created_at DESC LIMIT ?`,
		args...)
	return events, err
}

// GetAgentEventCounts returns tool usage counts for an agent.
// If agentName is empty, counts are not filtered by agent.
func (s *TaskStore) GetAgentEventCounts(ctx context.Context, agentName string, sessionID *string) ([]ToolCount, error) {
	var whereClauses []string
	var args []interface{}
	if agentName != "" {
		whereClauses = append(whereClauses, "agent_name = ?")
		args = append(args, agentName)
	}
	if sessionID != nil {
		whereClauses = append(whereClauses, "session_id = ?")
		args = append(args, *sessionID)
	}
	whereClauses = append(whereClauses, "tool_name IS NOT NULL")
	where := " WHERE " + strings.Join(whereClauses, " AND ")
	var counts []ToolCount
	err := s.db.SelectContext(ctx, &counts,
		`SELECT tool_name, COUNT(*) as count FROM agent_events`+where+`
		 GROUP BY tool_name ORDER BY count DESC`,
		args...)
	return counts, err
}

// GetLatestEventTypes returns the latest (event_type, summary) per session (excluding status/goal/confidence).
func (s *TaskStore) GetLatestEventTypes(ctx context.Context, sessionIDs []string) (map[string][2]string, error) {
	if len(sessionIDs) == 0 {
		return map[string][2]string{}, nil
	}
	query, args, err := sqlx.In(
		`SELECT session_id, event_type, summary FROM agent_events
		 WHERE session_id IN (?) AND event_type NOT IN ('status', 'goal', 'confidence')
		 ORDER BY created_at DESC`,
		sessionIDs)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		SessionID string `db:"session_id"`
		EventType string `db:"event_type"`
		Summary   string `db:"summary"`
	}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	result := make(map[string][2]string)
	for _, r := range rows {
		if _, ok := result[r.SessionID]; !ok {
			result[r.SessionID] = [2]string{r.EventType, r.Summary}
		}
	}
	return result, nil
}

// GetLatestGoals returns the latest goal summary per session.
func (s *TaskStore) GetLatestGoals(ctx context.Context, sessionIDs []string) (map[string]string, error) {
	if len(sessionIDs) == 0 {
		return map[string]string{}, nil
	}
	query, args, err := sqlx.In(
		`SELECT session_id, summary FROM agent_events
		 WHERE session_id IN (?) AND event_type = 'goal'
		 ORDER BY created_at DESC`,
		sessionIDs)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		SessionID string `db:"session_id"`
		Summary   string `db:"summary"`
	}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, r := range rows {
		if _, ok := result[r.SessionID]; !ok {
			result[r.SessionID] = r.Summary
		}
	}
	return result, nil
}

// ClearAgentEvents deletes all events for an agent, optionally scoped by session.
func (s *TaskStore) ClearAgentEvents(ctx context.Context, agentName string, sessionID *string) error {
	filter, filterArgs := sessionFilter(sessionID)
	args := append([]interface{}{agentName}, filterArgs...)
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM agent_events WHERE agent_name = ?"+filter,
		args...)
	return err
}

// GetFileEdits returns a map of relative file paths to the agents that edited them,
// derived from Write/Edit tool_use events in agent_events.
// The workingDir is used to convert absolute paths in detail_json to repo-relative paths.
func (s *TaskStore) GetFileEdits(ctx context.Context, sessionID, workingDir string) (map[string][]FileAgent, error) {
	var rows []struct {
		AgentName    string `db:"agent_name"`
		FilePath     string `db:"file_path"`
		LastEditedAt string `db:"last_edited_at"`
	}
	err := s.db.SelectContext(ctx, &rows,
		`SELECT agent_name,
		        json_extract(detail_json, '$.file_path') as file_path,
		        MAX(created_at) as last_edited_at
		 FROM agent_events
		 WHERE session_id = ?
		   AND event_type = 'tool_use'
		   AND tool_name IN ('Write', 'Edit')
		   AND detail_json IS NOT NULL
		 GROUP BY agent_name, json_extract(detail_json, '$.file_path')`,
		sessionID)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]FileAgent, len(rows))
	for _, r := range rows {
		if r.FilePath == "" {
			continue
		}
		// Convert absolute path to repo-relative
		relPath := r.FilePath
		if workingDir != "" && filepath.IsAbs(relPath) {
			if rel, err := filepath.Rel(workingDir, relPath); err == nil {
				relPath = rel
			}
		}
		result[relPath] = append(result[relPath], FileAgent{
			Name:         r.AgentName,
			LastEditedAt: r.LastEditedAt,
		})
	}
	return result, nil
}

// ── History queries (by session_id only) ────────────────────────────────

// ListTasksBySession returns tasks for a historical session.
func (s *TaskStore) ListTasksBySession(ctx context.Context, sessionID string) ([]AgentTask, error) {
	var tasks []AgentTask
	err := s.db.SelectContext(ctx, &tasks,
		`SELECT id, agent_name, title, completed, sort_order, created_at, updated_at
		 FROM agent_tasks WHERE session_id = ? ORDER BY sort_order`, sessionID)
	return tasks, err
}

// ListNotesBySession returns notes for a historical session.
func (s *TaskStore) ListNotesBySession(ctx context.Context, sessionID string) ([]AgentNote, error) {
	var notes []AgentNote
	err := s.db.SelectContext(ctx, &notes,
		`SELECT id, agent_name, content, created_at, updated_at
		 FROM agent_notes WHERE session_id = ? ORDER BY created_at DESC`, sessionID)
	return notes, err
}

