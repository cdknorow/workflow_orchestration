package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	at "github.com/cdknorow/coral/internal/agenttypes"
	"github.com/jmoiron/sqlx"
)

// SessionMeta holds notes and summary metadata for a session.
type SessionMeta struct {
	SessionID    string  `db:"session_id" json:"session_id"`
	NotesMD      string  `db:"notes_md" json:"notes_md"`
	AutoSummary  string  `db:"auto_summary" json:"auto_summary"`
	IsUserEdited bool    `db:"is_user_edited" json:"is_user_edited"`
	DisplayName  *string `db:"display_name" json:"display_name,omitempty"`
	UpdatedAt    *string `db:"updated_at" json:"updated_at,omitempty"`
}

// Tag represents a user-defined tag.
type Tag struct {
	ID    int64  `db:"id" json:"id"`
	Name  string `db:"name" json:"name"`
	Color string `db:"color" json:"color"`
}

// SessionIndex holds indexed session info for search and listing.
type SessionIndex struct {
	SessionID      string  `db:"session_id" json:"session_id"`
	SourceType     string  `db:"source_type" json:"source_type"`
	SourceFile     string  `db:"source_file" json:"source_file"`
	FirstTimestamp *string `db:"first_timestamp" json:"first_timestamp"`
	LastTimestamp  *string `db:"last_timestamp" json:"last_timestamp"`
	MessageCount   int     `db:"message_count" json:"message_count"`
	DisplaySummary string  `db:"display_summary" json:"summary"`
	IndexedAt      string  `db:"indexed_at" json:"-"`
	FileMtime      float64 `db:"file_mtime" json:"-"`
}

// LiveSession represents a persistent live session record.
type LiveSession struct {
	SessionID    string  `db:"session_id" json:"session_id"`
	AgentType    string  `db:"agent_type" json:"agent_type"`
	AgentName    string  `db:"agent_name" json:"agent_name"`
	WorkingDir   string  `db:"working_dir" json:"working_dir"`
	DisplayName  *string `db:"display_name" json:"display_name,omitempty"`
	ResumeFromID *string `db:"resume_from_id" json:"resume_from_id,omitempty"`
	Flags        *string `db:"flags" json:"flags,omitempty"`
	IsJob        int     `db:"is_job" json:"is_job"`
	Prompt       *string `db:"prompt" json:"prompt,omitempty"`
	BoardName    *string `db:"board_name" json:"board_name,omitempty"`
	BoardServer  *string `db:"board_server" json:"board_server,omitempty"`
	Backend      *string `db:"backend" json:"backend,omitempty"`
	Icon         *string `db:"icon" json:"icon,omitempty"`
	IsSleeping   int     `db:"is_sleeping" json:"is_sleeping"`
	BoardType    *string `db:"board_type" json:"board_type,omitempty"`
	GitDiffMode  *string `db:"git_diff_mode" json:"git_diff_mode,omitempty"`
	Capabilities *string `db:"capabilities" json:"capabilities,omitempty"`
	Model        *string `db:"model" json:"model,omitempty"`
	Tools        *string `db:"tools" json:"tools,omitempty"`
	MCPServers   *string `db:"mcp_servers" json:"mcp_servers,omitempty"`
	PID          int     `db:"pid" json:"pid,omitempty"`
	CreatedAt    string  `db:"created_at" json:"created_at"`
}

// UserSetting is a key-value pair.
type UserSetting struct {
	Key   string `db:"key" json:"key"`
	Value string `db:"value" json:"value"`
}

// SessionStore provides session-related database operations.
type SessionStore struct {
	db *DB
}

// NewSessionStore creates a new SessionStore.
func NewSessionStore(db *DB) *SessionStore {
	return &SessionStore{db: db}
}

// DB returns the underlying database connection.
func (s *SessionStore) DB() *DB {
	return s.db
}

// ── User Settings ──────────────────────────────────────────────────────

// GetSettings returns all user settings as a map.
func (s *SessionStore) GetSettings(ctx context.Context) (map[string]string, error) {
	var settings []UserSetting
	if err := s.db.SelectContext(ctx, &settings, "SELECT key, value FROM user_settings"); err != nil {
		return nil, err
	}
	result := make(map[string]string, len(settings))
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	return result, nil
}

