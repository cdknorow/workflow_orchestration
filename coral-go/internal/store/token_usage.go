package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

// TokenUsage represents a token usage snapshot for a session.
type TokenUsage struct {
	ID           int64   `db:"id" json:"id"`
	SessionID    string  `db:"session_id" json:"session_id"`
	AgentName    string  `db:"agent_name" json:"agent_name"`
	AgentType    string  `db:"agent_type" json:"agent_type"`
	TeamID       *int64  `db:"team_id" json:"team_id,omitempty"`
	BoardName    *string `db:"board_name" json:"board_name,omitempty"`
	InputTokens      int `db:"input_tokens" json:"input_tokens"`
	OutputTokens     int `db:"output_tokens" json:"output_tokens"`
	CacheReadTokens  int `db:"cache_read_tokens" json:"cache_read_tokens"`
	CacheWriteTokens int `db:"cache_write_tokens" json:"cache_write_tokens"`
	TotalTokens      int `db:"total_tokens" json:"total_tokens"`
	CostUSD        float64 `db:"cost_usd" json:"cost_usd"`
	NumTurns       int     `db:"num_turns" json:"num_turns"`
	SessionStartAt string  `db:"session_start_at" json:"session_start_at,omitempty"`
	LastActivityAt string  `db:"last_activity_at" json:"last_activity_at,omitempty"`
	RecordedAt     string  `db:"recorded_at" json:"recorded_at"`
}

// UsageSummary represents aggregated token usage totals.
type UsageSummary struct {
	AgentType        string  `db:"agent_type" json:"agent_type"`
	InputTokens      int64   `db:"input_tokens" json:"input_tokens"`
	OutputTokens     int64   `db:"output_tokens" json:"output_tokens"`
	CacheReadTokens  int64   `db:"cache_read_tokens" json:"cache_read_tokens"`
	CacheWriteTokens int64   `db:"cache_write_tokens" json:"cache_write_tokens"`
	TotalTokens      int64   `db:"total_tokens" json:"total_tokens"`
	CostUSD          float64 `db:"cost_usd" json:"cost_usd"`
	NumSessions      int     `db:"num_sessions" json:"num_sessions"`
}

// UsageFilter specifies filters for ListUsage.
type UsageFilter struct {
	SessionID string
	TeamID    *int64
	BoardName string
	Since     string
}

// TokenUsageStore provides CRUD and aggregation for token usage records.
type TokenUsageStore struct {
	db *DB
}

// NewTokenUsageStore creates a new TokenUsageStore.
func NewTokenUsageStore(db *DB) *TokenUsageStore {
	return &TokenUsageStore{db: db}
}

