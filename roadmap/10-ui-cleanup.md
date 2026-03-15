# UI Cleanup & UX Improvements

## 1. Goal

Clean up the Coral dashboard to reduce visual clutter, improve information hierarchy,
and make the interface more intuitive for operators managing multiple agents. The
current UI is feature-complete but has accumulated density — empty sections waste
space, action buttons compete for attention, and panels feel cramped at default widths.

Value delivered:
- **Reduced cognitive load:** Operators see what matters (active sessions, current
  status) without wading through empty sections and rarely-used buttons.
- **Better use of screen real estate:** Collapsible sections and icon-based tabs
  reclaim pixels for the content that matters (agent output, activity).
- **Stronger visual hierarchy:** Typography and spacing improvements make scanning
  faster — session names, status, and actions are immediately distinguishable.
- **More polished feel:** Micro-interactions, refined colors, and a distinctive
  typeface elevate the tool from "functional internal dashboard" to "product."

---

## 2. Current State Assessment

| Area | Issue | Severity |
|---|---|---|
| Sidebar | 4 stacked sections; empty states ("No active jobs") waste vertical space | High |
| Sidebar | History section shares scroll with Live Sessions, fights for room | Medium |
| Session header | 4 action buttons (Info, Attach, Restart, Kill) + branch + badge + status all in one row | High |
| Right panel tabs | 5 text labels (Activity, Files, Tasks, Notes, History) cramped in 280px | Medium |
| Command pane toolbar | Mode buttons + macros + nav keys + Send — visual noise | Medium |
| Typography | System font stack (`-apple-system`) — generic, forgettable | Low |
| Color contrast | `--bg-primary` / `--bg-secondary` / `--bg-tertiary` nearly indistinguishable | Medium |
| Welcome screen | Plain centered text, no quick actions | Low |
| Accessibility | Icon buttons lack `aria-label`, color-only status indicators | Medium |
| CSS file | Single 3,779-line monolith | Low |

---

## 3. Implementation Phases

### Phase 1: Sidebar Cleanup (High Impact, Low Risk)

**Goal:** Reclaim vertical space in the sidebar by hiding empty sections and making
all sections collapsible.

#### 3.1 Collapsible Sidebar Sections

Each `<section class="sidebar-section">` header becomes a click target that
toggles the section body. Collapsed state persists in `localStorage`.

**Files to modify:**
- `src/coral/templates/includes/sidebar.html` — Add collapse toggle markup
- `src/coral/static/sidebar.js` — Add collapse/expand logic with localStorage persistence
- `src/coral/static/style.css` — Add `.sidebar-section.collapsed` styles with rotation animation on chevron

**Markup pattern:**
```html
<section class="sidebar-section" data-section="live-sessions">
    <div class="sidebar-section-header" data-collapse-toggle>
        <div class="sidebar-section-title">
            <svg class="collapse-chevron" ...><!-- rotates 90deg when open --></svg>
            <h2>Live Sessions</h2>
            <span class="section-count-badge">2</span>
        </div>
        <button class="btn btn-small btn-primary sidebar-new-btn" onclick="showLaunchModal()">+ New</button>
    </div>
    <div class="sidebar-section-body">
        <ul id="live-sessions-list" class="session-list">...</ul>
    </div>
</section>
```

**CSS additions:**
```css
.sidebar-section.collapsed .sidebar-section-body {
    display: none;
}

.collapse-chevron {
    transition: transform 0.2s ease;
    width: 12px;
    height: 12px;
    flex-shrink: 0;
}

.sidebar-section.collapsed .collapse-chevron {
    transform: rotate(-90deg);
}

.section-count-badge {
    font-size: 10px;
    font-weight: 600;
    color: var(--text-muted);
    background: var(--bg-tertiary);
    border-radius: 8px;
    padding: 0 6px;
    line-height: 16px;
    min-width: 16px;
    text-align: center;
}
```

#### 3.2 Auto-hide Empty Sections

When a section has zero items, collapse it automatically and show the count as `0`
in the badge. The section header remains visible but the body is hidden. When items
appear (via WebSocket update), auto-expand.

