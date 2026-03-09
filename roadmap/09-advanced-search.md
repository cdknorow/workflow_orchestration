# 09 — Advanced Search Filters

## 1. Goal

The current history search (`GET /api/sessions/history`) accepts a single free-text query `q`, one `tag_id`, and a `source_type` enum. This is enough for casual lookup but falls short when a user wants to ask questions like:

- "Show me all Claude sessions tagged *bug-fix* OR *refactor* in the last 30 days"
- "Find sessions that mention 'auth' AND 'migration' and ran for more than 10 minutes"
- "All Gemini sessions in February where my notes contain 'regression'"

The feature adds:

| Filter | What it adds |
|---|---|
| Date range picker | `date_from` / `date_to` boundaries on `last_timestamp` |
| Agent type multi-select | Extends existing `source_type` single-value to a list |
| Duration filter | Min/max session duration derived from `first_timestamp`…`last_timestamp` |
| Tag combination filter | Multiple tags with AND/OR logic |
| FTS AND/OR/NOT operators | Expose FTS5 query syntax safely via a mode toggle |
| URL state persistence | Full filter state in query string, shareable and bookmarkable |

The value is discoverability and cross-session research. Corral accumulates hundreds of sessions over weeks of parallel agent work, and the single keyword box does not support the nuanced narrowing that power users need.

---

## 2. Existing Code That Changes

| File | Current role | What changes |
|---|---|---|
| `src/corral/store/sessions.py` | `list_sessions_paged()` at line 253 | Add 7 new parameters; extend query builder |
| `src/corral/store/connection.py` | `_ensure_schema()` at line 41 | Add 2 new indexes |
| `src/corral/api/history.py` | `GET /api/sessions/history` at line 27 | Accept and forward new `Query` params |
| `src/corral/static/api.js` | `loadHistorySessionsPaged()` at line 28 | Delegate param building to `search_filters.js` |
| `src/corral/static/app.js` | Filter state block at lines 85–89 | Replace old state variables; wire new UI functions |
| `src/corral/static/render.js` | `renderHistorySessions()` at line 121 | Display duration alongside timestamp in sidebar |
| `src/corral/templates/includes/sidebar.html` | `.history-filters` block at lines 14–26 | Expand to full advanced filter panel |
| `src/corral/static/style.css` | `.history-filters` rules at line 171 | Add `.hf-*` styles for filter panel components |

New file:

| File | Purpose |
|---|---|
| `src/corral/static/search_filters.js` | Filter state, URL serialization/deserialization, `buildApiParams()` |

---

## 3. Backend Changes

### 3.1 `src/corral/store/sessions.py` — `list_sessions_paged()`

Replace the current signature at line 253:

```python
async def list_sessions_paged(
    self,
    page: int = 1,
    page_size: int = 50,
    search: str | None = None,
    tag_id: int | None = None,        # legacy — kept for compatibility
    source_type: str | None = None,   # legacy — kept for compatibility
) -> dict[str, Any]:
```

With the extended version:

```python
async def list_sessions_paged(
    self,
    page: int = 1,
    page_size: int = 50,
    search: str | None = None,
    fts_mode: str = "phrase",              # "phrase" | "and" | "or"
    # Legacy single-value aliases (merged with list variants below)
    tag_id: int | None = None,
    source_type: str | None = None,
    # New multi-value filters
    tag_ids: list[int] | None = None,
    tag_logic: str = "AND",               # "AND" | "OR"
    source_types: list[str] | None = None,
    date_from: str | None = None,         # "YYYY-MM-DD"
    date_to: str | None = None,           # "YYYY-MM-DD"
    min_duration_sec: int | None = None,
    max_duration_sec: int | None = None,
) -> dict[str, Any]:
```

At the top of the method body, merge the legacy aliases:

```python
# Merge legacy single-value aliases into list variants
effective_tag_ids: list[int] = list(tag_ids or [])
if tag_id is not None and tag_id not in effective_tag_ids:
    effective_tag_ids.append(tag_id)

effective_source_types: list[str] = list(source_types or [])
if source_type and source_type not in effective_source_types:
    effective_source_types.append(source_type)

# Validate fts_mode
if fts_mode not in ("phrase", "and", "or"):
    fts_mode = "phrase"
```

Then use `effective_tag_ids` and `effective_source_types` in all query clauses.

#### Query builder additions

**FTS search with mode** — Sanitize the user query before binding it. The `ORDER BY rank` stays for text search; falls back to `last_timestamp DESC` otherwise:

```python
if search:
    safe_q = _sanitize_fts_query(search, fts_mode)
    if safe_q:
        from_clause += " JOIN session_fts fts ON fts.session_id = si.session_id"
        where_clauses.append("fts MATCH ?")
        params.append(safe_q)
        order_clause = "rank"
```

Note: the current code uses `"session_fts MATCH ?"` with a table-name reference while joining with alias `fts`. Both work in SQLite FTS5, but using the alias `fts` is more consistent.

**Date range** — `last_timestamp` is stored as ISO-8601; SQLite lexicographic comparison works correctly:

```python
if date_from:
    where_clauses.append("si.last_timestamp >= ?")
    params.append(date_from + "T00:00:00")

if date_to:
    where_clauses.append("si.last_timestamp <= ?")
    params.append(date_to + "T23:59:59")
```

**Duration** — Computed inline using `julianday()`. Rows with NULL timestamps produce NULL from the arithmetic and fail the comparison, so they are correctly excluded:

```python
if min_duration_sec is not None:
    where_clauses.append(
        "(julianday(si.last_timestamp) - julianday(si.first_timestamp)) * 86400 >= ?"
    )
    params.append(min_duration_sec)

if max_duration_sec is not None:
    where_clauses.append(
        "(julianday(si.last_timestamp) - julianday(si.first_timestamp)) * 86400 <= ?"
    )
    params.append(max_duration_sec)
```

**Tag AND logic** — One subquery per tag, each narrowing the result set:

```python
if effective_tag_ids and tag_logic == "AND":
    for tid in effective_tag_ids:
        where_clauses.append(
            "si.session_id IN (SELECT session_id FROM session_tags WHERE tag_id = ?)"
        )
        params.append(tid)
```

**Tag OR logic** — Single IN subquery:

```python
elif effective_tag_ids and tag_logic == "OR":
    ph = ",".join("?" for _ in effective_tag_ids)
    where_clauses.append(
        f"si.session_id IN (SELECT session_id FROM session_tags WHERE tag_id IN ({ph}))"
    )
    params.extend(effective_tag_ids)
```

