# Custom Views — User-Generated Agentic Sidebar Tabs

## Concept

Users describe a view in natural language, and Coral generates an HTML/JS widget that renders as a new tab in the agentic sidebar. Views use Coral's REST APIs to fetch and display data.

## User Flow

```
1. User clicks '+' on agentic sidebar tabs
2. Prompt input appears: "Describe the view you want..."
3. User types: "Show token usage per agent as a bar chart"
4. Coral sends the prompt + API reference to an LLM
5. LLM generates self-contained HTML/JS/CSS
6. View renders in a new sidebar tab
7. User can edit, rename, delete, or share the view
```

## Architecture

### View Format

Each custom view is a self-contained HTML document:

```html
<!-- Coral Custom View: Token Usage Chart -->
<div id="root"></div>
<script>
  // Context provided by Coral
  const CORAL_API = '';  // base URL (same origin)
  const CORAL_SESSION = { name: '...', session_id: '...', agent_type: '...' };

  // Fetch data from Coral API
  async function load() {
    const resp = await fetch(`${CORAL_API}/api/sessions/live`);
    const sessions = await resp.json();
    // ... render chart
  }

  load();
  setInterval(load, 10000); // auto-refresh
</script>
```

### Rendering — iframe Sandbox

Views render in a sandboxed iframe within the sidebar tab:

```html
<iframe sandbox="allow-scripts allow-same-origin"
        srcdoc="<user-generated HTML>"
        style="width:100%; height:100%; border:none;">
</iframe>
```

**Why iframe:**
- Isolation: user JS can't access or modify Coral's DOM
- Security: sandbox attribute limits capabilities
- Simplicity: self-contained document, no build step
- Same-origin: API calls work via fetch() since iframe is same-origin with srcdoc

**Why not shadow DOM:** Less isolation, user JS could still access parent document.

### Storage

```sql
CREATE TABLE custom_views (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    prompt TEXT,              -- the natural language prompt that generated it
    html TEXT NOT NULL,       -- the generated HTML/JS/CSS
    tab_order INTEGER,        -- position in sidebar tabs
    scope TEXT DEFAULT 'global', -- 'global' or 'session'
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### API Endpoints

```
GET    /api/views              — list all custom views
POST   /api/views              — create a new view { name, prompt, html }
GET    /api/views/{id}         — get a view
PUT    /api/views/{id}         — update a view
DELETE /api/views/{id}         — delete a view
POST   /api/views/generate     — generate HTML from a prompt (calls LLM)
```

### API Reference for LLM

The `/api/views/generate` endpoint sends the user's prompt along with a concise API reference to the LLM. The reference should include:

**Session APIs:**
- `GET /api/sessions/live` — list all live agent sessions
- `GET /api/sessions/live/{name}/poll` — get session status, output, events
- `GET /api/sessions/live/{name}/files` — changed files list
- `GET /api/sessions/live/{name}/diff?filepath=` — file diff
- `GET /api/sessions/live/{name}/file-content?filepath=` — file content

**Task APIs:**
- `GET /api/sessions/live/{name}/tasks` — agent tasks
- `GET /api/sessions/live/{name}/events` — agent tool-use events

**Board APIs:**
- `GET /api/board/{project}/messages/all` — board messages
- `GET /api/board/{project}/subscribers` — board subscribers

**System APIs:**
- `GET /api/settings` — user settings
- `GET /api/system/cli-check?type=` — CLI availability

**Git APIs:**
- `GET /api/sessions/live/{name}/git-state` — branch, commit info

### LLM Prompt Template

```
You are generating a self-contained HTML/JS view for the Coral dashboard sidebar.