**Logic (in `render.js` or `sidebar.js`):**
```javascript
function updateSectionVisibility(sectionId, itemCount) {
    const section = document.querySelector(`[data-section="${sectionId}"]`);
    const badge = section.querySelector('.section-count-badge');
    badge.textContent = itemCount;

    if (itemCount === 0 && !section.dataset.manualExpand) {
        section.classList.add('collapsed');
    }
}
```

#### 3.3 Merge Jobs + Scheduled into Single Section

Combine "Jobs" (active runs) and "Scheduled" (job definitions) into one
**"Jobs"** section with an inline toggle:

```
Jobs                           [+ Job]
  [Active (1)] [Scheduled (3)]
  ─────────────────────────────
  <list items>
```

**Files to modify:**
- `src/coral/templates/includes/sidebar.html` — Merge two `<section>` blocks
- `src/coral/static/style.css` — Add `.sidebar-subtab` toggle styles
- `src/coral/static/sidebar.js` — Add subtab switching logic
- `src/coral/static/live_jobs.js` — Update render target
- `src/coral/static/scheduler.js` — Update render target

---

### Phase 2: Session Header Declutter (High Impact, Medium Risk)

**Goal:** Reduce the session header from two dense rows to a clean, scannable bar.

#### 3.4 Overflow Menu for Secondary Actions

Move `Info` and `Kill` into a three-dot overflow menu. Keep `Attach` (primary
action) and `Restart` (common action) as visible buttons.

**New markup:**
```html
<div class="session-title-actions">
    <button class="btn btn-icon-label btn-primary" onclick="attachTerminal()">
        <svg>...</svg><span>Attach</span>
    </button>
    <button class="btn btn-icon-label btn-warning" onclick="restartSession()">
        <svg>...</svg><span>Restart</span>
    </button>
    <div class="overflow-menu-wrapper">
        <button class="btn btn-icon overflow-menu-trigger" title="More actions">
            <svg><!-- three dots --></svg>
        </button>
        <div class="overflow-menu" style="display:none">
            <button class="overflow-menu-item" onclick="showInfoModal()">
                <svg>...</svg> Session Info
            </button>
            <hr class="overflow-menu-divider">
            <button class="overflow-menu-item overflow-menu-danger" onclick="killSession()">
                <svg>...</svg> Kill Session
            </button>
        </div>
    </div>
</div>
```

**CSS additions:**
```css
.overflow-menu-wrapper {
    position: relative;
}

.overflow-menu {
    position: absolute;
    top: 100%;
    right: 0;
    margin-top: 4px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-light);
    border-radius: 8px;
    padding: 4px;
    min-width: 180px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
    z-index: 50;
}

.overflow-menu-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 8px 12px;
    background: none;
    border: none;
    color: var(--text-secondary);
    font-size: 13px;
    cursor: pointer;
    border-radius: 4px;
    transition: background 0.1s;
}

.overflow-menu-item:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
}

.overflow-menu-item.overflow-menu-danger:hover {
    background: rgba(248, 81, 73, 0.15);
    color: var(--error);
}

.overflow-menu-divider {
    border: none;
    border-top: 1px solid var(--border);
    margin: 4px 0;
}
```

#### 3.5 Compact Status/Goal Line

Merge the two-line Goal + Status into a single line with a separator:

```
Status: Writing tests  ·  Goal: Implementing user auth end-to-end
```

When no goal is set, show only the status line (no empty "Goal:" label).

**Files to modify:**
- `src/coral/templates/includes/views/live_session.html` — Merge `.summary-line` and `.status-line`
- `src/coral/static/style.css` — Single-line `.session-meta-compact` style
- `src/coral/static/render.js` / `websocket.js` — Update status/summary rendering

#### 3.6 Branch as Chip

Convert the inline branch text to a styled chip with an icon:

```css
.branch-chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 10px;
    background: rgba(88, 166, 255, 0.1);
    border: 1px solid rgba(88, 166, 255, 0.2);
    border-radius: 12px;
    font-size: 12px;
    color: var(--accent);
    font-family: 'SF Mono', 'Fira Code', monospace;
}
```

---

### Phase 3: Right Panel Tab Refinement (Medium Impact, Low Risk)