**Multi-source** — SQLite `IN` clause replaces the current `== ?` check:

```python
if effective_source_types:
    ph = ",".join("?" for _ in effective_source_types)
    where_clauses.append(f"si.source_type IN ({ph})")
    params.extend(effective_source_types)
```

**Duration in response** — Add `duration_sec` to each session dict returned:

```python
sessions.append({
    ...existing fields...,
    "duration_sec": _compute_duration(r["first_timestamp"], r["last_timestamp"]),
})
```

#### New module-level helpers

Add these two functions at module level in `store/sessions.py`, just below the `_extract_first_header()` function at line 12:

```python
def _sanitize_fts_query(raw: str, mode: str = "phrase") -> str:
    """Translate a plain user query into a safe FTS5 expression.

    mode='phrase' → "full phrase in quotes"
    mode='and'    → token1 AND token2 AND ...
    mode='or'     → token1 OR token2 OR ...

    Existing quoted sub-phrases in the input are preserved intact.
    FTS5 operator tokens (AND, OR, NOT) entered as bare words by the user
    are dropped to prevent injection.
    Returns empty string for blank input (caller skips the MATCH clause).
    """
    raw = raw.strip()
    if not raw:
        return ""

    if mode not in ("phrase", "and", "or"):
        mode = "phrase"

    if mode == "phrase":
        # Strip internal double-quotes (FTS5 does not support escaping
        # quotes inside phrase queries) and wrap in a single phrase.
        cleaned = raw.replace('"', ' ').strip()
        return f'"{cleaned}"' if cleaned else ""

    # Tokenise: keep "quoted phrases" together, split bare words
    tokens: list[str] = []
    i = 0
    while i < len(raw):
        if raw[i] == '"':
            j = raw.find('"', i + 1)
            end = j if j != -1 else len(raw) - 1
            tokens.append(raw[i : end + 1])
            i = end + 1
        elif raw[i].isspace():
            i += 1
        else:
            j = i
            while j < len(raw) and not raw[j].isspace() and raw[j] != '"':
                j += 1
            word = raw[i:j]
            # Drop bare operator keywords — they are not safe to pass through
            if word.upper() not in ("AND", "OR", "NOT"):
                tokens.append(word)
            i = j

    if not tokens:
        return ""

    joiner = " AND " if mode == "and" else " OR "
    return joiner.join(tokens)


def _compute_duration(first_ts: str | None, last_ts: str | None) -> int | None:
    """Return session duration in seconds, or None if timestamps are missing/invalid."""
    if not first_ts or not last_ts:
        return None
    try:
        from datetime import datetime
        # fromisoformat handles timezone suffixes in Python 3.11+.
        # For robustness, strip fractional seconds and timezone info
        # (all Corral timestamps are local/UTC with no offset).
        def _parse(ts: str) -> datetime:
            # Strip timezone suffix (+HH:MM, Z) and fractional seconds
            base = ts.split("+")[0].split("Z")[0]
            dot = base.find(".")
            if dot != -1:
                base = base[:dot]
            return datetime.fromisoformat(base)

        a = _parse(first_ts)
        b = _parse(last_ts)
        delta = int((b - a).total_seconds())
        return max(0, delta)
    except Exception:
        return None
```

#### New indexes in `_ensure_schema()` (`src/corral/store/connection.py`)

Add these after the existing `idx_session_index_last_ts` index (line 79), inside the same schema SQL block:

```sql
CREATE INDEX IF NOT EXISTS idx_session_tags_tag_id
    ON session_tags(tag_id);

CREATE INDEX IF NOT EXISTS idx_session_index_first_ts
    ON session_index(first_timestamp);
```

If adding inside the multi-statement block isn't feasible, add them as separate `await conn.execute()` calls after the `await conn.commit()`:

```python
for ddl in [
    "CREATE INDEX IF NOT EXISTS idx_session_tags_tag_id ON session_tags(tag_id)",
    "CREATE INDEX IF NOT EXISTS idx_session_index_first_ts ON session_index(first_timestamp)",
]:
    try:
        await conn.execute(ddl)
    except Exception:
        pass
await conn.commit()
```

### 3.2 `src/corral/api/history.py` — `GET /api/sessions/history`

Replace the endpoint at line 27 with the expanded version. The cold-start fallback logic is preserved exactly as-is, with the new parameters forwarded:

```python
@router.get("/api/sessions/history")
async def get_history_sessions(
    page: int = Query(1, ge=1),
    page_size: int = Query(50, ge=1, le=200),
    q: Optional[str] = Query(None),
    fts_mode: str = Query("phrase"),
    # Legacy single-value params (backward compat)
    tag_id: Optional[int] = Query(None),
    source_type: Optional[str] = Query(None),
    # New multi-value params (comma-separated strings)
    tag_ids: Optional[str] = Query(None),
    tag_logic: str = Query("AND"),
    source_types: Optional[str] = Query(None),
    date_from: Optional[str] = Query(None),
    date_to: Optional[str] = Query(None),
    min_duration_sec: Optional[int] = Query(None, ge=0),
    max_duration_sec: Optional[int] = Query(None, ge=0),
):
    """Paginated history sessions with advanced search filters."""
    import re as _re

    # Parse comma-separated tag_ids
    resolved_tag_ids: list[int] = []
    if tag_ids:
        resolved_tag_ids = [int(x) for x in tag_ids.split(",") if x.strip().isdigit()]
    if tag_id is not None and tag_id not in resolved_tag_ids:
        resolved_tag_ids.append(tag_id)

    # Parse comma-separated source_types
    resolved_source_types: list[str] | None = None
    if source_types:
        resolved_source_types = [s.strip() for s in source_types.split(",") if s.strip()]
    elif source_type:
        resolved_source_types = [source_type]

    # Validate date format
    _DATE_RE = _re.compile(r"^\d{4}-\d{2}-\d{2}$")
    if date_from and not _DATE_RE.match(date_from):
        date_from = None
    if date_to and not _DATE_RE.match(date_to):
        date_to = None
    if date_from and date_to and date_from > date_to:
        date_from, date_to = date_to, date_from

    # Guard duration bounds
    if min_duration_sec is not None and max_duration_sec is not None:
        if min_duration_sec > max_duration_sec:
            min_duration_sec, max_duration_sec = max_duration_sec, min_duration_sec

    # Normalize tag_logic and fts_mode
    if tag_logic not in ("AND", "OR"):
        tag_logic = "AND"
    if fts_mode not in ("phrase", "and", "or"):
        fts_mode = "phrase"

    # Build keyword args for the new parameters
    advanced_kwargs = dict(
        fts_mode=fts_mode,
        tag_ids=resolved_tag_ids or None,
        tag_logic=tag_logic,
        source_types=resolved_source_types,
        date_from=date_from,
        date_to=date_to,
        min_duration_sec=min_duration_sec,
        max_duration_sec=max_duration_sec,
    )

    result = await store.list_sessions_paged(page, page_size, q, **advanced_kwargs)

    # Cold-start fallback — preserved from existing logic
    has_any_filter = (
        q or resolved_tag_ids or resolved_source_types
        or date_from or date_to
        or min_duration_sec is not None or max_duration_sec is not None
    )

    if result["total"] == 0 and not has_any_filter:
        indexer = (
            getattr(_app, "state", None)
            and getattr(_app.state, "indexer", None)
            if _app else None
        )
        if indexer:
            try:
                await indexer.run_once()
                result = await store.list_sessions_paged(
                    page, page_size, q, **advanced_kwargs
                )
            except Exception:
                pass

        # If still empty, fall back to old file-scan method
        if result["total"] == 0:
            sessions = load_history_sessions()
            metadata = await store.get_all_session_metadata()
            for s in sessions:
                meta = metadata.get(s["session_id"])
                if meta:
                    s["tags"] = meta["tags"]
                    s["has_notes"] = meta["has_notes"]
                else:
                    s["tags"] = []
                    s["has_notes"] = False
            return {
                "sessions": sessions,
                "total": len(sessions),
                "page": 1,
                "page_size": len(sessions),
            }

    return result
```

