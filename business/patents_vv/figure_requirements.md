# Figure Requirements — Patent Filing Package

**Consolidated list of all screenshots and figures needed for both the utility patent and design patent applications.**

**Disclaimer: This document is a draft template for informational purposes only. Consult with a qualified patent attorney before filing.**

---

## Overview

This document specifies all visual assets needed for the patent filing package. Screenshots should be captured at high resolution (2x/Retina if possible) in a clean state (no personal data, placeholder/demo content preferred).

For the **utility patent**, figures illustrate the technical architecture and method flows.
For the **design patent**, figures show the ornamental GUI design in formal drawing format.

---

## UTILITY PATENT FIGURES

### FIGURE U1 — System Architecture Diagram

**Type**: Technical diagram (to be created by illustrator or diagramming tool)

**Content**: Block diagram showing the major system components and their relationships:
- Agent Process Manager (spawns agents in tmux sessions)
- Git Worktrees (isolated execution environments)
- PULSE Protocol Scanner (reads log files, extracts events)
- Message Board System (SQLite store + CLI + REST API)
- Session Persistence Layer (SQLite database)
- Web Dashboard (FastAPI + WebSocket + browser clients)
- Arrows showing data flow: agent output -> log files -> PULSE scanner -> database -> WebSocket -> dashboard

**Notes**: Standard patent-style block diagram with labeled boxes and directional arrows. No screenshots needed — this is a schematic.

---

### FIGURE U2 — Agent Spawning Flowchart

**Type**: Method flowchart (to be created)

**Content**: Step-by-step flow for Claim 1 (Dynamic AI Agent Team Formation):
1. START: User submits team configuration via UI modal
2. For each agent in team definition:
   a. Select/create git worktree
   b. Create tmux session with UUID name
   c. Configure pipe-pane log streaming
   d. Launch agent CLI in tmux session
   e. Write board state file
   f. Subscribe agent to team message board
   g. Transmit behavior prompt to agent
3. Render team card in sidebar
4. END

**Notes**: Standard patent flowchart with rectangular process boxes, diamond decision boxes, and arrows.

---

### FIGURE U3 — PULSE Protocol Detection Flowchart

**Type**: Method flowchart (to be created)

**Content**: Step-by-step flow for Claim 2 (PULSE Protocol):
1. START: Scan cycle triggered
2. Read file position pointer for agent log
3. Check if new content exists (file size > position)
4. If no: RETURN (no new content)
5. If yes: Seek to position, read new content
6. Strip ANSI escape sequences
7. Apply regex pattern match for ||PULSE:TYPE payload||
8. For each match:
   a. Extract event type and payload
   b. If STATUS/SUMMARY: Check for deduplication (has value changed?)
      - If changed: Broadcast via WebSocket to dashboard clients
      - If unchanged: Skip
   c. If CONFIDENCE: Store as activity event
9. Update file position pointer
10. Check idle threshold (file mtime vs current time)
11. If idle > threshold AND last event = notification: Trigger webhook
12. END

**Notes**: Include the regex pattern `\|\|PULSE:(STATUS|SUMMARY|CONFIDENCE)\s+(.*?)\|\|` as an annotation.

---

### FIGURE U4 — Message Board Cursor Mechanism Flowchart

**Type**: Method flowchart (to be created)

**Content**: Step-by-step flow for Claim 3 (Cursor-Based Communication):