// SetSetting upserts a user setting.
func (s *SessionStore) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// DeleteSetting deletes a user setting. Returns true if the key existed.
func (s *SessionStore) DeleteSetting(ctx context.Context, key string) (bool, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM user_settings WHERE key = ?", key)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ── Session Notes ──────────────────────────────────────────────────────

// GetSessionNotes returns notes metadata for a session.
func (s *SessionStore) GetSessionNotes(ctx context.Context, sessionID string) (*SessionMeta, error) {
	var meta SessionMeta
	err := s.db.GetContext(ctx, &meta,
		`SELECT notes_md, auto_summary, is_user_edited, updated_at
		 FROM session_meta WHERE session_id = ?`, sessionID)
	if err == sql.ErrNoRows {
		return &SessionMeta{SessionID: sessionID}, nil
	}
	if err != nil {
		return nil, err
	}
	meta.SessionID = sessionID
	return &meta, nil
}

// SaveSessionNotes saves user-edited notes for a session.
func (s *SessionStore) SaveSessionNotes(ctx context.Context, sessionID, notesMD string) error {
	now := nowUTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session_meta (session_id, notes_md, is_user_edited, created_at, updated_at)
		 VALUES (?, ?, 1, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		     notes_md = excluded.notes_md,
		     is_user_edited = 1,
		     updated_at = excluded.updated_at`,
		sessionID, notesMD, now, now)
	return err
}

// SaveAutoSummary saves an AI-generated summary, but only if the user hasn't edited.
func (s *SessionStore) SaveAutoSummary(ctx context.Context, sessionID, summary string) error {
	now := nowUTC()

	// Check if user has edited
	var edited int
	err := s.db.GetContext(ctx, &edited,
		"SELECT is_user_edited FROM session_meta WHERE session_id = ?", sessionID)
	if err == nil && edited == 1 {
		return nil // Don't overwrite user edits
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO session_meta (session_id, auto_summary, created_at, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		     auto_summary = excluded.auto_summary,
		     updated_at = excluded.updated_at`,
		sessionID, summary, now, now)
	return err
}

// ── Display Names ──────────────────────────────────────────────────────

// GetDisplayName returns the display name for a session.
func (s *SessionStore) GetDisplayName(ctx context.Context, sessionID string) (*string, error) {
	var name *string
	err := s.db.GetContext(ctx, &name,
		"SELECT display_name FROM session_meta WHERE session_id = ?", sessionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return name, err
}

// SetDisplayName sets or updates the display name for a session.
func (s *SessionStore) SetDisplayName(ctx context.Context, sessionID, displayName string) error {
	now := nowUTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session_meta (session_id, display_name, created_at, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		     display_name = excluded.display_name,
		     updated_at = excluded.updated_at`,
		sessionID, displayName, now, now)
	return err
}

// GetDisplayNames returns display names for multiple sessions.
func (s *SessionStore) GetDisplayNames(ctx context.Context, sessionIDs []string) (map[string]string, error) {
	if len(sessionIDs) == 0 {
		return map[string]string{}, nil
	}

	query, args, err := sqlx.In(
		"SELECT session_id, display_name FROM session_meta WHERE session_id IN (?) AND display_name IS NOT NULL",
		sessionIDs)
	if err != nil {
		return nil, err
	}

	var rows []struct {
		SessionID   string `db:"session_id"`
		DisplayName string `db:"display_name"`
	}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	result := make(map[string]string, len(rows))
	for _, r := range rows {
		result[r.SessionID] = r.DisplayName
	}
	return result, nil
}

// MigrateDisplayName copies a display name from an old session to a new one.
func (s *SessionStore) MigrateDisplayName(ctx context.Context, oldSessionID, newSessionID string) error {
	name, err := s.GetDisplayName(ctx, oldSessionID)
	if err != nil || name == nil {
		return err
	}
	return s.SetDisplayName(ctx, newSessionID, *name)
}

// ── Tags ───────────────────────────────────────────────────────────────

// ListTags returns all defined tags.
func (s *SessionStore) ListTags(ctx context.Context) ([]Tag, error) {
	var tags []Tag
	err := s.db.SelectContext(ctx, &tags, "SELECT id, name, color FROM tags ORDER BY name")
	return tags, err
}

// CreateTag creates a new tag.
func (s *SessionStore) CreateTag(ctx context.Context, name, color string) (*Tag, error) {
	if color == "" {
		color = "#58a6ff"
	}
	result, err := s.db.ExecContext(ctx,
		"INSERT INTO tags (name, color) VALUES (?, ?)", name, color)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Tag{ID: id, Name: name, Color: color}, nil
}

// DeleteTag deletes a tag by ID.
func (s *SessionStore) DeleteTag(ctx context.Context, tagID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM tags WHERE id = ?", tagID)
	return err
}

// GetSessionTags returns tags associated with a session.
func (s *SessionStore) GetSessionTags(ctx context.Context, sessionID string) ([]Tag, error) {
	var tags []Tag
	err := s.db.SelectContext(ctx, &tags,
		`SELECT t.id, t.name, t.color FROM tags t
		 JOIN session_tags st ON st.tag_id = t.id
		 WHERE st.session_id = ? ORDER BY t.name`, sessionID)
	return tags, err
}

// AddSessionTag associates a tag with a session.
func (s *SessionStore) AddSessionTag(ctx context.Context, sessionID string, tagID int64) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO session_tags (session_id, tag_id) VALUES (?, ?)",
		sessionID, tagID)
	return err
}

// RemoveSessionTag removes a tag association from a session.
func (s *SessionStore) RemoveSessionTag(ctx context.Context, sessionID string, tagID int64) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM session_tags WHERE session_id = ? AND tag_id = ?",
		sessionID, tagID)
	return err
}

// ── Folder Tags ────────────────────────────────────────────────────────

// GetFolderTags returns tags for a folder.
func (s *SessionStore) GetFolderTags(ctx context.Context, folderName string) ([]Tag, error) {
	var tags []Tag
	err := s.db.SelectContext(ctx, &tags,
		`SELECT t.id, t.name, t.color FROM tags t
		 JOIN folder_tags ft ON ft.tag_id = t.id
		 WHERE ft.folder_name = ? ORDER BY t.name`, folderName)
	return tags, err
}

// AddFolderTag associates a tag with a folder.
func (s *SessionStore) AddFolderTag(ctx context.Context, folderName string, tagID int64) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO folder_tags (folder_name, tag_id) VALUES (?, ?)",
		folderName, tagID)
	return err
}

// RemoveFolderTag removes a tag from a folder.
func (s *SessionStore) RemoveFolderTag(ctx context.Context, folderName string, tagID int64) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM folder_tags WHERE folder_name = ? AND tag_id = ?",
		folderName, tagID)
	return err
}

// GetAllFolderTags returns all folder-tag associations grouped by folder.
func (s *SessionStore) GetAllFolderTags(ctx context.Context) (map[string][]Tag, error) {
	var rows []struct {
		FolderName string `db:"folder_name"`
		ID         int64  `db:"id"`
		Name       string `db:"name"`
		Color      string `db:"color"`
	}
	err := s.db.SelectContext(ctx, &rows,
		`SELECT ft.folder_name, t.id, t.name, t.color FROM folder_tags ft
		 JOIN tags t ON t.id = ft.tag_id ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]Tag)
	for _, r := range rows {
		result[r.FolderName] = append(result[r.FolderName], Tag{
			ID: r.ID, Name: r.Name, Color: r.Color,
		})
	}
	return result, nil
}

// ── Session Index ──────────────────────────────────────────────────────

// UpsertSessionIndex inserts or replaces a session index entry.
func (s *SessionStore) UpsertSessionIndex(ctx context.Context, idx *SessionIndex) error {
	now := nowUTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO session_index
		 (session_id, source_type, source_file, first_timestamp, last_timestamp,
		  message_count, display_summary, indexed_at, file_mtime)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idx.SessionID, idx.SourceType, idx.SourceFile,
		idx.FirstTimestamp, idx.LastTimestamp,
		idx.MessageCount, idx.DisplaySummary, now, idx.FileMtime)
	return err
}