---

## 4. Frontend Changes

### 4.1 `src/corral/static/search_filters.js` (new file)

This file is an ES module imported by `app.js`. It does **not** need its own `<script>` tag in `index.html` — the existing `<script type="module" src="/static/app.js">` resolves ES module imports automatically.

```javascript
/* Advanced search filter state and URL serialization */

export const filterState = {
    q: '',
    ftsMode: 'phrase',       // 'phrase' | 'and' | 'or'
    tagIds: [],              // array of int
    tagLogic: 'AND',         // 'AND' | 'OR'
    sourceTypes: [],         // array of string; empty = all
    dateFrom: '',            // 'YYYY-MM-DD' or ''
    dateTo: '',              // 'YYYY-MM-DD' or ''
    minDurationSec: null,    // int or null
    maxDurationSec: null,    // int or null
};

export function buildApiParams(page, pageSize) {
    const p = new URLSearchParams({ page, page_size: pageSize });
    if (filterState.q)
        p.set('q', filterState.q);
    if (filterState.q && filterState.ftsMode !== 'phrase')
        p.set('fts_mode', filterState.ftsMode);
    if (filterState.tagIds.length)
        p.set('tag_ids', filterState.tagIds.join(','));
    if (filterState.tagIds.length > 1)
        p.set('tag_logic', filterState.tagLogic);
    if (filterState.sourceTypes.length)
        p.set('source_types', filterState.sourceTypes.join(','));
    if (filterState.dateFrom)
        p.set('date_from', filterState.dateFrom);
    if (filterState.dateTo)
        p.set('date_to', filterState.dateTo);
    if (filterState.minDurationSec != null)
        p.set('min_duration_sec', String(filterState.minDurationSec));
    if (filterState.maxDurationSec != null)
        p.set('max_duration_sec', String(filterState.maxDurationSec));
    return p;
}

export function serializeToUrl(page) {
    const params = buildApiParams(page, 50);
    const url = new URL(window.location.href);
    url.search = params.toString();
    window.history.replaceState(null, '', url.toString());
}

export function deserializeFromUrl() {
    const p = new URLSearchParams(window.location.search);
    filterState.q = p.get('q') || '';
    filterState.ftsMode = ['phrase', 'and', 'or'].includes(p.get('fts_mode'))
        ? p.get('fts_mode') : 'phrase';
    filterState.tagIds = (p.get('tag_ids') || '')
        .split(',').filter(Boolean).map(Number).filter(n => !isNaN(n));
    filterState.tagLogic = p.get('tag_logic') === 'OR' ? 'OR' : 'AND';
    filterState.sourceTypes = (p.get('source_types') || '')
        .split(',').filter(Boolean);
    filterState.dateFrom = p.get('date_from') || '';
    filterState.dateTo = p.get('date_to') || '';
    filterState.minDurationSec = p.has('min_duration_sec')
        ? parseInt(p.get('min_duration_sec')) : null;
    filterState.maxDurationSec = p.has('max_duration_sec')
        ? parseInt(p.get('max_duration_sec')) : null;

    // Restore page number (default 1)
    const pageVal = parseInt(p.get('page'));
    return { filterState, page: isNaN(pageVal) || pageVal < 1 ? 1 : pageVal };
}

export function resetFilters() {
    filterState.q = '';
    filterState.ftsMode = 'phrase';
    filterState.tagIds = [];
    filterState.tagLogic = 'AND';
    filterState.sourceTypes = [];
    filterState.dateFrom = '';
    filterState.dateTo = '';
    filterState.minDurationSec = null;
    filterState.maxDurationSec = null;
}

export function hasActiveFilters() {
    return filterState.tagIds.length > 0
        || filterState.sourceTypes.length > 0
        || !!filterState.dateFrom
        || !!filterState.dateTo
        || filterState.minDurationSec != null
        || filterState.maxDurationSec != null;
}

export function countActiveFilters() {
    let n = 0;
    if (filterState.tagIds.length) n++;
    if (filterState.sourceTypes.length) n++;
    if (filterState.dateFrom || filterState.dateTo) n++;
    if (filterState.minDurationSec != null || filterState.maxDurationSec != null) n++;
    return n;
}
```

### 4.2 `src/corral/static/api.js` — update `loadHistorySessionsPaged()`

Replace the function at line 28. Remove the old `search`, `tagId`, `sourceType` parameters entirely:

```javascript
import { buildApiParams } from './search_filters.js';

export async function loadHistorySessionsPaged(page = 1, pageSize = 50) {
    try {
        const params = buildApiParams(page, pageSize);
        const resp = await fetch(`/api/sessions/history?${params}`);
        const data = await resp.json();
        const sessions = data.sessions || data;
        renderHistorySessions(sessions, data.total, data.page, data.page_size);
        return data;
    } catch (e) {
        console.error("Failed to load paged history sessions:", e);
        return null;
    }
}
```

