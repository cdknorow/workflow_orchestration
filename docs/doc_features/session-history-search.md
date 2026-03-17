# Doc Feature Guide: Session History & Search

## Overview

Coral automatically indexes every Claude and Gemini coding session into a searchable SQLite database with FTS5 full-text search. Sessions are discovered from agent history files, indexed with their full message text, and queued for AI-powered summarization. The result is a browsable, searchable archive of all coding conversations.

---

## Key Source Files & Architecture

| File | Role |
|------|------|
| `src/coral/store/sessions.py` | SQLite queries for session index, FTS5 search, tags, notes, summaries |
| `src/coral/api/history.py` | FastAPI routes for session browsing, search, tags, notes, and summaries |
| `src/coral/background_tasks/session_indexer.py` | Background service — scans agent history files every 120s, upserts into session index and FTS |
| `src/coral/background_tasks/auto_summarizer.py` | Background service — processes summarizer queue, generates markdown summaries via Claude CLI |
| `src/coral/tools/jsonl_reader.py` | Parses Claude JSONL and Gemini JSON session files into structured messages |
| `src/coral/agents/claude.py` | Claude-specific session file discovery (`~/.claude/projects/**/*.jsonl`) |
| `src/coral/agents/gemini.py` | Gemini-specific session file discovery (`~/.gemini/tmp/*/chats/session-*.json`) |

### Database Tables

| Table | Purpose |
|-------|---------|
| `session_index` | Session metadata — session_id, source type, source file, timestamps, message count, display summary, file mtime |
| `session_fts` | FTS5 virtual table — full text of all messages for search |
| `session_meta` | User-editable metadata — notes, auto_summary, is_user_edited, display_name |
| `summarizer_queue` | Queue for pending AI summarizations |
| `tags` | Tag definitions — name and color |
| `session_tags` | Many-to-many linking sessions to tags |

### Indexing Pipeline

1. **SessionIndexer** scans all known history file paths every 120 seconds
2. Tracks each file's `file_mtime` — skips unchanged files
3. New/updated sessions are upserted into `session_index` table
4. Full message text is inserted into `session_fts` FTS5 virtual table
5. Each newly indexed session is enqueued in `summarizer_queue`
6. **BatchSummarizer** polls the queue and processes up to 5 sessions at a time
7. Summaries are generated via `claude --print --model haiku` and stored in `session_meta.auto_summary`

### Search Architecture

- FTS5 with Porter stemming tokenizer — "testing" matches "test", "tests", "tested"
- Three search modes: Phrase, All Words (AND), Any Word (OR)
- Advanced filters: date range, duration, source type, tags (with AND/OR logic)
- Filter state encoded in URL for bookmarking/sharing

---

## User-Facing Functionality & Workflows

### Browsing History

- History section in sidebar lists all sessions sorted by most recent activity
- Each entry shows: timestamp, duration, agent type badge, branch, tags, summary title
- Pagination at 50 sessions per page

### Searching Sessions

- Search box at top of History sidebar
- FTS5 relevance ranking when a query is active
- Searches full message body — code snippets, error messages, file paths, natural language

### Advanced Filters

- Click funnel icon next to search box
- Filters: FTS Mode, Source (Claude/Gemini), Tags (AND/OR), Date Range, Duration
- Active filter count badge on funnel icon
- Clear All button to reset

### Viewing a Session

Session detail view has six tabs:
1. **Summary** — AI-generated markdown summary, with Edit and Re-summarize buttons
2. **Chat** — Full conversation transcript with tool-use cards
3. **Activity** — Event timeline and bar chart of agent actions
4. **Tasks** — Read-only task checklist from the session
5. **Notes** — Agent-written notes
6. **Commits** — Git commits during the session's time range

### Tagging

- Click **+Tag** in session header
- Select existing tag or create new (name + color)
- Tags appear as colored pills in sidebar and header
- Filter by tags in advanced filter panel

### Resuming Sessions

- Click **Resume** on any historical Claude session
- Select a live agent to continue the session
- Agent restarts with `--resume` and full conversation context

---

## Suggested MkDocs Page Structure

### Title: "Session History & Search"

1. **Introduction** — What's indexed and why it matters
2. **Browsing History** — Sidebar list, pagination, entry details
   - Screenshot: History sidebar with session entries
3. **Searching Sessions** — Search box, FTS5 capabilities
   - Search syntax: phrase, all words, any word modes
   - Porter stemming examples
   - What's indexed (full message text)
4. **Advanced Filters** — Filter panel walkthrough
   - FTS mode, source, tags, date range, duration
   - Screenshot: Filter panel expanded
5. **Viewing a Session** — Detail view with all six tabs
   - Summary (with Edit/Re-summarize)
   - Chat transcript with tool cards
   - Activity timeline
   - Tasks, Notes, Commits
   - Screenshot: Session detail view
6. **Tagging Sessions** — Creating and using tags
7. **Resuming Sessions** — How to continue historical sessions on live agents
8. **How Indexing Works** — Pipeline architecture
   - SessionIndexer, FTS5, BatchSummarizer
   - Manual re-index API endpoint
9. **Configuration** — Database location, indexer interval, summarizer settings

### Screenshots to Include

- History sidebar with session entries, tags, and search results
- Advanced filter panel
- Session detail view showing Summary tab
- Chat tab with tool-use cards
- Tag creation modal

### Code Examples

- Manual re-index API call: `curl -X POST http://localhost:8420/api/indexer/refresh`
- Search API examples

---

## Important Details for Technical Writer

1. **FTS5 safety**: Operator tokens (AND, OR, NOT) entered as bare words are stripped to prevent query injection.
2. **User edits preserve**: If a user manually edits a summary, the auto-summarizer will not overwrite it. The `is_user_edited` flag in `session_meta` controls this.
3. **Summarizer model**: Uses `claude --print --model haiku` for cost efficiency. Processes up to 5 sessions at a time.
4. **File mtime tracking**: The indexer uses file modification time to skip unchanged files, making the 120-second scan efficient.
5. **Quoted sub-phrases**: Users can mix quoted phrases with individual words in All Words or Any Word mode.
6. **URL-encoded filters**: Filter state is encoded in the URL, enabling bookmarks and shared links.
7. **Resume is Claude-only**: Gemini does not support session resume.
8. **Re-summarize**: Discards the current auto-summary and re-queues. If the user has manual edits, those take priority in display.