// UpsertFTS updates the FTS5 index for a session.
func (s *SessionStore) UpsertFTS(ctx context.Context, sessionID, body string) error {
	return s.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM session_fts WHERE session_id = ?", sessionID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			"INSERT INTO session_fts (session_id, body) VALUES (?, ?)",
			sessionID, body)
		return err // FTS5 may not be available
	})
}

// EnqueueForSummarization adds a session to the summarization queue.
func (s *SessionStore) EnqueueForSummarization(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO summarizer_queue (session_id, status) VALUES (?, 'pending')",
		sessionID)
	return err
}

// MarkSummarized updates the summarization status for a session.
func (s *SessionStore) MarkSummarized(ctx context.Context, sessionID, status string, errMsg *string) error {
	now := nowUTC()
	_, err := s.db.ExecContext(ctx,
		"UPDATE summarizer_queue SET status = ?, attempted_at = ?, error_msg = ? WHERE session_id = ?",
		status, now, errMsg, sessionID)
	return err
}

// GetPendingSummaries returns session IDs pending summarization.
func (s *SessionStore) GetPendingSummaries(ctx context.Context, limit int) ([]string, error) {
	var ids []string
	err := s.db.SelectContext(ctx, &ids,
		"SELECT session_id FROM summarizer_queue WHERE status = 'pending' LIMIT ?", limit)
	return ids, err
}

// GetIndexedMtimes returns source_file -> file_mtime for all indexed sessions.
func (s *SessionStore) GetIndexedMtimes(ctx context.Context) (map[string]float64, error) {
	var rows []struct {
		SourceFile string  `db:"source_file"`
		FileMtime  float64 `db:"file_mtime"`
	}
	err := s.db.SelectContext(ctx, &rows,
		"SELECT source_file, file_mtime FROM session_index")
	if err != nil {
		return nil, err
	}
	result := make(map[string]float64, len(rows))
	for _, r := range rows {
		if existing, ok := result[r.SourceFile]; !ok || r.FileMtime > existing {
			result[r.SourceFile] = r.FileMtime
		}
	}
	return result, nil
}

// SessionListParams holds parameters for paginated session listing.
type SessionListParams struct {
	Page           int
	PageSize       int
	Search         string
	FTSMode        string // "phrase", "and", "or"
	TagIDs         []int64
	TagLogic       string // "AND" or "OR"
	SourceTypes    []string
	DateFrom       string
	DateTo         string
	MinDurationSec *int
	MaxDurationSec *int
}