### 4.3 `src/corral/templates/includes/sidebar.html` — expand filter panel

Replace lines 14–26 (the current `.history-filters` block) with:

```html
<div class="history-filters">
    <!-- Row 1: Text search + advanced toggle -->
    <div class="hf-search-row">
        <input id="history-search" type="search" placeholder="Search sessions...">
        <button id="hf-adv-toggle" class="btn btn-small hf-adv-btn"
                title="Advanced filters">
            <svg width="12" height="12" viewBox="0 0 16 16" fill="none"
                 stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
                <line x1="2" y1="4" x2="14" y2="4"/>
                <line x1="4" y1="8" x2="12" y2="8"/>
                <line x1="6" y1="12" x2="10" y2="12"/>
            </svg>
            <span id="hf-active-count" class="hf-active-badge"
                  style="display:none">0</span>
        </button>
    </div>

    <!-- Advanced panel (collapsed by default) -->
    <div id="hf-advanced" class="hf-advanced" style="display:none">

        <!-- FTS mode (shown only when search box has text) -->
        <div class="hf-row" id="hf-fts-mode-row" style="display:none">
            <label class="hf-label">Match</label>
            <div class="hf-toggle-group">
                <button class="hf-toggle active" data-mode="phrase">Phrase</button>
                <button class="hf-toggle" data-mode="and">All words</button>
                <button class="hf-toggle" data-mode="or">Any word</button>
            </div>
        </div>

        <!-- Source multi-select -->
        <div class="hf-row">
            <label class="hf-label">Source</label>
            <div class="hf-toggle-group">
                <button class="hf-toggle active" data-source="all">All</button>
                <button class="hf-toggle" data-source="claude">Claude</button>
                <button class="hf-toggle" data-source="gemini">Gemini</button>
            </div>
        </div>

        <!-- Tag multi-filter -->
        <div class="hf-row">
            <label class="hf-label">Tags</label>
            <div class="hf-tag-filter" id="hf-tag-pills"></div>
            <select id="hf-tag-add" class="hf-tag-add-select">
                <option value="">+ tag</option>
            </select>
        </div>
        <div class="hf-row" id="hf-tag-logic-row" style="display:none">
            <label class="hf-label">Tag logic</label>
            <div class="hf-toggle-group">
                <button class="hf-toggle active" data-logic="AND">AND</button>
                <button class="hf-toggle" data-logic="OR">OR</button>
            </div>
        </div>

        <!-- Date range -->
        <div class="hf-row">
            <label class="hf-label">From</label>
            <input type="date" id="hf-date-from" class="hf-date">
        </div>
        <div class="hf-row">
            <label class="hf-label">To</label>
            <input type="date" id="hf-date-to" class="hf-date">
        </div>

        <!-- Duration range (in minutes for usability) -->
        <div class="hf-row">
            <label class="hf-label">Duration</label>
            <input type="number" id="hf-dur-min" class="hf-dur-input"
                   placeholder="min" min="0" step="1">
            <span class="hf-dur-sep">&ndash;</span>
            <input type="number" id="hf-dur-max" class="hf-dur-input"
                   placeholder="max" min="0" step="1">
            <span class="hf-dur-unit">min</span>
        </div>

        <button class="btn btn-small hf-clear-btn">Clear filters</button>
    </div>
</div>
```

Remove the old `<select id="tag-filter">` and `<select id="source-filter">` elements — they are replaced by the new panel.

Note: All click/change handlers are attached via `addEventListener` in `app.js` (see Section 4.4), **not** via inline `onclick` attributes. This keeps the HTML clean and avoids polluting `window.*`.

### 4.4 `src/corral/static/app.js` — rewire filter logic

At the top of the file, add the import:

```javascript
import { filterState, deserializeFromUrl, serializeToUrl,
         hasActiveFilters, countActiveFilters, resetFilters }
    from './search_filters.js';
```

Remove the old state block at lines 85–89:

```javascript
// REMOVE these four lines:
let historyPage = 1;
let historySearch = '';
let historyTagId = null;
let historySourceType = null;
```

Replace with:

```javascript
let historyPage = 1;  // page number only; all other filter state lives in filterState
```

Replace `loadHistoryFiltered()` with:

```javascript
function loadHistoryFiltered() {
    serializeToUrl(historyPage);
    loadHistorySessionsPaged(historyPage, 50);
    updateFilterBadge();
}
```

Replace `loadHistoryPage()` with (unchanged except the call):

```javascript
function loadHistoryPage(page) {
    historyPage = page;
    loadHistoryFiltered();
}
```

Replace `populateTagFilter()` with `populateHfTagSelect()`. Use the tag data already fetched by `loadAllTags()` rather than making a redundant `/api/tags` call:

```javascript
async function populateHfTagSelect() {
    const tags = await loadAllTags();  // returns cached array
    const sel = document.getElementById('hf-tag-add');
    if (!sel || !tags) return;
    sel.innerHTML = '<option value="">+ tag</option>';
    for (const tag of tags) {
        const opt = document.createElement('option');
        opt.value = tag.id;
        opt.textContent = tag.name;
        sel.appendChild(opt);
    }
}
```

Remove the old tag/source `change` event listener blocks (lines 141–159).

Replace the search input listener (lines 127–139) and add all new filter handlers using `addEventListener` instead of `window.*`:

```javascript
// ── Filter event wiring (inside DOMContentLoaded) ─────────────────────

// Search bar with debounce
const searchInput = document.getElementById('history-search');
if (searchInput) {
    let debounceTimer;
    searchInput.addEventListener('input', () => {
        clearTimeout(debounceTimer);
        debounceTimer = setTimeout(() => {
            filterState.q = searchInput.value.trim();
            historyPage = 1;
            const ftsModeRow = document.getElementById('hf-fts-mode-row');
            if (ftsModeRow)
                ftsModeRow.style.display = filterState.q ? '' : 'none';
            loadHistoryFiltered();
        }, 300);
    });
}

// Advanced panel toggle
const advToggle = document.getElementById('hf-adv-toggle');
if (advToggle) {
    advToggle.addEventListener('click', () => {
        const panel = document.getElementById('hf-advanced');
        if (panel) panel.style.display = panel.style.display === 'none' ? '' : 'none';
    });
}

// FTS mode buttons
document.querySelectorAll('[data-mode]').forEach(btn => {
    btn.addEventListener('click', () => {
        filterState.ftsMode = btn.dataset.mode;
        document.querySelectorAll('[data-mode]')
            .forEach(b => b.classList.toggle('active', b.dataset.mode === filterState.ftsMode));
        historyPage = 1;
        loadHistoryFiltered();
    });
});

// Source toggle buttons
document.querySelectorAll('[data-source]').forEach(btn => {
    btn.addEventListener('click', () => {
        const source = btn.dataset.source;
        if (source === 'all') {
            filterState.sourceTypes = [];
        } else {
            const idx = filterState.sourceTypes.indexOf(source);
            if (idx >= 0) filterState.sourceTypes.splice(idx, 1);
            else filterState.sourceTypes.push(source);
        }
        document.querySelectorAll('[data-source]').forEach(b => {
            if (b.dataset.source === 'all')
                b.classList.toggle('active', filterState.sourceTypes.length === 0);
            else
                b.classList.toggle('active', filterState.sourceTypes.includes(b.dataset.source));
        });
        historyPage = 1;
        loadHistoryFiltered();
    });
});

// Tag add select
const tagAddSel = document.getElementById('hf-tag-add');
if (tagAddSel) {
    tagAddSel.addEventListener('change', () => {
        const id = parseInt(tagAddSel.value);
        if (!id || filterState.tagIds.includes(id)) return;
        filterState.tagIds.push(id);
        tagAddSel.value = '';
        renderFilterTagPills();
        const logicRow = document.getElementById('hf-tag-logic-row');
        if (logicRow) logicRow.style.display = filterState.tagIds.length > 1 ? '' : 'none';
        historyPage = 1;
        loadHistoryFiltered();
    });
}

// Tag logic buttons
document.querySelectorAll('[data-logic]').forEach(btn => {
    btn.addEventListener('click', () => {
        filterState.tagLogic = btn.dataset.logic;
        document.querySelectorAll('[data-logic]')
            .forEach(b => b.classList.toggle('active', b.dataset.logic === filterState.tagLogic));
        historyPage = 1;
        loadHistoryFiltered();
    });
});

// Date filters
const dateFrom = document.getElementById('hf-date-from');
const dateTo = document.getElementById('hf-date-to');
if (dateFrom) dateFrom.addEventListener('change', () => {
    filterState.dateFrom = dateFrom.value || '';
    historyPage = 1;
    loadHistoryFiltered();
});
if (dateTo) dateTo.addEventListener('change', () => {
    filterState.dateTo = dateTo.value || '';
    historyPage = 1;
    loadHistoryFiltered();
});

// Duration filters
const durMin = document.getElementById('hf-dur-min');
const durMax = document.getElementById('hf-dur-max');
if (durMin) durMin.addEventListener('change', () => {
    const val = parseFloat(durMin.value);
    filterState.minDurationSec = isNaN(val) ? null : Math.round(val * 60);
    historyPage = 1;
    loadHistoryFiltered();
});
if (durMax) durMax.addEventListener('change', () => {
    const val = parseFloat(durMax.value);
    filterState.maxDurationSec = isNaN(val) ? null : Math.round(val * 60);
    historyPage = 1;
    loadHistoryFiltered();
});

// Clear all filters button
const clearBtn = document.querySelector('.hf-clear-btn');
if (clearBtn) {
    clearBtn.addEventListener('click', () => {
        resetFilters();
        if (searchInput) searchInput.value = '';
        if (dateFrom) dateFrom.value = '';
        if (dateTo) dateTo.value = '';
        if (durMin) durMin.value = '';
        if (durMax) durMax.value = '';
        document.querySelectorAll('[data-source]')
            .forEach(b => b.classList.toggle('active', b.dataset.source === 'all'));
        document.querySelectorAll('[data-logic]')
            .forEach(b => b.classList.toggle('active', b.dataset.logic === 'AND'));
        document.querySelectorAll('[data-mode]')
            .forEach(b => b.classList.toggle('active', b.dataset.mode === 'phrase'));
        renderFilterTagPills();
        historyPage = 1;
        loadHistoryFiltered();
    });
}
```

Add two local helpers (not exported to `window`):

```javascript
function renderFilterTagPills() {
    const container = document.getElementById('hf-tag-pills');
    if (!container) return;
    // Get tag names from the hf-tag-add select options
    const sel = document.getElementById('hf-tag-add');
    const tagMap = {};
    if (sel) {
        for (const opt of sel.options) {
            if (opt.value) tagMap[parseInt(opt.value)] = opt.textContent;
        }
    }
    container.innerHTML = filterState.tagIds.map(id => {
        const name = tagMap[id] || `Tag ${id}`;
        return `<span class="hf-tag-pill" data-tag-id="${id}">
            ${escapeHtml(name)}
            <span class="hf-tag-remove">&times;</span>
        </span>`;
    }).join('');

    // Attach remove handlers via event delegation
    container.querySelectorAll('.hf-tag-remove').forEach(btn => {
        btn.addEventListener('click', (e) => {
            const tagId = parseInt(e.target.closest('[data-tag-id]').dataset.tagId);
            filterState.tagIds = filterState.tagIds.filter(id => id !== tagId);
            renderFilterTagPills();
            const logicRow = document.getElementById('hf-tag-logic-row');
            if (logicRow) logicRow.style.display = filterState.tagIds.length > 1 ? '' : 'none';
            historyPage = 1;
            loadHistoryFiltered();
        });
    });
}

function updateFilterBadge() {
    const badge = document.getElementById('hf-active-count');
    if (!badge) return;
    const n = countActiveFilters();
    badge.textContent = String(n);
    badge.style.display = n > 0 ? '' : 'none';
}
```

Update the `DOMContentLoaded` handler to restore filter state from URL before the initial load, and sync DOM inputs:

```javascript
document.addEventListener("DOMContentLoaded", () => {
    loadSettings();

    // Restore filter state from URL query params before first load
    const restored = deserializeFromUrl();
    historyPage = restored.page;

    loadLiveSessions();
    connectCorralWs();
    populateHfTagSelect().then(() => {
        // Sync DOM to restored state, then load
        syncFilterDomToState();
        loadHistoryFiltered();
    });

    // ... rest of existing initialization (initNotesMd, keyboard handlers, etc.) ...
});

function syncFilterDomToState() {
    const searchInput = document.getElementById('history-search');
    if (searchInput) searchInput.value = filterState.q;

    const dateFrom = document.getElementById('hf-date-from');
    if (dateFrom) dateFrom.value = filterState.dateFrom;
    const dateTo = document.getElementById('hf-date-to');
    if (dateTo) dateTo.value = filterState.dateTo;

    const durMin = document.getElementById('hf-dur-min');
    if (durMin && filterState.minDurationSec != null)
        durMin.value = String(Math.round(filterState.minDurationSec / 60));
    const durMax = document.getElementById('hf-dur-max');
    if (durMax && filterState.maxDurationSec != null)
        durMax.value = String(Math.round(filterState.maxDurationSec / 60));

    document.querySelectorAll('[data-source]').forEach(b => {
        if (b.dataset.source === 'all')
            b.classList.toggle('active', filterState.sourceTypes.length === 0);
        else
            b.classList.toggle('active', filterState.sourceTypes.includes(b.dataset.source));
    });
    document.querySelectorAll('[data-logic]').forEach(b =>
        b.classList.toggle('active', b.dataset.logic === filterState.tagLogic));
    document.querySelectorAll('[data-mode]').forEach(b =>
        b.classList.toggle('active', b.dataset.mode === filterState.ftsMode));

    // Auto-open advanced panel if any non-text filters are active
    if (hasActiveFilters()) {
        const panel = document.getElementById('hf-advanced');
        if (panel) panel.style.display = '';
    }

    // Show FTS mode row if there is a query
    const ftsModeRow = document.getElementById('hf-fts-mode-row');
    if (ftsModeRow) ftsModeRow.style.display = filterState.q ? '' : 'none';

    renderFilterTagPills();
    updateFilterBadge();
}
```

Also remove the old `loadHistorySessions()` call from `DOMContentLoaded` (line 123) — it was doing an unconditional non-paginated fetch; now `populateHfTagSelect().then(...)` handles the first load.

### 4.5 `src/corral/static/render.js` — display duration

Add the helper function after the existing `formatShortTime()` function (after line 14):

```javascript
function formatDuration(sec) {
    if (sec == null || sec <= 0) return '';
    if (sec < 60) return `${sec}s`;
    if (sec < 3600) return `${Math.round(sec / 60)}m`;
    const h = Math.floor(sec / 3600);
    const m = Math.round((sec % 3600) / 60);
    return m > 0 ? `${h}h ${m}m` : `${h}h`;
}
```

Inside `renderHistorySessions()`, update the session item template in the `.map()` callback (around line 138):

```javascript
const durStr = s.duration_sec != null ? formatDuration(s.duration_sec) : '';
const durTag = durStr ? `<span class="session-dur">${escapeHtml(durStr)}</span>` : '';
// Add durTag after timeTag in session-row-top:
// `<div class="session-row-top">${timeTag}${durTag}<span class="session-label" ...>
```

### 4.6 `src/corral/static/style.css` — new rules

Replace the `.history-filter-row` block (lines 193–212) with the new `.hf-*` rules. Keep the `.history-filters` base styles (lines 171–191) — they still apply to the new panel wrapper.

```css
/* Advanced filter panel */
.hf-search-row {
    display: flex;
    gap: 6px;
    align-items: center;
}
.hf-search-row input[type="search"] {
    flex: 1;
    min-width: 0;
}
.hf-adv-btn {
    flex-shrink: 0;
    position: relative;
    padding: 5px 8px;
}
.hf-active-badge {
    position: absolute;
    top: -4px;
    right: -4px;
    background: var(--accent);
    color: var(--bg-primary);
    border-radius: 10px;
    font-size: 9px;
    font-weight: 700;
    min-width: 14px;
    height: 14px;
    line-height: 14px;
    text-align: center;
    padding: 0 2px;
}
.hf-advanced {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px 0 4px;
    border-top: 1px solid var(--border);
    margin-top: 2px;
}
.hf-row {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    min-height: 22px;
}
.hf-label {
    font-size: 11px;
    color: var(--text-muted);
    min-width: 44px;
    flex-shrink: 0;
}
.hf-toggle-group {
    display: flex;
    gap: 3px;
    flex-wrap: wrap;
}
.hf-toggle {
    padding: 2px 8px;
    font-size: 11px;
    border: 1px solid var(--border);
    border-radius: 3px;
    background: var(--bg-tertiary);
    color: var(--text-secondary);
    cursor: pointer;
    transition: background 0.1s, border-color 0.1s;
}
.hf-toggle.active {
    background: var(--accent-dim);
    border-color: var(--accent);
    color: #fff;
}
.hf-toggle:hover:not(.active) {
    background: var(--bg-hover);
}
.hf-tag-filter {
    display: flex;
    flex-wrap: wrap;
    gap: 3px;
}
.hf-tag-pill {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 1px 7px;
    border-radius: 10px;
    font-size: 11px;
    background: var(--accent-dim);
    color: #fff;
}
.hf-tag-remove {
    cursor: pointer;
    opacity: 0.75;
    font-size: 13px;
    line-height: 1;
}
.hf-tag-remove:hover { opacity: 1; }
.hf-tag-add-select {
    max-width: 90px;
    padding: 2px 4px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text-secondary);
    font-size: 11px;
    outline: none;
    cursor: pointer;
}
.hf-tag-add-select:focus { border-color: var(--accent); }
.hf-date {
    flex: 1;
    min-width: 0;
    padding: 3px 6px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text-primary);
    font-size: 11px;
    outline: none;
}
.hf-date:focus { border-color: var(--accent); }
.hf-dur-input {
    width: 50px;
    padding: 3px 5px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text-primary);
    font-size: 11px;
    outline: none;
}
.hf-dur-input:focus { border-color: var(--accent); }
.hf-dur-sep,
.hf-dur-unit {
    font-size: 11px;
    color: var(--text-muted);
}
.hf-clear-btn {
    align-self: flex-start;
    font-size: 11px;
    color: var(--text-muted);
    margin-top: 2px;
}
/* Duration badge in sidebar items */
.session-dur {
    font-size: 10px;
    color: var(--text-muted);
    margin-left: 4px;
}
```

---

## 5. FTS5 Query Language

FTS5 with `tokenize='porter'` (already in production) provides:

| Feature | Syntax example | Effect |
|---|---|---|
| Phrase match | `"auth flow"` | Exact phrase; porter stems not applied inside quotes |
| All words | `auth AND migration` | Both stems present anywhere in body |
| Any word | `auth OR migration` | Either stem present |
| Exclusion | `auth NOT migration` | auth present, migration absent |
| Column prefix | `body:auth` | Search within specific FTS column |

The dashboard exposes only phrase/and/or via the mode toggle. NOT is intentionally not exposed in the UI (it produces confusing empty-result behavior for novice users) but the `_sanitize_fts_query` function is designed to allow it to be added later by extending `mode` to `"not"`.

Porter stemming means the user never needs to think about word forms — `running` finds `run`, `runs`, `ran`.

---

## 6. Filter Composition

All filter dimensions are combined with SQL AND. Within the tag filter, the user chooses AND or OR:

| Filter dimension | Composed with others via | Internal logic |
|---|---|---|
| FTS text (`q`) | AND | Phrase / all-words / any-word (mode toggle) |
| Tag filter (`tag_ids`) | AND | AND (default) or OR (toggle) |
| Source type (`source_types`) | AND | OR within (multi-select means "any of these sources") |
| Date from/to | AND | Both bounds applied as AND |
| Duration min/max | AND | Both bounds applied as AND |

Example composition in SQL pseudo-form:

```
WHERE session_fts MATCH "auth AND migration"
  AND last_timestamp >= '2025-01-01T00:00:00'
  AND last_timestamp <= '2025-03-01T23:59:59'
  AND session_id IN (SELECT ... WHERE tag_id = 3)
  AND session_id IN (SELECT ... WHERE tag_id = 7)
  AND source_type IN ('claude')
  AND (julianday(last_ts) - julianday(first_ts)) * 86400 >= 300