The view should:
- Be a single HTML document (no external dependencies except CDN libs)
- Render in a ~300px wide sidebar panel
- Use dark theme (background: #0d1117, text: #e6edf3)
- Fetch data from these APIs (same origin, no auth needed):
  {API_REFERENCE}
- Auto-refresh every 10 seconds
- Be responsive to the panel width
- Handle loading and error states gracefully

User's request: {USER_PROMPT}

Current context:
- Active session: {SESSION_NAME} ({AGENT_TYPE})
- Board: {BOARD_NAME}

Generate only the HTML. No explanation.
```

## Frontend Integration

### Dynamic Tabs

The agentic sidebar tabs (Files, Tasks, Stats, Chat, Notes) gain a '+' button:

```html
<button class="agentic-tab" onclick="showCreateViewModal()" title="Add custom view">
    <span class="material-icons">add</span>
</button>
```

Each custom view gets its own tab with the view name and a close/edit button on hover.

### View Panel

```html
<div id="agentic-panel-custom-{id}" class="agentic-panel">
    <iframe sandbox="allow-scripts allow-same-origin"
            srcdoc="..."
            style="width:100%; height:100%; border:none;">
    </iframe>
</div>
```

## Implementation Plan

### Phase 1: Manual views (no LLM)
1. Add custom_views table and CRUD API
2. Add '+' tab button and create/edit modal with HTML editor
3. Render views in iframe tabs
4. Users write their own HTML/JS manually

### Phase 2: LLM-generated views
1. Document all APIs as a structured reference
2. Add /api/views/generate endpoint that calls an LLM
3. Create view modal accepts natural language prompt
4. Generated code shown in editor for review before saving

### Phase 3: Sharing and templates
1. Export/import views as JSON
2. Community view template gallery
3. Pre-built views shipped with Coral (token usage, commit timeline, etc.)

## Security Assessment

### Rendering Isolation — iframe with Strict Sandbox

All custom views render in a sandboxed iframe:
```html
<iframe sandbox="allow-scripts allow-same-origin" ...>
```

This provides:
- Full JS isolation — user code cannot access parent DOM, cookies, localStorage, or Coral's internal state
- CSS isolation — user styles cannot break the sidebar layout
- Clean lifecycle — destroying the iframe fully cleans up (no orphaned event listeners)
- Same-origin API access — views can call Coral's REST APIs via fetch()

**Why not Shadow DOM:** Shadow DOM isolates CSS but NOT JavaScript. User-generated JS in a shadow DOM has full access to the window object and Coral's app state. Not acceptable for untrusted code.

**Why not direct injection:** Zero isolation. Only viable for built-in trusted views.

### Risk Analysis

| Risk | Severity | Mitigation |
|------|----------|------------|
| **Destructive API calls** | Medium | Views can call PUT/DELETE endpoints. Add read-only API proxy or `X-Coral-View` header check to restrict write ops from view contexts |
| **XSS via generated code** | Low | iframe sandbox isolates from parent. Generated JS cannot access parent DOM, cookies, or localStorage |
| **Prompt injection in LLM generation** | Medium | CSP: `connect-src 'self'` blocks external fetch. `script-src 'unsafe-inline'` blocks external scripts. Rate limit API calls. Kill button for runaway views |
| **Stored XSS via view persistence** | Low | Views are per-user. If sharing is added later, require explicit confirmation before loading another user's view |
| **Data exposure** | Low | Views can read all session data, messages, files, tasks via API. This is intentional — no per-view permission scoping needed for single-user desktop app |
| **Resource exhaustion** | Low | Badly-written view could infinite-loop or hammer the API. Rate limiter (per-session token bucket) + CPU kill button mitigate this |

### Content Security Policy

Set strict CSP on the iframe:
```
script-src 'unsafe-inline';     -- needed for generated inline JS
connect-src 'self';             -- only same-origin API calls (no data exfiltration)
img-src 'self' data: https:;    -- allow images from API and CDN charts
style-src 'unsafe-inline';      -- needed for generated inline CSS
default-src 'none';             -- block everything else
```

This prevents:
- Loading external scripts (malware, crypto-mining)
- Exfiltrating data to external URLs
- Loading external iframes or frames

### Recommended: Read-Only API Surface for Views

To eliminate the destructive API call risk, consider a dedicated view API namespace:

```
GET /api/view-data/sessions         — read-only session list
GET /api/view-data/tasks/{name}     — read-only tasks
GET /api/view-data/events/{name}    — read-only events
GET /api/view-data/board/{project}  — read-only board messages
GET /api/view-data/git/{name}       — read-only git state
```

These endpoints are thin wrappers around existing routes but only expose GET operations. Views use these instead of the full API. The main API remains available for the Coral UI itself.

### Team Consensus

- **Go Expert**: iframe is the correct isolation boundary. Add rate limiting and strict CSP.
- **QA Engineer**: Main risk is unintended destructive API calls. Read-only API surface eliminates this.
- **Frontend Dev**: iframe proven from earlier preview pane work. Dynamic tabs straightforward.
- **Lead Developer**: Curated ~20 endpoint API spec (not full OpenAPI) is better for LLM context.