// SessionListResult holds paginated session results.
type SessionListResult struct {
	Sessions []SessionListItem `json:"sessions"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

// SessionListItem is an enriched session for list display.
type SessionListItem struct {
	SessionID      string  `json:"session_id"`
	SourceType     string  `json:"source_type"`
	SourceFile     string  `json:"source_file"`
	FirstTimestamp *string `json:"first_timestamp"`
	LastTimestamp  *string `json:"last_timestamp"`
	MessageCount   int     `json:"message_count"`
	Summary        string  `json:"summary"`
	SummaryTitle   string  `json:"summary_title"`
	HasNotes       bool    `json:"has_notes"`
	Tags           []Tag   `json:"tags"`
	Branch         *string `json:"branch"`
	DurationSec    *int    `json:"duration_sec"`
}

// ListSessionsPaged returns a paginated, filtered list of sessions.
func (s *SessionStore) ListSessionsPaged(ctx context.Context, params SessionListParams) (*SessionListResult, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 {
		params.PageSize = 50
	}
	if params.FTSMode == "" {
		params.FTSMode = "and"
	}

	var args []interface{}
	var whereClauses []string
	fromClause := "session_index si"
	orderClause := "si.last_timestamp DESC"

	// Full-text search
	if params.Search != "" {
		safeQ := sanitizeFTSQuery(params.Search, params.FTSMode)
		if safeQ != "" {
			fromClause += " JOIN session_fts fts ON fts.session_id = si.session_id"
			whereClauses = append(whereClauses, "session_fts MATCH ?")
			args = append(args, safeQ)
			orderClause = "rank"
		}
	}

	// Date filters
	if params.DateFrom != "" {
		whereClauses = append(whereClauses, "si.last_timestamp >= ?")
		args = append(args, params.DateFrom+"T00:00:00")
	}
	if params.DateTo != "" {
		whereClauses = append(whereClauses, "si.last_timestamp <= ?")
		args = append(args, params.DateTo+"T23:59:59")
	}

	// Duration filters
	if params.MinDurationSec != nil {
		whereClauses = append(whereClauses,
			"(julianday(si.last_timestamp) - julianday(si.first_timestamp)) * 86400 >= ?")
		args = append(args, *params.MinDurationSec)
	}
	if params.MaxDurationSec != nil {
		whereClauses = append(whereClauses,
			"(julianday(si.last_timestamp) - julianday(si.first_timestamp)) * 86400 <= ?")
		args = append(args, *params.MaxDurationSec)
	}

	// Tag filters
	if len(params.TagIDs) > 0 && params.TagLogic == "AND" {
		for _, tid := range params.TagIDs {
			whereClauses = append(whereClauses,
				"si.session_id IN (SELECT session_id FROM session_tags WHERE tag_id = ?)")
			args = append(args, tid)
		}
	} else if len(params.TagIDs) > 0 {
		whereClauses = append(whereClauses,
			"si.session_id IN (SELECT session_id FROM session_tags WHERE tag_id IN (?))")
		args = append(args, params.TagIDs)
	}

	// Source type filter
	if len(params.SourceTypes) > 0 {
		whereClauses = append(whereClauses, "si.source_type IN (?)")
		args = append(args, params.SourceTypes)
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = " WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Count total
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", fromClause, whereSQL)
	countSQL, countArgs, err := sqlx.In(countSQL, args...)
	if err != nil {
		return nil, err
	}
	var total int
	if err := s.db.GetContext(ctx, &total, countSQL, countArgs...); err != nil {
		return nil, err
	}

	// Fetch page
	offset := (params.Page - 1) * params.PageSize
	selectFields := `si.session_id, si.source_type, si.source_file,
		si.first_timestamp, si.last_timestamp, si.message_count, si.display_summary`
	query := fmt.Sprintf("SELECT %s FROM %s%s ORDER BY %s LIMIT ? OFFSET ?",
		selectFields, fromClause, whereSQL, orderClause)
	query, pageArgs, err := sqlx.In(query, append(args, params.PageSize, offset)...)
	if err != nil {
		return nil, err
	}

	var rows []struct {
		SessionID      string  `db:"session_id"`
		SourceType     string  `db:"source_type"`
		SourceFile     string  `db:"source_file"`
		FirstTimestamp *string `db:"first_timestamp"`
		LastTimestamp  *string `db:"last_timestamp"`
		MessageCount   int     `db:"message_count"`
		DisplaySummary string  `db:"display_summary"`
	}
	if err := s.db.SelectContext(ctx, &rows, query, pageArgs...); err != nil {
		return nil, err
	}

	// Collect session IDs for enrichment
	sessionIDs := make([]string, len(rows))
	for i, r := range rows {
		sessionIDs[i] = r.SessionID
	}

	// Enrich with metadata
	metaMap := make(map[string]struct {
		HasNotes     bool
		IsUserEdited bool
		SummaryTitle string
	})
	tagsMap := make(map[string][]Tag)
	branchMap := make(map[string]string)

	if len(sessionIDs) > 0 {
		// Fetch notes metadata
		metaQuery, metaArgs, _ := sqlx.In(
			"SELECT session_id, notes_md, auto_summary, is_user_edited FROM session_meta WHERE session_id IN (?)",
			sessionIDs)
		var metaRows []struct {
			SessionID    string `db:"session_id"`
			NotesMD      string `db:"notes_md"`
			AutoSummary  string `db:"auto_summary"`
			IsUserEdited bool   `db:"is_user_edited"`
		}
		if err := s.db.SelectContext(ctx, &metaRows, metaQuery, metaArgs...); err == nil {
			for _, r := range metaRows {
				content := r.NotesMD
				if content == "" {
					content = r.AutoSummary
				}
				metaMap[r.SessionID] = struct {
					HasNotes     bool
					IsUserEdited bool
					SummaryTitle string
				}{
					HasNotes:     r.NotesMD != "" || r.AutoSummary != "",
					IsUserEdited: r.IsUserEdited,
					SummaryTitle: extractFirstHeader(content),
				}
			}
		}

		// Fetch tags
		tagQuery, tagArgs, _ := sqlx.In(
			`SELECT st.session_id, t.id, t.name, t.color
			 FROM session_tags st JOIN tags t ON t.id = st.tag_id
			 WHERE st.session_id IN (?) ORDER BY t.name`,
			sessionIDs)
		var tagRows []struct {
			SessionID string `db:"session_id"`
			ID        int64  `db:"id"`
			Name      string `db:"name"`
			Color     string `db:"color"`
		}
		if err := s.db.SelectContext(ctx, &tagRows, tagQuery, tagArgs...); err == nil {
			for _, r := range tagRows {
				tagsMap[r.SessionID] = append(tagsMap[r.SessionID], Tag{
					ID: r.ID, Name: r.Name, Color: r.Color,
				})
			}
		}

		// Fetch git branches
		branchQuery, branchArgs, _ := sqlx.In(
			`SELECT gs.session_id, gs.branch
			 FROM git_snapshots gs
			 INNER JOIN (
			     SELECT session_id, MAX(recorded_at) as max_ts
			     FROM git_snapshots WHERE session_id IN (?)
			     GROUP BY session_id
			 ) latest ON gs.session_id = latest.session_id AND gs.recorded_at = latest.max_ts`,
			sessionIDs)
		var branchRows []struct {
			SessionID string `db:"session_id"`
			Branch    string `db:"branch"`
		}
		if err := s.db.SelectContext(ctx, &branchRows, branchQuery, branchArgs...); err == nil {
			for _, r := range branchRows {
				branchMap[r.SessionID] = r.Branch
			}
		}
	}

	// Build results
	sessions := make([]SessionListItem, len(rows))
	for i, r := range rows {
		meta := metaMap[r.SessionID]
		tags := tagsMap[r.SessionID]
		if tags == nil {
			tags = []Tag{}
		}
		var branch *string
		if b, ok := branchMap[r.SessionID]; ok {
			branch = &b
		}

		sessions[i] = SessionListItem{
			SessionID:      r.SessionID,
			SourceType:     r.SourceType,
			SourceFile:     r.SourceFile,
			FirstTimestamp: r.FirstTimestamp,
			LastTimestamp:  r.LastTimestamp,
			MessageCount:   r.MessageCount,
			Summary:        r.DisplaySummary,
			SummaryTitle:   meta.SummaryTitle,
			HasNotes:       meta.HasNotes,
			Tags:           tags,
			Branch:         branch,
			DurationSec:    computeDuration(r.FirstTimestamp, r.LastTimestamp),
		}
	}

	return &SessionListResult{
		Sessions: sessions,
		Total:    total,
		Page:     params.Page,
		PageSize: params.PageSize,
	}, nil
}

// ── Agent Live State ──────────────────────────────────────────────────

// GetAgentSessionID returns the current session_id for a live agent.
func (s *SessionStore) GetAgentSessionID(ctx context.Context, agentName string) (*string, error) {
	var id *string
	err := s.db.GetContext(ctx, &id,
		"SELECT current_session_id FROM agent_live_state WHERE agent_name = ?", agentName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return id, err
}

// SetAgentSessionID sets the current session_id for a live agent.
func (s *SessionStore) SetAgentSessionID(ctx context.Context, agentName, sessionID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_live_state (agent_name, current_session_id) VALUES (?, ?)
		 ON CONFLICT(agent_name) DO UPDATE SET current_session_id = excluded.current_session_id`,
		agentName, sessionID)
	return err
}