```

---

## 7. URL State Persistence

Filter state is serialized to the **query string** of the page URL, leaving the existing `#session/<id>` hash intact. `window.history.replaceState` is used (not `pushState`) to update the URL without creating browser history entries on every filter change.

The `page` number is included in serialization and restored by `deserializeFromUrl()`, so multi-page result sets survive page reloads and URL sharing.

Example URL with all filters active:

```
http://localhost:8420/?page=2&q=auth&fts_mode=and&tag_ids=3%2C7&tag_logic=AND
    &source_types=claude&date_from=2025-01-01&date_to=2025-03-01
    &min_duration_sec=300&max_duration_sec=3600
    #session/abc-123-def
```

On page load the sequence is:

1. `deserializeFromUrl()` — populates `filterState` and restores `historyPage` from `window.location.search`
2. `populateHfTagSelect()` — loads tag list for the tag pills (uses cached `loadAllTags()` data)
3. `syncFilterDomToState()` — writes restored state back to all DOM inputs and toggle buttons
4. `loadHistoryFiltered()` — fires the first API request with the restored params

If the URL has no search params, `deserializeFromUrl()` leaves `filterState` at its defaults (empty/null), which is equivalent to no filters.

---

## 8. Performance Considerations

### Existing indexes that serve the new filters

| Filter | Index used |
|---|---|
| Date `date_to` | `idx_session_index_last_ts` (already on `last_timestamp DESC`) |
| Date `date_from` | New `idx_session_index_first_ts` (added in Phase 1) |
| Tag filter (each subquery) | New `idx_session_tags_tag_id` (added in Phase 1) |
| Source type | No dedicated index — only 2 distinct values; table scan of `session_index` is negligible |
| FTS text | FTS5 internal B-tree (already in place) |

### Interaction between FTS and date filters

When both `q` and `date_from`/`date_to` are active, SQLite evaluates FTS MATCH first (a fast inverted-index lookup), then applies timestamp predicates to the already-narrow result set.

### Duration filter cost

`julianday()` on two nullable columns is computed at scan time, not indexed. For the expected Corral database scale (hundreds to a few thousand sessions), this is negligible. The duration clause fires only after FTS and date filters have already narrowed the candidate set.

### Count query optimization

The paginated endpoint runs `SELECT COUNT(*)` with the same WHERE clause before the data query. Both benefit from the same indexes. No additional optimization is needed at current scale.

---

## 9. Edge Cases

| Scenario | Handling |
|---|---|
| `date_from` after `date_to` | Backend swaps them silently; frontend sets `max` attribute on `hf-date-from` and `min` on `hf-date-to` dynamically in date change handler to prevent bad input |
| Invalid date format in URL | Backend regex `^\d{4}-\d{2}-\d{2}$` discards non-matching values; filter is silently ignored |
| `min_duration_sec` > `max_duration_sec` | Backend swaps them silently |
| Duration filter with NULL `first_timestamp` or `last_timestamp` | `julianday(NULL)` = NULL; arithmetic on NULL = NULL; `NULL >= N` = false; rows excluded. This is correct — duration is unknown. |
| Tag ID in URL no longer exists in `tags` table | The subquery `WHERE tag_id = ?` returns no matching `session_id` values, producing 0 results for that constraint. No error is raised. |
| FTS5 not compiled into SQLite | Existing `try/except` in `upsert_fts()` already handles this. `_sanitize_fts_query` still runs and returns a string; the JOIN + MATCH clause will silently fail and return 0 results. |
| Zero results for all filters active | `renderHistorySessions()` already renders "No history found" when the sessions array is empty. No new handling needed. |
| Very long `tag_ids` list in URL | Backend validates each token with `.isdigit()`; non-numeric tokens silently dropped. No SQL injection risk because each ID is bound as a parameterized value. |
| Refreshing page with shared URL | Works fully — `deserializeFromUrl()` reconstructs all filter state including page, `syncFilterDomToState()` updates the DOM, `loadHistoryFiltered()` reproduces the same API call. |
| `source_types` containing an unknown value (e.g. `"grok"`) | The `IN (...)` clause simply produces no matches for that value. Existing data has only `claude` and `gemini`, so this is a no-op. |
| Invalid `fts_mode` value | Both backend and `_sanitize_fts_query` fall back to `"phrase"` mode. |
| Double-quotes in phrase search | Stripped from input and replaced with spaces before wrapping in FTS5 phrase quotes; prevents syntax errors. |

---

## 10. Implementation Order

### Phase 1 — Backend foundation

- [ ] Add `_sanitize_fts_query(raw, mode)` function to `store/sessions.py` (below `_extract_first_header`)
- [ ] Add `_compute_duration(first_ts, last_ts)` function to `store/sessions.py` (below `_sanitize_fts_query`)
- [ ] Add `idx_session_tags_tag_id` index creation to `_ensure_schema()` in `store/connection.py`
- [ ] Add `idx_session_index_first_ts` index creation to `_ensure_schema()` in `store/connection.py`
- [ ] Extend `list_sessions_paged()` signature with `fts_mode`, `tag_ids`, `tag_logic`, `source_types`, `date_from`, `date_to`, `min_duration_sec`, `max_duration_sec` parameters
- [ ] Add legacy-merge block at top of `list_sessions_paged()` body
- [ ] Add `fts_mode` validation at top of method body
- [ ] Replace `search` MATCH clause to call `_sanitize_fts_query(search, fts_mode)`
- [ ] Add date range WHERE clauses to query builder
- [ ] Add duration WHERE clauses to query builder
- [ ] Replace single-tag subquery with multi-tag AND/OR logic
- [ ] Replace single-source `== ?` clause with multi-source `IN (...)` clause
- [ ] Add `duration_sec` field to each session dict in the result using `_compute_duration`
- [ ] Update `GET /api/sessions/history` in `api/history.py` with new `Query` parameters and normalization block
- [ ] Update the cold-start fallback path to pass resolved params to the retry call