// RecordUsage inserts a per-call token usage record. Each row represents a
// single API call's delta tokens at a specific timestamp. The composite key
// (session_id, recorded_at) ensures one row per call — duplicates are ignored.
func (s *TokenUsageStore) RecordUsage(ctx context.Context, u *TokenUsage) error {
	if u.RecordedAt == "" {
		u.RecordedAt = NowUTC()
	}
	if u.AgentType == "" {
		u.AgentType = "claude"
	}

	result, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO token_usage
		 (session_id, agent_name, agent_type, team_id, board_name, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_tokens, cost_usd, num_turns, session_start_at, last_activity_at, recorded_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.SessionID, u.AgentName, u.AgentType, u.TeamID, u.BoardName,
		u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens, u.TotalTokens, u.CostUSD, u.NumTurns, u.SessionStartAt, u.LastActivityAt, u.RecordedAt)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

// GetSessionUsage returns the latest token usage snapshot for a session.
func (s *TokenUsageStore) GetSessionUsage(ctx context.Context, sessionID string) (*TokenUsage, error) {
	var u TokenUsage
	err := s.db.GetContext(ctx, &u,
		`SELECT id, session_id, agent_name, agent_type, team_id, board_name,
		 input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_tokens, cost_usd, num_turns,
		 COALESCE(session_start_at, '') as session_start_at, COALESCE(last_activity_at, '') as last_activity_at, recorded_at
		 FROM token_usage WHERE session_id = ? ORDER BY id DESC LIMIT 1`, sessionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetTeamUsage returns aggregated token usage for a team.
// Uses the latest snapshot per session to avoid double-counting cumulative data.
func (s *TokenUsageStore) GetTeamUsage(ctx context.Context, teamID int64) (*UsageSummary, error) {
	var summary UsageSummary
	err := s.db.GetContext(ctx, &summary,
		`SELECT COALESCE(SUM(input_tokens), 0) as input_tokens,
		        COALESCE(SUM(output_tokens), 0) as output_tokens,
		        COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens,
		        COALESCE(SUM(cache_write_tokens), 0) as cache_write_tokens,
		        COALESCE(SUM(total_tokens), 0) as total_tokens,
		        COALESCE(SUM(cost_usd), 0) as cost_usd,
		        COUNT(*) as num_sessions
		 FROM (
		   SELECT session_id, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_tokens, cost_usd,
		          ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY id DESC) as rn
		   FROM token_usage WHERE team_id = ?
		 ) WHERE rn = 1`, teamID)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

// GetBoardUsage returns aggregated token usage for a board.
func (s *TokenUsageStore) GetBoardUsage(ctx context.Context, boardName string) (*UsageSummary, error) {
	var summary UsageSummary
	err := s.db.GetContext(ctx, &summary,
		`SELECT COALESCE(SUM(input_tokens), 0) as input_tokens,
		        COALESCE(SUM(output_tokens), 0) as output_tokens,
		        COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens,
		        COALESCE(SUM(cache_write_tokens), 0) as cache_write_tokens,
		        COALESCE(SUM(total_tokens), 0) as total_tokens,
		        COALESCE(SUM(cost_usd), 0) as cost_usd,
		        COUNT(*) as num_sessions
		 FROM (
		   SELECT session_id, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_tokens, cost_usd,
		          ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY id DESC) as rn
		   FROM token_usage WHERE board_name = ?
		 ) WHERE rn = 1`, boardName)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

// GetUsageSummary returns totals grouped by agent_type since a given time.
func (s *TokenUsageStore) GetUsageSummary(ctx context.Context, since string) ([]UsageSummary, error) {
	var summaries []UsageSummary
	query := `SELECT agent_type,
	          COALESCE(SUM(input_tokens), 0) as input_tokens,
	          COALESCE(SUM(output_tokens), 0) as output_tokens,
	          COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens,
	          COALESCE(SUM(cache_write_tokens), 0) as cache_write_tokens,
	          COALESCE(SUM(total_tokens), 0) as total_tokens,
	          COALESCE(SUM(cost_usd), 0) as cost_usd,
	          COUNT(DISTINCT session_id) as num_sessions
	   FROM (
	     SELECT session_id, agent_type, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_tokens, cost_usd,
	            ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY id DESC) as rn
	     FROM token_usage`
	var args []interface{}
	if since != "" {
		query += " WHERE recorded_at >= ?"
		args = append(args, since)
	}
	query += `) WHERE rn = 1 GROUP BY agent_type`
	err := s.db.SelectContext(ctx, &summaries, query, args...)
	return summaries, err
}

// ListUsage returns filtered token usage records (latest per session).
func (s *TokenUsageStore) ListUsage(ctx context.Context, f UsageFilter) ([]TokenUsage, error) {
	query := `SELECT id, session_id, agent_name, agent_type, team_id, board_name,
	          input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_tokens, cost_usd, num_turns, recorded_at
	   FROM (
	     SELECT *, ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY id DESC) as rn
	     FROM token_usage WHERE 1=1`
	var args []interface{}

	if f.SessionID != "" {
		query += " AND session_id = ?"
		args = append(args, f.SessionID)
	}
	if f.TeamID != nil {
		query += " AND team_id = ?"
		args = append(args, *f.TeamID)
	}
	if f.BoardName != "" {
		query += " AND board_name = ?"
		args = append(args, f.BoardName)
	}
	if f.Since != "" {
		query += " AND recorded_at >= ?"
		args = append(args, f.Since)
	}

	query += `) WHERE rn = 1 ORDER BY recorded_at DESC`

	var results []TokenUsage
	err := s.db.SelectContext(ctx, &results, query, args...)
	return results, err
}

// GetLatestUsageBySessionIDs returns the latest token usage record per session
// for the given session IDs, keyed by session_id.
func (s *TokenUsageStore) GetLatestUsageBySessionIDs(ctx context.Context, sessionIDs []string) (map[string]*TokenUsage, error) {
	if len(sessionIDs) == 0 {
		return map[string]*TokenUsage{}, nil
	}

	query, args, err := sqlx.In(
		`SELECT id, session_id, agent_name, agent_type, team_id, board_name,
		 input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_tokens, cost_usd, num_turns, recorded_at
		 FROM (
		   SELECT *, ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY id DESC) as rn
		   FROM token_usage WHERE session_id IN (?)
		 ) WHERE rn = 1`, sessionIDs)
	if err != nil {
		return nil, err
	}

	var records []TokenUsage
	if err := s.db.SelectContext(ctx, &records, query, args...); err != nil {
		return nil, err
	}

	result := make(map[string]*TokenUsage, len(records))
	for i := range records {
		result[records[i].SessionID] = &records[i]
	}
	return result, nil
}