// ClearAgentSessionID clears the current session_id for a live agent.
func (s *SessionStore) ClearAgentSessionID(ctx context.Context, agentName string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_live_state (agent_name, current_session_id) VALUES (?, NULL)
		 ON CONFLICT(agent_name) DO UPDATE SET current_session_id = NULL`,
		agentName)
	return err
}

// ── Live Sessions ─────────────────────────────────────────────────────

// RegisterLiveSession registers a new live session.
func (s *SessionStore) RegisterLiveSession(ctx context.Context, ls *LiveSession) error {
	if ls.CreatedAt == "" {
		ls.CreatedAt = nowUTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO live_sessions
		 (session_id, agent_type, agent_name, working_dir, display_name, resume_from_id, flags, is_job, prompt, board_name, board_server, backend, icon, is_sleeping, board_type, capabilities, model, tools, mcp_servers, pid, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ls.SessionID, ls.AgentType, ls.AgentName, ls.WorkingDir,
		ls.DisplayName, ls.ResumeFromID, ls.Flags, ls.IsJob,
		ls.Prompt, ls.BoardName, ls.BoardServer, ls.Backend, ls.Icon, ls.IsSleeping, ls.BoardType,
		ls.Capabilities, ls.Model, ls.Tools, ls.MCPServers, ls.PID, ls.CreatedAt)
	return err
}

// UnregisterLiveSession removes a live session record.
func (s *SessionStore) UnregisterLiveSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM live_sessions WHERE session_id = ?", sessionID)
	return err
}

// GetAllLiveSessions returns all registered live sessions.
func (s *SessionStore) GetAllLiveSessions(ctx context.Context) ([]LiveSession, error) {
	var sessions []LiveSession
	err := s.db.SelectContext(ctx, &sessions,
		`SELECT session_id, agent_type, agent_name, working_dir, display_name,
		 resume_from_id, flags, is_job, prompt, board_name, board_server, icon, is_sleeping, board_type, capabilities, model, tools, mcp_servers, created_at
		 FROM live_sessions ORDER BY created_at`)
	return sessions, err
}

// GetBoardSessions returns all live sessions on a given board.
func (s *SessionStore) GetBoardSessions(ctx context.Context, boardName string) ([]LiveSession, error) {
	var sessions []LiveSession
	err := s.db.SelectContext(ctx, &sessions,
		`SELECT session_id, agent_type, agent_name, working_dir, display_name,
		 resume_from_id, flags, is_job, prompt, board_name, board_server, icon, is_sleeping, board_type, capabilities, model, tools, mcp_servers, created_at
		 FROM live_sessions WHERE board_name = ? ORDER BY created_at`, boardName)
	return sessions, err
}

// CountLiveSessions returns the total number of live sessions (including sleeping).
func (s *SessionStore) CountLiveSessions(ctx context.Context) (int, error) {
	var count int
	err := s.db.GetContext(ctx, &count,
		"SELECT COUNT(*) FROM live_sessions")
	return count, err
}

// UpdateSessionPID stores the shell process PID for a live session.
func (s *SessionStore) UpdateSessionPID(ctx context.Context, sessionID string, pid int) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE live_sessions SET pid = ? WHERE session_id = ?", pid, sessionID)
	return err
}

// ResolveByPIDs looks up live sessions matching any of the given PIDs.
// Returns the first match (used for process-tree-based identity resolution).
func (s *SessionStore) ResolveByPIDs(ctx context.Context, pids []int) (*LiveSession, error) {
	if len(pids) == 0 {
		return nil, fmt.Errorf("no PIDs provided")
	}
	query := "SELECT session_id, agent_type, agent_name, working_dir, display_name, board_name, pid FROM live_sessions WHERE pid IN (?" + strings.Repeat(",?", len(pids)-1) + ") AND pid > 0 LIMIT 1"
	args := make([]interface{}, len(pids))
	for i, p := range pids {
		args[i] = p
	}
	var ls LiveSession
	err := s.db.GetContext(ctx, &ls, query, args...)
	if err != nil {
		return nil, err
	}
	return &ls, nil
}

// CountLiveTeams returns the number of distinct teams (board_name values),
// including both active and sleeping teams.
func (s *SessionStore) CountLiveTeams(ctx context.Context) (int, error) {
	var count int
	err := s.db.GetContext(ctx, &count,
		"SELECT COUNT(DISTINCT board_name) FROM live_sessions WHERE board_name IS NOT NULL AND board_name != ''")
	return count, err
}

// CountBoardSessions returns the number of live sessions on a given board.
func (s *SessionStore) CountBoardSessions(ctx context.Context, boardName string) (int, error) {
	var count int
	err := s.db.GetContext(ctx, &count,
		"SELECT COUNT(*) FROM live_sessions WHERE board_name = ?", boardName)
	return count, err
}

// GetLiveSessionPromptInfo returns prompt, board_name, and board_server for a live session.
func (s *SessionStore) GetLiveSessionPromptInfo(ctx context.Context, sessionID string) (*LiveSession, error) {
	var ls LiveSession
	err := s.db.GetContext(ctx, &ls,
		"SELECT prompt, board_name, board_server, board_type FROM live_sessions WHERE session_id = ?", sessionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &ls, err
}

// GetLiveSession returns a full live session record by session_id.
func (s *SessionStore) GetLiveSession(ctx context.Context, sessionID string) (*LiveSession, error) {
	var ls LiveSession
	err := s.db.GetContext(ctx, &ls,
		`SELECT session_id, agent_type, agent_name, working_dir, display_name,
		 resume_from_id, flags, is_job, prompt, board_name, board_server, icon, is_sleeping, board_type, git_diff_mode, capabilities, model, tools, mcp_servers, created_at
		 FROM live_sessions WHERE session_id = ?`, sessionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &ls, err
}

// GetAgentTypeForSession looks up the agent type for a live session.
func (s *SessionStore) GetAgentTypeForSession(ctx context.Context, sessionID string) string {
	var agentType string
	err := s.db.GetContext(ctx, &agentType,
		"SELECT agent_type FROM live_sessions WHERE session_id = ?", sessionID)
	if err != nil {
		return at.Claude
	}
	return agentType
}

// ReplaceLiveSession replaces an old session with a new one, carrying forward metadata.
func (s *SessionStore) ReplaceLiveSession(ctx context.Context, oldSessionID string, newSession *LiveSession) error {
	return s.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		// Carry forward flags, prompt, board from old session if not set
		var old LiveSession
		err := tx.GetContext(ctx, &old,
			"SELECT flags, prompt, board_name, board_server, icon, is_sleeping, board_type, display_name FROM live_sessions WHERE session_id = ?",
			oldSessionID)
		if err == nil {
			if newSession.Flags == nil {
				newSession.Flags = old.Flags
			}
			if newSession.Prompt == nil {
				newSession.Prompt = old.Prompt
			}
			if newSession.BoardName == nil {
				newSession.BoardName = old.BoardName
			}
			if newSession.BoardServer == nil {
				newSession.BoardServer = old.BoardServer
			}
			if newSession.Icon == nil {
				newSession.Icon = old.Icon
			}
			if newSession.BoardType == nil {
				newSession.BoardType = old.BoardType
			}
			if newSession.DisplayName == nil {
				newSession.DisplayName = old.DisplayName
			}
			newSession.IsSleeping = old.IsSleeping
		}

		if _, err := tx.ExecContext(ctx, "DELETE FROM live_sessions WHERE session_id = ?", oldSessionID); err != nil {
			return err
		}

		now := nowUTC()
		_, err = tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO live_sessions
			 (session_id, agent_type, agent_name, working_dir, display_name, resume_from_id, flags, prompt, board_name, board_server, icon, is_sleeping, board_type, pid, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			newSession.SessionID, newSession.AgentType, newSession.AgentName,
			newSession.WorkingDir, newSession.DisplayName, newSession.ResumeFromID,
			newSession.Flags, newSession.Prompt, newSession.BoardName,
			newSession.BoardServer, newSession.Icon, newSession.IsSleeping, newSession.BoardType, newSession.PID, now)
		return err
	})
}

// UpdateLiveSessionDisplayName updates only the display_name field on a live session.
func (s *SessionStore) UpdateLiveSessionDisplayName(ctx context.Context, sessionID, displayName string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE live_sessions SET display_name = ? WHERE session_id = ?",
		displayName, sessionID)
	return err
}

// SetIcon sets or clears the emoji icon for a live session.
func (s *SessionStore) SetIcon(ctx context.Context, sessionID string, icon *string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE live_sessions SET icon = ? WHERE session_id = ?",
		icon, sessionID)
	return err
}

// GetIcons returns {session_id: icon} for sessions that have an icon set.
func (s *SessionStore) GetIcons(ctx context.Context, sessionIDs []string) (map[string]string, error) {
	if len(sessionIDs) == 0 {
		return map[string]string{}, nil
	}
	query, args, err := sqlx.In(
		"SELECT session_id, icon FROM live_sessions WHERE session_id IN (?) AND icon IS NOT NULL AND icon != ''",
		sessionIDs)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var sid, icon string
		if err := rows.Scan(&sid, &icon); err == nil {
			result[sid] = icon
		}
	}
	return result, nil
}

// ── Sleep/Wake ────────────────────────────────────────────────────────

// SetBoardSleeping sets is_sleeping for all sessions on a board. Returns the count of affected rows.
func (s *SessionStore) SetBoardSleeping(ctx context.Context, boardName string, sleeping bool) (int, error) {
	val := 0
	if sleeping {
		val = 1
	}
	result, err := s.db.ExecContext(ctx,
		"UPDATE live_sessions SET is_sleeping = ? WHERE board_name = ?",
		val, boardName)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// SetSessionSleeping sets is_sleeping for a single session.
func (s *SessionStore) SetSessionSleeping(ctx context.Context, sessionID string, sleeping bool) error {
	val := 0
	if sleeping {
		val = 1
	}
	_, err := s.db.ExecContext(ctx,
		"UPDATE live_sessions SET is_sleeping = ? WHERE session_id = ?",
		val, sessionID)
	return err
}

// GetSleepingBoardNames returns distinct board names that have at least one sleeping session.
func (s *SessionStore) GetSleepingBoardNames(ctx context.Context) ([]string, error) {
	var names []string
	err := s.db.SelectContext(ctx, &names,
		"SELECT DISTINCT board_name FROM live_sessions WHERE is_sleeping = 1 AND board_name IS NOT NULL")
	if err != nil {
		return nil, err
	}
	return names, nil
}

// CleanupOrphanedSleeping removes sleeping session rows where an awake
// version with the same display_name and board_name already exists.
// This cleans up duplicates from old wake code that created new sessions.
func (s *SessionStore) CleanupOrphanedSleeping(ctx context.Context) (int, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM live_sessions WHERE is_sleeping = 1
		AND session_id IN (
			SELECT sleeping.session_id FROM live_sessions sleeping
			INNER JOIN live_sessions awake
			ON sleeping.board_name = awake.board_name
			AND sleeping.display_name = awake.display_name
			AND sleeping.is_sleeping = 1
			AND awake.is_sleeping = 0
			AND sleeping.session_id != awake.session_id
		)`)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// ── Live Session Flags Helper ──────────────────────────────────────────