**Read Operation:**
1. Agent calls `coral-board read`
2. System retrieves agent's `last_read_id` from subscriber record
3. Query: SELECT messages WHERE id > last_read_id AND sender != agent
4. Compute new cursor = MAX(current_cursor, highest_returned_msg_id, agent's_own_highest_msg_id)
5. UPDATE subscriber SET last_read_id = new_cursor
6. Return messages to agent

**Post Operation:**
1. Agent calls `coral-board post "message"`
2. INSERT new message with next sequential ID
3. No cursor advancement for any subscriber
4. Return confirmation

**Notes**: Show both operations in a single figure with clear separation.

---

### FIGURE U5 — Session Sleep/Wake State Diagram

**Type**: State transition diagram (to be created)

**Content**: State diagram for Claim 4 (Session Persistence):

States:
- **Active**: Agent process running, board subscribed, responding to commands
- **Sleeping**: Process suspended, session record preserved, board cursor frozen
- **Terminated**: Process killed, session moved to history

Transitions:
- Active -> Sleeping: Sleep command (preserves prompt, board, cursor)
- Sleeping -> Active: Wake command (re-spawns process, re-subscribes board, re-sends prompt, delivers unread messages)
- Active -> Terminated: Kill command
- Sleeping -> Terminated: Kill command

**Notes**: Standard state diagram with circles/ovals for states and labeled arrows for transitions.

---

### FIGURE U6 — Subscription Transfer Diagram

**Type**: Sequence diagram (to be created)

**Content**: Shows the subscription transfer process when an agent session restarts:
1. Agent A has session_id = "old-uuid", last_read_id = 47
2. Agent A process dies/restarts
3. New process gets session_id = "new-uuid"
4. System calls transfer_subscription("project", "old-uuid", "new-uuid")
5. New subscriber record created with session_id = "new-uuid", last_read_id = 47
6. Old subscriber record deleted
7. Messages 48, 49, 50 (posted during downtime) now available on next read

**Notes**: Standard sequence diagram with lifelines for Agent, System, and Database.

---

## DESIGN PATENT FIGURES

All design patent figures require both a **screenshot** (for reference) and a **formal line drawing** (for filing). The patent illustrator will convert screenshots to drawings.

### FIGURE D1 — Full Dashboard View

**Screenshot instructions**:
- Capture the full browser window (no OS chrome)
- Dashboard should show:
  - Left sidebar with at least 2 team cards (1 expanded with 3+ agents, 1 collapsed)
  - At least 1 standalone agent below the teams
  - Main panel showing an active agent's session
  - Activity panel at the bottom
- Use demo/placeholder content (no real proprietary code visible)
- Light theme preferred for primary figure

**Formal drawing notes**: Solid lines for sidebar layout, team cards, status dots, kebab menus. Broken lines for browser chrome, terminal content, scrollbars.

---

### FIGURE D2 — Sidebar Detail (Both Team States)

**Screenshot instructions**:
- Crop to just the sidebar
- Show at minimum:
  - Workspace header with name + gear icon
  - Team card A: expanded, 3-4 agents with different status dot colors (green, yellow, gray)
  - Team card B: collapsed, different accent border color, folder path footer visible
  - Standalone agents section with folder headers
- Ensure two different accent border colors are visible (e.g., blue and orange)

**Formal drawing notes**: Solid lines for all team card elements, accent borders, status dots, text layout, kebab icons. Broken lines for actual text content (names, paths).

---

### FIGURE D3 — Team Card Expanded (Close-Up)

**Screenshot instructions**:
- Crop tightly to a single expanded team card
- Show:
  - Full accent color left border
  - Header with chevron (down), team name, member count badge, kebab
  - 3-4 agent rows with varying status dots
  - Goal summary text visible (truncated with ellipsis on at least one)
- High zoom level for detail visibility

**Formal drawing notes**: Solid lines for all elements. This is the primary detail view of the team card component.

---

### FIGURE D4 — Team Card Collapsed (Close-Up)

**Screenshot instructions**:
- Crop tightly to a single collapsed team card
- Show:
  - Accent color left border (shorter height)
  - Header with chevron (right), team name, member count badge, kebab
  - Folder path footer with path text + copy icon

**Formal drawing notes**: Solid lines for all elements. Show the contrast with the expanded state.

---

### FIGURE D5 — Agent Status Rows (Close-Up)

**Screenshot instructions**:
- Crop to show 3-4 agent rows within a team card
- Ideally show all three status dot states:
  - Green dot: active agent
  - Yellow/amber dot: idle agent
  - Gray dot (dimmed): sleeping agent
- Show at least one row with goal text truncated with ellipsis
- Show hover state if possible (background highlight on one row)

**Formal drawing notes**: Solid lines for dots, text layout, kebab icons. May need a composite view showing all dot color states.

---

### FIGURE D6 — Team Controls Dropdown

**Screenshot instructions**:
- Show a team card with the kebab menu dropdown open
- Dropdown should display:
  - Add Agent (with plus icon)
  - Divider
  - Sleep All (moon icon)
  - Wake All (sun icon)
  - Kill All (X icon)
  - Board link
- The dropdown should overlay content below it

**Formal drawing notes**: Solid lines for dropdown container, menu items, icons, divider.

---

### FIGURE D7 — Workspace Settings Menu

**Screenshot instructions**:
- Show the sidebar header with the gear icon dropdown open
- Dropdown should display:
  - Group by Team toggle (with checkmark in checked state)
  - Divider
  - Sleep All / Wake All
  - Theme options (if visible)

**Formal drawing notes**: Solid lines for dropdown items including checkmark icon.

---

### FIGURE D8 — Folder-First Mode

**Screenshot instructions**:
- Toggle "Group by Team" to OFF
- Capture sidebar showing folder-first organization:
  - Agents grouped by directory/folder
  - Within folders, agents on the same board shown as nested sub-groups
  - Visual distinction from team-first mode

**Formal drawing notes**: Solid lines for the alternate grouping layout. This establishes both modes as part of the claimed design.

---

## CAPTURE CHECKLIST

| Figure | Type | Source | Status |
|--------|------|--------|--------|
| U1 | System architecture diagram | Created by illustrator | [ ] Pending |
| U2 | Agent spawning flowchart | Created by illustrator | [ ] Pending |
| U3 | PULSE protocol flowchart | Created by illustrator | [ ] Pending |
| U4 | Message board cursor flowchart | Created by illustrator | [ ] Pending |
| U5 | Sleep/wake state diagram | Created by illustrator | [ ] Pending |
| U6 | Subscription transfer sequence | Created by illustrator | [ ] Pending |
| D1 | Full dashboard screenshot | Captured from running Coral | [ ] Pending |
| D2 | Sidebar detail screenshot | Captured from running Coral | [ ] Pending |
| D3 | Team card expanded screenshot | Captured from running Coral | [ ] Pending |
| D4 | Team card collapsed screenshot | Captured from running Coral | [ ] Pending |
| D5 | Agent status rows screenshot | Captured from running Coral | [ ] Pending |
| D6 | Team controls dropdown screenshot | Captured from running Coral | [ ] Pending |
| D7 | Workspace settings menu screenshot | Captured from running Coral | [ ] Pending |
| D8 | Folder-first mode screenshot | Captured from running Coral | [ ] Pending |

**Total figures needed: 14** (6 utility diagrams + 8 design screenshots/drawings)

---

## PRE-CAPTURE SETUP CHECKLIST

Before capturing design patent screenshots, ensure the following dashboard state:

- [ ] At least 2 agent teams running with different team names (e.g., "onboarding-flow", "auth-refactor")
- [ ] Team 1: 3-4 agents with mixed states — at least one active (green dot), one idle (yellow dot), one sleeping (gray dot)
- [ ] Team 2: 2-3 agents, ideally all active or a mix
- [ ] At least 1 standalone agent (launched individually, not part of any team)
- [ ] Active agents have been running long enough to emit PULSE:SUMMARY events (goal text visible in sidebar)
- [ ] Active agents have emitted multiple PULSE:STATUS events (activity timeline populated in main panel)
- [ ] Browser: clean Chrome or Firefox window, no bookmarks bar, no visible extensions, no DevTools
- [ ] Resolution: 1920x1080 minimum; 2x Retina/HiDPI preferred for close-up detail shots
- [ ] Theme: Default light theme for all primary figures (dark mode screenshots optional as supplementary)
- [ ] No sensitive/proprietary content visible — use generic project names and demo code

---

## NOTES FOR PATENT ILLUSTRATOR

1. All utility patent figures (U1-U6) should follow standard USPTO patent drawing conventions: black lines on white background, reference numerals, consistent line weights.

2. All design patent figures (D1-D8) require conversion from screenshots to formal line drawings:
   - Solid lines for claimed design elements (see design_patent_application.md for specification)
   - Broken/dashed lines for unclaimed context elements
   - No color in line drawings (unless filing with color petition)
   - Surface shading (stippling/hatching) may be used to indicate colored areas (accent borders, status dots)

3. Figure numbering in the final filing should be sequential (FIG. 1, FIG. 2, etc.) — the U/D prefixes here are for organizational reference only.

4. Each figure needs a brief description in the specification (e.g., "FIG. 1 is a block diagram showing the system architecture of the multi-agent orchestration system.").
