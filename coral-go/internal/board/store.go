// Package board provides the message board store and HTTP handlers.
// It uses a separate SQLite database from the main Coral store.
package board

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// Subscriber represents a board subscriber.
type Subscriber struct {
	ID           int64   `db:"id" json:"id"`
	Project      string  `db:"project" json:"project"`
	SessionID    string  `db:"session_id" json:"session_id"`
	JobTitle     string  `db:"job_title" json:"job_title"`
	WebhookURL   *string `db:"webhook_url" json:"webhook_url"`
	OriginServer *string `db:"origin_server" json:"origin_server"`
	ReceiveMode  string  `db:"receive_mode" json:"receive_mode"`
	LastReadID   int64   `db:"last_read_id" json:"last_read_id"`
	SubscribedAt string  `db:"subscribed_at" json:"subscribed_at"`
}

// GroupInfo holds group summary info.
type GroupInfo struct {
	GroupID     string `db:"group_id" json:"group_id"`
	MemberCount int   `db:"member_count" json:"member_count"`
}

// Message represents a board message.
type Message struct {
	ID             int64   `db:"id" json:"id"`
	Project        string  `db:"project" json:"project"`
	SessionID      string  `db:"session_id" json:"session_id"`
	Content        string  `db:"content" json:"content"`
	CreatedAt      string  `db:"created_at" json:"created_at"`
	JobTitle       string  `db:"job_title" json:"job_title,omitempty"`
	TargetGroupID  *string `db:"target_group_id" json:"target_group_id,omitempty"`
}

// ProjectInfo holds project summary info.
type ProjectInfo struct {
	Project         string `db:"project" json:"project"`
	SubscriberCount int    `db:"subscriber_count" json:"subscriber_count"`
	MessageCount    int    `db:"message_count" json:"message_count"`
}

// Store provides message board operations with its own SQLite database.
type Store struct {
	db *sqlx.DB
}

// NewStore creates a new board Store with its own database.
func NewStore(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create board db directory: %w", err)
	}

	db, err := sqlx.Open("sqlite", dbPath+"?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-8000)")
	if err != nil {
		return nil, fmt.Errorf("open board database: %w", err)
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.ensureSchema(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) ensureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS board_subscribers (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			project       TEXT NOT NULL,
			session_id    TEXT NOT NULL,
			job_title     TEXT NOT NULL,
			webhook_url   TEXT,
			origin_server TEXT,
			receive_mode  TEXT NOT NULL DEFAULT 'mentions',
			last_read_id  INTEGER NOT NULL DEFAULT 0,
			subscribed_at TEXT NOT NULL,
			UNIQUE(project, session_id)
		);
		CREATE TABLE IF NOT EXISTS board_messages (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			project     TEXT NOT NULL,
			session_id  TEXT NOT NULL,
			content     TEXT NOT NULL,
			created_at  TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_board_messages_project ON board_messages(project, id);
		CREATE TABLE IF NOT EXISTS board_groups (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			project    TEXT NOT NULL,
			group_id   TEXT NOT NULL,
			session_id TEXT NOT NULL,
			UNIQUE(project, group_id, session_id)
		);
		CREATE INDEX IF NOT EXISTS idx_board_groups_project_group ON board_groups(project, group_id);
	`)
	if err != nil {
		return err
	}
	// Migrations for existing DBs
	s.db.ExecContext(ctx, "ALTER TABLE board_subscribers ADD COLUMN receive_mode TEXT NOT NULL DEFAULT 'mentions'")
	s.db.ExecContext(ctx, "ALTER TABLE board_messages ADD COLUMN target_group_id TEXT")
	return nil
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// ── Subscribers ──────────────────────────────────────────────────────

// Subscribe adds or updates a subscriber on a board project.
func (s *Store) Subscribe(ctx context.Context, project, sessionID, jobTitle string, webhookURL, originServer *string, receiveMode string) (*Subscriber, error) {
	if receiveMode == "" {
		receiveMode = "mentions"
	}
	now := nowUTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO board_subscribers (project, session_id, job_title, webhook_url, origin_server, receive_mode, subscribed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(project, session_id) DO UPDATE SET
		     job_title = excluded.job_title,
		     webhook_url = excluded.webhook_url,
		     origin_server = excluded.origin_server,
		     receive_mode = excluded.receive_mode`,
		project, sessionID, jobTitle, webhookURL, originServer, receiveMode, now)
	if err != nil {
		return nil, err
	}
	var sub Subscriber
	err = s.db.GetContext(ctx, &sub,
		"SELECT * FROM board_subscribers WHERE project = ? AND session_id = ?",
		project, sessionID)
	return &sub, err
}