**Goal:** Make the 5-tab agentic state panel usable at its default 280px width.

#### 3.7 Icon Tabs with Tooltips

Replace text labels with Material Icons + tooltip. Show count badges inline.

| Current Text | Icon | Material Icon Name |
|---|---|---|
| Activity | timeline | `timeline` |
| Files | description | `description` |
| Tasks | checklist | `checklist` |
| Notes | edit_note | `edit_note` |
| History | history | `history` |

**New markup pattern:**
```html
<button class="agentic-tab active" onclick="switchAgenticTab('activity')"
        id="agentic-tab-activity" title="Activity">
    <span class="material-icons">timeline</span>
    <span class="agentic-tab-count" id="activity-bar-count"></span>
</button>
```

**CSS changes:**
```css
.agentic-tab {
    flex: 1;
    padding: 8px 4px;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 4px;
}

.agentic-tab .material-icons {
    font-size: 18px;
}

.agentic-tab-count:empty {
    display: none;
}
```

#### 3.8 Collapsible Right Panel

Add a toggle button at the top-left of the agentic state panel to collapse it
entirely, giving full width to the main output.

**Files to modify:**
- `src/coral/templates/includes/views/live_session.html` — Add collapse toggle
- `src/coral/static/sidebar.js` — Reuse resize handle logic pattern
- `src/coral/static/style.css` — Add `.agentic-state.collapsed` with width transition

```css
.agentic-state.collapsed {
    width: 36px;
    min-width: 36px;
}

.agentic-state.collapsed .agentic-panel,
.agentic-state.collapsed .agentic-state-tabs {
    display: none;
}

.agentic-collapse-btn {
    position: absolute;
    top: 6px;
    left: -12px;
    width: 24px;
    height: 24px;
    border-radius: 50%;
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    color: var(--text-muted);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 10;
    transition: all 0.15s;
}
```

---

### Phase 4: Typography & Color Upgrade (Medium Impact, Low Risk)

**Goal:** Replace the generic system font stack with a distinctive developer-oriented
typeface and refine the color palette for better contrast.

#### 3.9 Font Upgrade