### Phase 2 — `search_filters.js` module

- [ ] Create `src/corral/static/search_filters.js` with `filterState`, `buildApiParams`, `serializeToUrl`, `deserializeFromUrl` (with page restoration), `resetFilters`, `hasActiveFilters`, `countActiveFilters`
- [ ] Verify `pyproject.toml` `[tool.setuptools.package-data]` glob `static/*.js` already covers the new file (it does — line 69)

### Phase 3 — HTML and CSS

- [ ] Replace `.history-filters` block in `templates/includes/sidebar.html` (lines 14–26) with the expanded filter panel HTML
- [ ] Remove old `<select id="tag-filter">` and `<select id="source-filter">` elements (they are inside the replaced block)
- [ ] Use no inline `onclick` attributes — all handlers attached via `addEventListener` in `app.js`
- [ ] Replace `.history-filter-row` CSS block in `style.css` (lines 193–212) with `.hf-*` rules
- [ ] Keep `.history-filters` base styles (lines 171–191) — still used as the panel wrapper
- [ ] Add `.session-dur` CSS rule

### Phase 4 — JavaScript wiring

- [ ] Update `api.js`: import `buildApiParams` from `search_filters.js`; replace `loadHistorySessionsPaged` signature (remove old params)
- [ ] Update `app.js`:
  - [ ] Add import for `search_filters.js` exports
  - [ ] Remove `historySearch`, `historyTagId`, `historySourceType` state variables
  - [ ] Replace `loadHistoryFiltered()` with URL-syncing version
  - [ ] Replace `populateTagFilter()` with `populateHfTagSelect()` (use `loadAllTags()` cache, no redundant fetch)
  - [ ] Remove old tag/source `change` event listeners (lines 141–159)
  - [ ] Update search input debounce to set `filterState.q` and show/hide FTS mode row
  - [ ] Add all filter handlers via `addEventListener` (no `window.*` pollution)
  - [ ] Add `renderFilterTagPills()` helper (uses event delegation for remove buttons)
  - [ ] Add `updateFilterBadge()` helper
  - [ ] Add `syncFilterDomToState()` function
  - [ ] Update `DOMContentLoaded`: call `deserializeFromUrl()` first (restoring `historyPage`), then `populateHfTagSelect().then(...)`, remove direct `loadHistorySessions()` call (line 123)
- [ ] Update `render.js`: add `formatDuration()`, update session item HTML in `renderHistorySessions` to include `.session-dur` span

### Phase 5 — Integration and smoke tests

- [ ] Reinstall: `pip install -e .` in the worktree, restart `corral-web-server` tmux session
- [ ] Hard refresh browser (`cmd+shift+r`); verify no JS console errors, no 404s on new file
- [ ] Apply each filter in isolation; verify sidebar list updates correctly
- [ ] Apply date range — verify sessions outside range disappear
- [ ] Apply tag AND filter with two tags — verify only sessions with both tags appear
- [ ] Apply tag OR filter with two tags — verify sessions with either tag appear
- [ ] Apply duration filter (e.g. min=5 minutes) — verify short sessions excluded
- [ ] Apply text search + date range simultaneously — verify combined filtering
- [ ] Copy URL after applying filters; open in new tab; verify filters restore exactly (including page number)
- [ ] Click "Clear filters" — verify all inputs reset and full session list returns
- [ ] Verify `date_from` > `date_to` (type manually) — verify backend swaps silently, results shown
- [ ] Verify zero-result state displays "No history found" in sidebar

---

## 11. File Summary

| File | Action | Description |
|---|---|---|
| `src/corral/store/sessions.py` | Modify | `_sanitize_fts_query`, `_compute_duration`, extended `list_sessions_paged`, `duration_sec` in response |
| `src/corral/store/connection.py` | Modify | 2 new indexes in `_ensure_schema()` |
| `src/corral/api/history.py` | Modify | Extended `GET /api/sessions/history` with 8 new Query params + normalization |
| `src/corral/static/search_filters.js` | **Create** | All filter state, URL serialization/deserialization (with page), `buildApiParams` |
| `src/corral/static/api.js` | Modify | `loadHistorySessionsPaged` delegates to `buildApiParams` |
| `src/corral/static/app.js` | Modify | New filter handlers via addEventListener, DOM sync on load, `populateHfTagSelect` |
| `src/corral/static/render.js` | Modify | `formatDuration`, duration span in sidebar items |
| `src/corral/static/style.css` | Modify | Replace `.history-filter-row` with `.hf-*` filter panel styles, add `.session-dur` |
| `src/corral/templates/includes/sidebar.html` | Modify | Replace filter section with expanded advanced panel |

---

## 12. Changes From Previous Plan Version

This plan was updated to reflect the codebase refactors since the original was written (commit `2d4256c`). Key differences:

| Change | Old | New |
|---|---|---|
| Session store location | `src/corral/session_store.py` | `src/corral/store/sessions.py` |
| Schema management | In `session_store.py` | `src/corral/store/connection.py` |
| History API endpoint | `src/corral/web_server.py` line 236 | `src/corral/api/history.py` line 27 |
| Filter HTML location | `src/corral/templates/index.html` lines 32–44 | `src/corral/templates/includes/sidebar.html` lines 14–26 |
| FTS phrase quote handling | Double-quote escaping (`""`) | Strip internal quotes (FTS5 has no quote escape) |
| `fts_mode` validation | Missing | Added in both backend and `_sanitize_fts_query` |
| Click handlers | Inline `onclick` + `window.*` exports | `addEventListener` in module scope |
| Tag data fetching | Redundant `/api/tags` call in `populateHfTagSelect` | Uses `loadAllTags()` cache |
| Page in URL | Not deserialized | `deserializeFromUrl()` restores page number |
| Duration input | No `step` attribute | `step="1"` to prevent fractional minutes |

---

*Updated against codebase state as of branch `features/search`, 2026-03-09.*