// ParseFlags deserializes a JSON flags string into a slice.
func ParseFlags(flagsJSON *string) []string {
	if flagsJSON == nil || *flagsJSON == "" {
		return nil
	}
	var flags []string
	if err := json.Unmarshal([]byte(*flagsJSON), &flags); err != nil {
		return nil
	}
	return flags
}

// MarshalFlags serializes a flags slice to JSON.
func MarshalFlags(flags []string) *string {
	if len(flags) == 0 {
		return nil
	}
	b, err := json.Marshal(flags)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}

// MarshalCapabilities serializes capabilities to a JSON string pointer for DB storage.
func MarshalCapabilities(caps any) *string {
	if caps == nil {
		return nil
	}
	b, err := json.Marshal(caps)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}

// UnmarshalFlags deserializes a JSON flags string to a slice.
func UnmarshalFlags(flags *string) []string {
	if flags == nil || *flags == "" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(*flags), &result); err != nil {
		return nil
	}
	return result
}

// UnmarshalMCPServers deserializes a JSON string to a map of MCP server configs.
func UnmarshalMCPServers(s *string) map[string]any {
	if s == nil || *s == "" {
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(*s), &result); err != nil {
		return nil
	}
	return result
}

// ── Helpers ────────────────────────────────────────────────────────────