**Recommended:** [Plus Jakarta Sans](https://fonts.google.com/specimen/Plus+Jakarta+Sans)
— geometric, modern, excellent at small sizes, free.

**Alternative:** [Geist Sans](https://vercel.com/font) — Vercel's system font,
purpose-built for developer UIs.

**Files to modify:**
- `src/coral/templates/index.html` — Add Google Fonts `<link>` tag
- `src/coral/static/style.css` — Update `body` font-family

```css
body {
    font-family: 'Plus Jakarta Sans', -apple-system, BlinkMacSystemFont,
                 'Segoe UI', sans-serif;
}
```

#### 3.10 Color Palette Refinement

Increase separation between background layers and add depth cues:

```css
:root {
    /* Increase contrast between layers */
    --bg-primary: #0a0e14;      /* Deeper base (was #0d1117) */
    --bg-secondary: #12171e;    /* Slightly more contrast (was #161b22) */
    --bg-tertiary: #1c2128;     /* Component surfaces (was #21262d) */
    --bg-hover: #2a313a;        /* Hover (was #30363d) */

    /* Add depth variable for overlays */
    --bg-elevated: #252b33;
    --shadow-md: 0 4px 16px rgba(0, 0, 0, 0.25);
    --shadow-lg: 0 8px 32px rgba(0, 0, 0, 0.35);
}
```

#### 3.11 Top Bar Glassmorphism

Add a subtle frosted glass effect to the top bar for depth:

```css
.top-bar {
    background: rgba(18, 23, 30, 0.85);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border-bottom: 1px solid rgba(48, 54, 61, 0.6);
}
```

---

### Phase 5: Command Pane Polish (Medium Impact, Low Risk)

**Goal:** Reduce toolbar visual noise and make the primary action (Send) more
prominent.

#### 3.12 Toolbar Grouping

Reorganize the toolbar into clear groups with visual separators:

```
[Plan Mode] [Accept Edits] [Bash Mode] [new branch]  |  [macros...]  |  [Esc] [↑] [↓]  <spacer>  [Send]
 ─── mode group ───────────────────────   ── macros ──   ── nav ────         ── primary ──
```

Keep the existing `toolbar-divider` pattern but ensure groups are visually distinct.

#### 3.13 Enhanced Send Button

Make Send the obvious primary action:

```css
.btn-send {
    background: var(--accent);
    color: #fff;
    font-weight: 700;
    padding: 6px 20px;
    border-radius: 8px;
    box-shadow: 0 2px 8px rgba(88, 166, 255, 0.25);
    transition: all 0.15s;
}

.btn-send:hover {
    background: #70b8ff;
    box-shadow: 0 4px 12px rgba(88, 166, 255, 0.35);
    transform: translateY(-1px);
}
```

#### 3.14 Keyboard Shortcut Hint

Add a subtle hint below the textarea:

```html
<div class="command-hint">
    <kbd>Ctrl</kbd>+<kbd>Enter</kbd> to send
</div>
```

---

### Phase 6: Welcome Screen & Polish (Low Impact, Low Risk)

#### 3.15 Enhanced Welcome Screen

Replace the plain text with a centered layout featuring the Coral logo, quick
action buttons, and recent activity.

```html
<div class="welcome">
    <img src="/static/coral.png" alt="Coral" class="welcome-logo">
    <h2>Welcome to Coral</h2>
    <p>Select a session from the sidebar, or get started:</p>
    <div class="welcome-actions">
        <button class="welcome-action-card" onclick="showLaunchModal()">
            <span class="material-icons">add_circle</span>
            <span>Launch Agent</span>
        </button>
        <button class="welcome-action-card" onclick="showSettingsModal()">
            <span class="material-icons">settings</span>
            <span>Settings</span>
        </button>
    </div>
</div>
```

#### 3.16 Micro-interaction Improvements

- **Sidebar hover:** Add subtle `box-shadow` on hover for session items
- **Status dots:** Smoother pulse animation with `cubic-bezier` easing
- **Resize handles:** Add a centered grip pattern (3 dots)
- **Transitions:** Ensure all interactive elements have consistent `0.15s ease` transitions

#### 3.17 Accessibility Pass

- Add `aria-label` to all icon-only buttons (top bar, toolbar, filter chips)
- Add `role="status"` to status dot containers
- Add `aria-expanded` to collapsible sections
- Ensure focus-visible outlines on all interactive elements
- Add `title` attributes to truncated text (sidebar session names)

---

## 4. Implementation Order & Dependencies

```
Phase 1 (Sidebar)         ──── no dependencies, start here
    │
Phase 2 (Header)          ──── independent of Phase 1
    │
Phase 3 (Right Panel)     ──── independent
    │
Phase 4 (Typography)      ──── independent, can run parallel
    │
Phase 5 (Command Pane)    ──── independent
    │
Phase 6 (Polish)          ──── after Phases 1-5 (builds on all changes)
```

All phases are independently implementable. Phases 1-4 can be done in parallel.
Phase 6 should come last as it involves final polish across all changed areas.

---

## 5. Files Affected Summary

| File | Phases |
|---|---|
| `src/coral/templates/includes/sidebar.html` | 1 |
| `src/coral/templates/includes/views/live_session.html` | 2, 3, 5 |
| `src/coral/templates/index.html` | 4, 6 |
| `src/coral/static/style.css` | 1, 2, 3, 4, 5, 6 |
| `src/coral/static/sidebar.js` | 1, 3 |
| `src/coral/static/render.js` | 1, 2 |
| `src/coral/static/controls.js` | 2 |
| `src/coral/static/modals.js` | 2 |
| `src/coral/static/live_jobs.js` | 1 |
| `src/coral/static/scheduler.js` | 1 |
| `src/coral/static/websocket.js` | 2 |
| `src/coral/static/agentic_state.js` | 3 |

---

## 6. Non-Goals (Out of Scope)

- **Framework migration** (React/Vue) — too large for a cleanup pass
- **CSS file splitting** — valuable but orthogonal to UX improvements
- **Virtual scrolling** — performance optimization, separate effort
- **Mobile responsiveness** — desktop-first tool, separate roadmap item
- **Dark/light theme toggle** — separate feature request
