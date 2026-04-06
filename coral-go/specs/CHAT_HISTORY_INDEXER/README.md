# Chat History Indexer

## Problem

The Chat History page showed 0 sessions. The `session_index` table was empty because the `SessionIndexer` in `internal/background/indexer.go` was created with `scanners: nil` in `internal/startup/startup.go`. The indexer ran on its interval loop but had no scanners to discover session files, so it never indexed anything.

## Design

### HistoryScanner Interface

Each agent type implements the `HistoryScanner` interface (defined in `internal/agent/agent.go`):

```go
type HistoryScanner interface {
    HistoryBasePath() string
    HistoryGlobPattern() string
    ExtractSessions(basePath string, knownMtimes map[string]float64) ([]IndexedSession, error)
}
```

`ExtractSessions` walks the agent's history directory, stats each file against `knownMtimes` to skip unchanged files, parses only new/modified files, and returns metadata with per-file `SourceFile` and `FileMtime` for mtime tracking.

### Startup Wiring

In `internal/startup/startup.go`, the indexer is created with all three agent scanners:

```go
scanners := []agent.HistoryScanner{
    &agent.ClaudeAgent{},
    &agent.GeminiAgent{},
    &agent.CodexAgent{},
}
indexer := background.NewSessionIndexer(sessStore, scanners, ...)
```

### Indexer Loop

`SessionIndexer.Run()` waits for a startup delay, then calls `RunOnce()` on a configurable interval. The indexer runs asynchronously in a goroutine via `safeGo` and does not block app startup.

Each pass:
1. Fetches known file mtimes from `session_index` via `GetIndexedMtimes()`
2. Passes `knownMtimes` to each scanner's `ExtractSessions(basePath, knownMtimes)`
3. Scanners skip files whose mtime hasn't changed since last index (avoids re-parsing unchanged files)
4. Only changed/new sessions are upserted into `session_index` and `session_fts`

## Data Flow

```
Agent history files on disk
  → stat() file mtime → skip if unchanged (mtime matches knownMtimes)
  → HistoryScanner.ExtractSessions(basePath, knownMtimes)
    → []IndexedSession (with SourceFile + FileMtime per entry)
      → SessionStore.UpsertSessionIndex()
        → session_index table
          → SessionStore.ListSessionsPaged()
            → GET /api/sessions
              → Frontend Chat History page
```

FTS indexing (when `FTSBody` is populated):
```
IndexedSession.FTSBody → SessionStore.UpsertFTS() → session_fts virtual table → search queries
```

## Agent Implementations

### Claude (`ClaudeAgent`)

- **Base path**: `~/.claude/projects/` (or `$CLAUDE_PROJECTS_DIR`)
- **Structure**: `<base>/<encoded-workdir>/<session-id>.jsonl`
- **Parsing**: Walks all project subdirectories, globs `*.jsonl` files, reads each line as JSON. Extracts `timestamp` and `type` fields. Counts `user`/`assistant` messages. First assistant text block becomes the display summary.
- **Session ID**: Derived from filename (strip `.jsonl` extension).

### Gemini (`GeminiAgent`)

- **Base path**: `~/.gemini/tmp/` (or `$GEMINI_TMP_DIR`)
- **Structure**: `<base>/<uuid>/chats/session-<id>.json`
- **Parsing**: Each file is a JSON array of message objects with `role`, `parts`, and `timestamp` fields. First `model` response text becomes the display summary.
- **Session ID**: Extracted from filename (`session-<id>.json` → `<id>`).

### Codex (`CodexAgent`)

- **Base path**: `~/.codex/sessions/` (or `$CODEX_HOME/sessions/`)
- **Structure**: `<base>/YYYY/MM/DD/rollout-<timestamp>-<id>.jsonl`
- **Parsing**: Uses `filepath.Walk` to find all `rollout-*.jsonl` files. Each line is a JSON object with `role` and `timestamp` fields. First `assistant` text becomes the display summary.
- **Session ID**: Full filename without extension (e.g., `rollout-1234567890-abc123`).

## Architecture Decision

The `HistoryScanner` interface and `IndexedSession` type are defined in the `agent` package rather than `background`. This avoids an import cycle: `background` already imports `agent` (for `launcher.go` and `workflow_runner.go`), so `agent` cannot import `background`.

## Schema

### session_index

```sql
CREATE TABLE IF NOT EXISTS session_index (
    session_id      TEXT PRIMARY KEY,
    source_type     TEXT NOT NULL,       -- "claude", "gemini", "codex"
    source_file     TEXT NOT NULL,       -- full path to the history file
    first_timestamp TEXT,                -- ISO 8601 timestamp of first message
    last_timestamp  TEXT,                -- ISO 8601 timestamp of last message
    message_count   INTEGER DEFAULT 0,
    display_summary TEXT DEFAULT '',      -- first assistant response (truncated)
    indexed_at      TEXT NOT NULL,        -- when this row was indexed
    file_mtime      REAL NOT NULL         -- file modification time at indexing
);

CREATE INDEX IF NOT EXISTS idx_session_index_last_ts
    ON session_index(last_timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_session_index_first_ts
    ON session_index(first_timestamp);
```

### session_fts

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS session_fts
    USING fts5(session_id, body, tokenize='porter');
```

Full-text search over session content. Populated when `IndexedSession.FTSBody` is non-empty. Queried via `ListSessionsPaged` when a search term is provided.