var headerRE = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)

func extractFirstHeader(text string) string {
	if text == "" {
		return ""
	}
	m := headerRE.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// sanitizeFTSQuery translates a user query into a safe FTS5 expression.
func sanitizeFTSQuery(raw, mode string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	switch mode {
	case "phrase", "and", "or":
	default:
		mode = "phrase"
	}

	if mode == "phrase" {
		cleaned := strings.ReplaceAll(raw, `"`, " ")
		cleaned = strings.TrimSpace(cleaned)
		if cleaned == "" {
			return ""
		}
		return `"` + cleaned + `"`
	}

	// Tokenize: keep "quoted phrases" together, split bare words
	var tokens []string
	i := 0
	for i < len(raw) {
		if raw[i] == '"' {
			j := strings.IndexByte(raw[i+1:], '"')
			var end int
			if j == -1 {
				end = len(raw) - 1
			} else {
				end = i + 1 + j
			}
			tokens = append(tokens, raw[i:end+1])
			i = end + 1
		} else if raw[i] == ' ' || raw[i] == '\t' {
			i++
		} else {
			j := i
			for j < len(raw) && raw[j] != ' ' && raw[j] != '\t' && raw[j] != '"' {
				j++
			}
			word := raw[i:j]
			upper := strings.ToUpper(word)
			if upper != "AND" && upper != "OR" && upper != "NOT" {
				tokens = append(tokens, word)
			}
			i = j
		}
	}

	if len(tokens) == 0 {
		return ""
	}

	joiner := " AND "
	if mode == "or" {
		joiner = " OR "
	}
	return strings.Join(tokens, joiner)
}

func computeDuration(firstTS, lastTS *string) *int {
	if firstTS == nil || lastTS == nil || *firstTS == "" || *lastTS == "" {
		return nil
	}

	parseTS := func(ts string) (time.Time, error) {
		// Strip timezone suffix and fractional seconds for parsing
		base := ts
		for _, sep := range []string{"+", "Z"} {
			if idx := strings.Index(base, sep); idx > 0 {
				base = base[:idx]
			}
		}
		if idx := strings.Index(base, "."); idx > 0 {
			base = base[:idx]
		}
		return time.Parse("2006-01-02T15:04:05", base)
	}

	a, err := parseTS(*firstTS)
	if err != nil {
		return nil
	}
	b, err := parseTS(*lastTS)
	if err != nil {
		return nil
	}

	delta := int(math.Max(0, b.Sub(a).Seconds()))
	return &delta
}

// isoFormat matches Python's datetime.isoformat() which includes microseconds and uses +00:00 instead of Z for UTC.
const isoFormat = "2006-01-02T15:04:05.000000+00:00"

func nowUTC() string {
	return time.Now().UTC().Format(isoFormat)
}

// NowUTC returns the current time as an ISO 8601 UTC string (exported for use by route handlers).
func NowUTC() string {
	return nowUTC()
}