// Unsubscribe removes a subscriber. Returns true if a row was deleted.
func (s *Store) Unsubscribe(ctx context.Context, project, sessionID string) (bool, error) {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM board_subscribers WHERE project = ? AND session_id = ?",
		project, sessionID)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

// ListSubscribers returns all subscribers for a project.
func (s *Store) ListSubscribers(ctx context.Context, project string) ([]Subscriber, error) {
	var subs []Subscriber
	err := s.db.SelectContext(ctx, &subs,
		"SELECT * FROM board_subscribers WHERE project = ? ORDER BY subscribed_at", project)
	return subs, err
}

// GetSubscription returns the active subscription for a session.
func (s *Store) GetSubscription(ctx context.Context, sessionID string) (*Subscriber, error) {
	var sub Subscriber
	err := s.db.GetContext(ctx, &sub,
		"SELECT * FROM board_subscribers WHERE session_id = ? LIMIT 1", sessionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &sub, err
}

// GetAllSubscriptions returns all subscriptions keyed by session_id.
func (s *Store) GetAllSubscriptions(ctx context.Context) (map[string]*Subscriber, error) {
	var subs []Subscriber
	err := s.db.SelectContext(ctx, &subs, "SELECT * FROM board_subscribers")
	if err != nil {
		return nil, err
	}
	result := make(map[string]*Subscriber, len(subs))
	for i := range subs {
		result[subs[i].SessionID] = &subs[i]
	}
	return result, nil
}

// TransferSubscription transfers a subscription from one session to another,
// preserving last_read_id. Used during session resume when the session_id
// changes but the agent should not re-read old messages.
func (s *Store) TransferSubscription(ctx context.Context, project, oldSessionID, newSessionID string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var old Subscriber
	err = tx.GetContext(ctx, &old,
		"SELECT last_read_id, job_title, webhook_url, origin_server, receive_mode FROM board_subscribers WHERE project = ? AND session_id = ?",
		project, oldSessionID)
	if err == sql.ErrNoRows {
		return nil // Nothing to transfer
	}
	if err != nil {
		return err
	}

	now := nowUTC()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO board_subscribers (project, session_id, job_title, webhook_url, origin_server, receive_mode, last_read_id, subscribed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(project, session_id)
		 DO UPDATE SET job_title = excluded.job_title,
		               webhook_url = excluded.webhook_url,
		               origin_server = excluded.origin_server,
		               receive_mode = excluded.receive_mode,
		               last_read_id = excluded.last_read_id`,
		project, newSessionID, old.JobTitle, old.WebhookURL,
		old.OriginServer, old.ReceiveMode, old.LastReadID, now)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx,
		"DELETE FROM board_subscribers WHERE project = ? AND session_id = ?",
		project, oldSessionID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ── Messages ─────────────────────────────────────────────────────────

// PostMessage posts a new message to a project board.
func (s *Store) PostMessage(ctx context.Context, project, sessionID, content string, targetGroupID *string) (*Message, error) {
	now := nowUTC()
	result, err := s.db.ExecContext(ctx,
		"INSERT INTO board_messages (project, session_id, content, target_group_id, created_at) VALUES (?, ?, ?, ?, ?)",
		project, sessionID, content, targetGroupID, now)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	msg := &Message{ID: id, Project: project, SessionID: sessionID, Content: content, CreatedAt: now}
	if targetGroupID != nil {
		msg.TargetGroupID = targetGroupID
	}
	return msg, nil
}

// ReadMessages returns unread messages for a subscriber (cursor-based).
func (s *Store) ReadMessages(ctx context.Context, project, sessionID string, limit int) ([]Message, error) {
	// Get subscriber cursor
	var lastReadID int64
	err := s.db.GetContext(ctx, &lastReadID,
		"SELECT last_read_id FROM board_subscribers WHERE project = ? AND session_id = ?",
		project, sessionID)
	if err != nil {
		return nil, nil // Not subscribed
	}

	// Fetch new messages from others
	var messages []Message
	err = s.db.SelectContext(ctx, &messages,
		`SELECT m.id, m.project, m.session_id, m.content, m.created_at,
		        COALESCE(s.job_title, 'Unknown') as job_title
		 FROM board_messages m
		 LEFT JOIN board_subscribers s ON m.project = s.project AND m.session_id = s.session_id
		 WHERE m.project = ? AND m.id > ? AND m.session_id != ?
		 ORDER BY m.id ASC LIMIT ?`,
		project, lastReadID, sessionID, limit)
	if err != nil {
		return nil, err
	}

	// Advance cursor past returned messages and own messages
	newCursor := lastReadID
	if len(messages) > 0 {
		for _, m := range messages {
			if m.ID > newCursor {
				newCursor = m.ID
			}
		}
	}
	// Skip past own messages
	var ownMax int64
	s.db.GetContext(ctx, &ownMax,
		"SELECT COALESCE(MAX(id), 0) FROM board_messages WHERE project = ? AND session_id = ?",
		project, sessionID)
	if ownMax > newCursor {
		newCursor = ownMax
	}

	if newCursor > lastReadID {
		s.db.ExecContext(ctx,
			"UPDATE board_subscribers SET last_read_id = ? WHERE project = ? AND session_id = ?",
			newCursor, project, sessionID)
	}

	return messages, nil
}

// ListMessages returns recent messages (no cursor, no side effects).
// If beforeID > 0, only messages with id < beforeID are returned (keyset pagination).
func (s *Store) ListMessages(ctx context.Context, project string, limit, offset int, beforeID int64) ([]Message, error) {
	var messages []Message
	var err error
	if beforeID > 0 {
		err = s.db.SelectContext(ctx, &messages,
			`SELECT m.id, m.project, m.session_id, m.content, m.created_at,
			        COALESCE(s.job_title, 'Unknown') as job_title,
			        m.target_group_id
			 FROM board_messages m
			 LEFT JOIN board_subscribers s ON m.project = s.project AND m.session_id = s.session_id
			 WHERE m.project = ? AND m.id < ?
			 ORDER BY m.id ASC LIMIT ? OFFSET ?`,
			project, beforeID, limit, offset)
	} else {
		err = s.db.SelectContext(ctx, &messages,
			`SELECT m.id, m.project, m.session_id, m.content, m.created_at,
			        COALESCE(s.job_title, 'Unknown') as job_title,
			        m.target_group_id
			 FROM board_messages m
			 LEFT JOIN board_subscribers s ON m.project = s.project AND m.session_id = s.session_id
			 WHERE m.project = ?
			 ORDER BY m.id ASC LIMIT ? OFFSET ?`,
			project, limit, offset)
	}
	return messages, err
}

// CountMessages returns the total message count for a project.
func (s *Store) CountMessages(ctx context.Context, project string) (int, error) {
	var count int
	err := s.db.GetContext(ctx, &count,
		"SELECT COUNT(*) FROM board_messages WHERE project = ?", project)
	return count, err
}

// CheckUnread returns the count of unread messages based on the subscriber's receive_mode.
//
// Modes:
//   - "none"     → always 0
//   - "all"      → all unread messages from others
//   - "mentions" → only messages with @notify-all, @<session_id>, or @<job_title>
//   - anything else → treat as group-id, count only messages from group members
func (s *Store) CheckUnread(ctx context.Context, project, sessionID string) (int, error) {
	var sub struct {
		LastReadID  int64  `db:"last_read_id"`
		JobTitle    string `db:"job_title"`
		ReceiveMode string `db:"receive_mode"`
	}
	err := s.db.GetContext(ctx, &sub,
		"SELECT last_read_id, job_title, receive_mode FROM board_subscribers WHERE project = ? AND session_id = ?",
		project, sessionID)
	if err != nil {
		return 0, nil
	}

	receiveMode := sub.ReceiveMode
	if receiveMode == "" {
		receiveMode = "mentions"
	}

	if receiveMode == "none" {
		return 0, nil
	}

	if receiveMode == "all" {
		var count int
		err := s.db.GetContext(ctx, &count,
			`SELECT COUNT(*) FROM board_messages WHERE project = ? AND id > ? AND session_id != ?`,
			project, sub.LastReadID, sessionID)
		return count, err
	}

	if receiveMode == "mentions" {
		patterns := []string{
			"%@notify-all%", "%@notify_all%", "%@notifyall%", "%@all%",
			fmt.Sprintf("%%@%s%%", sessionID),
		}
		if sub.JobTitle != "" {
			patterns = append(patterns, fmt.Sprintf("%%@%s%%", sub.JobTitle))
		}

		whereClauses := make([]string, len(patterns))
		args := []interface{}{project, sub.LastReadID, sessionID}
		for i, p := range patterns {
			whereClauses[i] = "content LIKE ? COLLATE NOCASE"
			args = append(args, p)
		}

		var count int
		query := fmt.Sprintf(
			`SELECT COUNT(*) FROM board_messages
			 WHERE project = ? AND id > ? AND session_id != ? AND (%s)`,
			strings.Join(whereClauses, " OR "))
		err = s.db.GetContext(ctx, &count, query, args...)
		return count, err
	}

	// Group-based mode: count messages from group members only
	var memberIDs []string
	err = s.db.SelectContext(ctx, &memberIDs,
		"SELECT session_id FROM board_groups WHERE project = ? AND group_id = ?",
		project, receiveMode)
	if err != nil || len(memberIDs) == 0 {
		return 0, nil
	}

	placeholders := make([]string, len(memberIDs))
	args := []interface{}{project, sub.LastReadID, sessionID}
	for i, id := range memberIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	var count int
	query := fmt.Sprintf(
		`SELECT COUNT(*) FROM board_messages
		 WHERE project = ? AND id > ? AND session_id != ? AND session_id IN (%s)`,
		strings.Join(placeholders, ","))
	err = s.db.GetContext(ctx, &count, query, args...)
	return count, err
}

// GetAllUnreadCounts returns unread counts for all subscribers, respecting each subscriber's receive_mode.
func (s *Store) GetAllUnreadCounts(ctx context.Context) (map[string]int, error) {
	var subs []struct {
		Project     string `db:"project"`
		SessionID   string `db:"session_id"`
		JobTitle    string `db:"job_title"`
		LastReadID  int64  `db:"last_read_id"`
		ReceiveMode string `db:"receive_mode"`
	}
	err := s.db.SelectContext(ctx, &subs,
		"SELECT project, session_id, job_title, last_read_id, receive_mode FROM board_subscribers")
	if err != nil || len(subs) == 0 {
		return map[string]int{}, nil
	}

	// Pre-load all group memberships
	var groupRows []struct {
		Project   string `db:"project"`
		GroupID   string `db:"group_id"`
		SessionID string `db:"session_id"`
	}
	s.db.SelectContext(ctx, &groupRows, "SELECT project, group_id, session_id FROM board_groups")

	type groupKey struct{ project, groupID string }
	groupsByKey := make(map[groupKey]map[string]bool)
	for _, gr := range groupRows {
		key := groupKey{gr.Project, gr.GroupID}
		if groupsByKey[key] == nil {
			groupsByKey[key] = make(map[string]bool)
		}
		groupsByKey[key][gr.SessionID] = true
	}

	// Group subscribers by project
	type subInfo struct {
		SessionID   string
		JobTitle    string
		LastReadID  int64
		ReceiveMode string
	}
	byProject := make(map[string][]subInfo)
	for _, sub := range subs {
		rm := sub.ReceiveMode
		if rm == "" {
			rm = "mentions"
		}
		byProject[sub.Project] = append(byProject[sub.Project], subInfo{
			sub.SessionID, sub.JobTitle, sub.LastReadID, rm,
		})
	}

	result := make(map[string]int)

	for project, projectSubs := range byProject {
		minCursor := projectSubs[0].LastReadID
		for _, sub := range projectSubs {
			if sub.LastReadID < minCursor {
				minCursor = sub.LastReadID
			}
		}

		var msgs []struct {
			ID        int64  `db:"id"`
			SessionID string `db:"session_id"`
			Content   string `db:"content"`
		}
		s.db.SelectContext(ctx, &msgs,
			"SELECT id, session_id, content FROM board_messages WHERE project = ? AND id > ? ORDER BY id",
			project, minCursor)

		if len(msgs) == 0 {
			for _, sub := range projectSubs {
				result[sub.SessionID] = 0
			}
			continue
		}

		for _, sub := range projectSubs {
			if sub.ReceiveMode == "none" {
				result[sub.SessionID] = 0
				continue
			}

			count := 0
			switch sub.ReceiveMode {
			case "all":
				for _, msg := range msgs {
					if msg.ID <= sub.LastReadID || msg.SessionID == sub.SessionID {
						continue
					}
					count++
				}
			case "mentions":
				mentionTerms := []string{"@notify-all", "@notify_all", "@notifyall", "@all",
					"@" + sub.SessionID}
				if sub.JobTitle != "" {
					mentionTerms = append(mentionTerms, "@"+sub.JobTitle)
				}
				for _, msg := range msgs {
					if msg.ID <= sub.LastReadID || msg.SessionID == sub.SessionID {
						continue
					}
					contentLower := strings.ToLower(msg.Content)
					for _, term := range mentionTerms {
						if strings.Contains(contentLower, strings.ToLower(term)) {
							count++
							break
						}
					}
				}
			default:
				// Group-based mode
				members := groupsByKey[groupKey{project, sub.ReceiveMode}]
				for _, msg := range msgs {
					if msg.ID <= sub.LastReadID || msg.SessionID == sub.SessionID {
						continue
					}
					if members[msg.SessionID] {
						count++
					}
				}
			}
			result[sub.SessionID] = count
		}
	}

	return result, nil
}

// ── Groups ───────────────────────────────────────────────────────────

// AddToGroup adds a session to a board group.
func (s *Store) AddToGroup(ctx context.Context, project, groupID, sessionID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO board_groups (project, group_id, session_id) VALUES (?, ?, ?)
		 ON CONFLICT(project, group_id, session_id) DO NOTHING`,
		project, groupID, sessionID)
	return err
}

// RemoveFromGroup removes a session from a board group. Returns true if removed.
func (s *Store) RemoveFromGroup(ctx context.Context, project, groupID, sessionID string) (bool, error) {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM board_groups WHERE project = ? AND group_id = ? AND session_id = ?",
		project, groupID, sessionID)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

// ListGroupMembers returns session_ids in a group.
func (s *Store) ListGroupMembers(ctx context.Context, project, groupID string) ([]string, error) {
	var members []string
	err := s.db.SelectContext(ctx, &members,
		"SELECT session_id FROM board_groups WHERE project = ? AND group_id = ? ORDER BY session_id",
		project, groupID)
	if err != nil {
		return []string{}, nil
	}
	return members, nil
}

// ListGroups returns all groups for a project with member counts.
func (s *Store) ListGroups(ctx context.Context, project string) ([]GroupInfo, error) {
	var groups []GroupInfo
	err := s.db.SelectContext(ctx, &groups,
		`SELECT group_id, COUNT(*) as member_count
		 FROM board_groups WHERE project = ?
		 GROUP BY group_id ORDER BY group_id`,
		project)
	if err != nil {
		return []GroupInfo{}, nil
	}
	return groups, nil
}

// DeleteMessage deletes a single message by ID.
func (s *Store) DeleteMessage(ctx context.Context, messageID int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM board_messages WHERE id = ?", messageID)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

// GetWebhookTargets returns subscribers with webhook URLs (excluding sender).
func (s *Store) GetWebhookTargets(ctx context.Context, project, excludeSessionID string) ([]Subscriber, error) {
	var subs []Subscriber
	err := s.db.SelectContext(ctx, &subs,
		`SELECT * FROM board_subscribers
		 WHERE project = ? AND session_id != ? AND webhook_url IS NOT NULL AND webhook_url != ''`,
		project, excludeSessionID)
	return subs, err
}

// ── Projects ─────────────────────────────────────────────────────────

// ListProjects returns all known projects with subscriber and message counts.
func (s *Store) ListProjects(ctx context.Context) ([]ProjectInfo, error) {
	var projects []ProjectInfo
	err := s.db.SelectContext(ctx, &projects,
		`SELECT project,
		        (SELECT COUNT(*) FROM board_subscribers s WHERE s.project = p.project) as subscriber_count,
		        (SELECT COUNT(*) FROM board_messages m WHERE m.project = p.project) as message_count
		 FROM (
		     SELECT DISTINCT project FROM board_subscribers
		     UNION
		     SELECT DISTINCT project FROM board_messages
		 ) p ORDER BY project`)
	return projects, err
}

// EnrichedProject holds project info with timestamps and participant names.
type EnrichedProject struct {
	Project          string  `db:"project" json:"project"`
	SubscriberCount  int     `db:"subscriber_count" json:"subscriber_count"`
	MessageCount     int     `db:"message_count" json:"message_count"`
	FirstMessageAt   *string `db:"first_message_at" json:"first_message_at"`
	LastMessageAt    *string `db:"last_message_at" json:"last_message_at"`
	ParticipantNames *string `db:"participant_names" json:"participant_names"`
}

// ListProjectsEnriched returns board projects with timestamps, subscriber info, and participant names.
func (s *Store) ListProjectsEnriched(ctx context.Context) ([]EnrichedProject, error) {
	var projects []EnrichedProject
	err := s.db.SelectContext(ctx, &projects,
		`SELECT
		     p.project,
		     (SELECT COUNT(*) FROM board_subscribers s WHERE s.project = p.project) as subscriber_count,
		     (SELECT COUNT(*) FROM board_messages m WHERE m.project = p.project) as message_count,
		     (SELECT MIN(created_at) FROM board_messages m WHERE m.project = p.project) as first_message_at,
		     (SELECT MAX(created_at) FROM board_messages m WHERE m.project = p.project) as last_message_at,
		     (SELECT GROUP_CONCAT(s.job_title, ', ')
		      FROM board_subscribers s WHERE s.project = p.project
		      ORDER BY s.subscribed_at LIMIT 5) as participant_names
		 FROM (
		     SELECT DISTINCT project FROM board_subscribers
		     UNION
		     SELECT DISTINCT project FROM board_messages
		 ) p
		 ORDER BY (SELECT MAX(created_at) FROM board_messages m WHERE m.project = p.project) DESC`)
	return projects, err
}

// SearchMessages returns project names that have messages matching the query (LIKE search).
func (s *Store) SearchMessages(ctx context.Context, query string) ([]string, error) {
	var projects []string
	err := s.db.SelectContext(ctx, &projects,
		"SELECT DISTINCT project FROM board_messages WHERE content LIKE ? COLLATE NOCASE",
		"%"+query+"%")
	if err != nil {
		return []string{}, nil
	}
	return projects, nil
}

// DeleteProject removes all messages and subscribers for a project.
func (s *Store) DeleteProject(ctx context.Context, project string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tx.ExecContext(ctx, "DELETE FROM board_messages WHERE project = ?", project)
	tx.ExecContext(ctx, "DELETE FROM board_subscribers WHERE project = ?", project)
	tx.ExecContext(ctx, "DELETE FROM board_groups WHERE project = ?", project)
	return tx.Commit()
}
