# Design Patent Application

## Graphical User Interface for a Multi-Agent Software Development Dashboard

**Disclaimer: This document is a draft template for informational purposes only. Consult with a qualified patent attorney before filing. Design patent applications require formal drawings prepared by a patent illustrator to USPTO standards.**

---

## CROSS-REFERENCE TO RELATED APPLICATIONS

This application is related to U.S. Provisional Patent Application No. [TO BE ASSIGNED], titled "System and Method for Multi-Agent AI Orchestration with Isolated Execution Environments and Collaborative Communication," filed [FILING DATE].

## TITLE OF THE INVENTION

Graphical User Interface for a Multi-Agent Software Development Dashboard

## CLAIM

The ornamental design for a graphical user interface for a multi-agent software development dashboard, as shown and described.

---

## DESCRIPTION OF THE DESIGN

The design relates to the ornamental appearance of a graphical user interface (GUI) displayed on a computer screen or display panel. The GUI is used for monitoring and controlling multiple artificial intelligence coding agents operating in parallel.

### Design Elements (Claimed — Solid Lines)

The following design elements constitute the claimed ornamental design and must be rendered in solid lines in the formal drawings:

#### 1. Team Card Component

The team card is a vertically-oriented container element with the following distinctive ornamental features:

- **Accent Color Left Border**: A solid vertical color stripe running the full height of the left edge of the card. The border color is unique per team (assigned from a palette) and serves as the primary visual identifier for team grouping. The border width is approximately 3-4 pixels.

- **Card Header Bar**: A horizontal header containing, from left to right:
  - An expand/collapse chevron icon (downward-pointing triangle when expanded, rightward when collapsed)
  - The team name in bold text
  - A numeric badge showing member count (circular or rounded-rectangle background)
  - A three-dot vertical kebab menu icon (right-aligned)

- **Expanded State**: When expanded, the card body contains a vertical stack of agent rows (see Agent Status Row below), separated by subtle dividers. The accent border continues along the full expanded height.

- **Collapsed State**: When collapsed, only the header bar and a footer showing the folder path are visible. The accent border remains visible. The folder path footer shows a truncated directory path with a copy-to-clipboard icon.

#### 2. Agent Status Row Component

Each agent within a team card (or standalone) is displayed as a horizontal row with the following ornamental features:

- **Status Dot**: A small filled circle (approximately 8-10px diameter) positioned at the left edge of the row. The dot color indicates agent state:
  - Green (filled): Active/working
  - Yellow/amber (filled): Idle or needs attention
  - Gray (filled, reduced opacity): Sleeping/suspended

- **Agent Identity Text**: The agent's folder name or display name, displayed in medium-weight text immediately right of the status dot.

- **Goal Summary Text**: A single line of truncated text (with ellipsis) showing the agent's current goal/summary, displayed in lighter weight below or right of the agent name. The text is a muted color relative to the agent name.

- **Row Kebab Menu**: A three-dot vertical icon (right-aligned) that opens a dropdown with agent-specific actions.

- **Row Hover State**: On mouse hover, the row background subtly highlights to indicate interactivity.

#### 3. Sidebar Layout

The sidebar occupies the left portion of the screen and contains:

- **Workspace Header**: A top bar containing the workspace name (left-aligned) and a gear icon (right-aligned) that opens a settings dropdown menu.

- **Team Cards Section**: A vertically scrollable list of team cards, each with the accent border and expand/collapse behavior described above.

- **Standalone Agents Section**: Below the team cards, individual agents not assigned to a team are grouped by folder name under folder-header rows.

- **Section Hierarchy**: Teams appear above standalone agents, creating a visual hierarchy that emphasizes collaborative groups.

#### 4. Team Controls Dropdown

The kebab menu on team cards opens a dropdown menu with the following distinctive layout:

- **Action Items**: Vertically stacked buttons with icon + text, including: "Add Agent" (plus icon), "Sleep All" (moon icon), "Wake All" (sun icon), "Kill All" (X icon)
- **Divider**: A horizontal line separating view-preference items from bulk actions
- **Board Link**: A clickable link to the team's message board

#### 5. Activity Panel Header

The bottom section of the main panel features a minimal header containing:

- **Filter Button**: An icon-only button (funnel icon) with an optional numeric badge showing active filter count
- **No label text or border**: The header is visually clean with no "Activity" label and no bottom border

#### 6. Dashboard Three-Panel Layout

The overall screen layout comprises:

- **Left Panel (Sidebar)**: Fixed-width, containing the workspace header and agent/team list described above
- **Center Panel (Main)**: Variable-width, containing the selected agent's interaction area (terminal/chat view) and activity timeline
- **The proportions**: Sidebar approximately 20-25% of screen width; main panel fills remaining space

### Unclaimed Context Elements (Broken Lines)

The following elements provide context but are NOT part of the claimed design. They must be rendered in broken (dashed) lines in formal drawings:

- Browser chrome (title bar, address bar, navigation buttons, tabs)
- Scrollbars
- Generic text content within agent interaction areas
- Standard form elements (text inputs, standard buttons without distinctive styling)
- Terminal/code content displayed in the main panel
- Operating system UI elements (dock, menu bar, taskbar)
- Monitor/screen bezel

---

## FIGURE DESCRIPTIONS

### FIGURE 1 — Full Dashboard View (Default State)

A front elevational view of the complete dashboard interface showing:
- Left sidebar with workspace header, two team cards (one expanded, one collapsed), and standalone agent entries below
- Center main panel with agent interaction area
- The three-panel proportional layout

**Purpose**: Establishes the overall composition and spatial relationships of the claimed design elements.

**Screenshot needed**: Full browser window showing the dashboard with at least 2 teams (one expanded, one collapsed) and at least 1 standalone agent visible in the sidebar. Main panel showing an active agent session.

### FIGURE 2 — Sidebar Detail (Teams Expanded)

An enlarged view of the sidebar showing:
- Workspace header with name and gear icon
- First team card: expanded, showing accent color left border, team name header with chevron (down), member count badge, kebab icon, and 3-4 agent status rows with dots, names, goal text, and row kebab menus
- Second team card: collapsed, showing accent color left border (different color), team name header with chevron (right), member count badge, and folder path footer
- Standalone agents section below with folder grouping headers

**Purpose**: Shows the detailed ornamental design of team cards in both expanded and collapsed states, and the visual hierarchy between teams and standalone agents.

**Screenshot needed**: Sidebar zoomed/cropped showing both team states. Ensure two different accent border colors are visible.

### FIGURE 3 — Team Card Expanded (Close-Up)

A close-up view of a single expanded team card showing:
- Accent color left border (full height)
- Header: chevron (down) + team name (bold) + member count badge + kebab icon
- Agent rows (3-4 agents):
  - Each row: status dot (green/yellow/gray) + agent name + goal summary text (truncated with ellipsis) + row kebab
- The visual rhythm and spacing between rows

**Purpose**: Shows the distinctive ornamental arrangement of elements within the team card component.

**Screenshot needed**: Single team card expanded, cropped closely. Multiple agents with different status dot colors if possible.

### FIGURE 4 — Team Card Collapsed (Close-Up)

A close-up view of a single collapsed team card showing:
- Accent color left border (reduced height)
- Header: chevron (right) + team name (bold) + member count badge + kebab icon
- Folder path footer with directory path and copy icon

**Purpose**: Shows the collapsed state design, particularly the persistent accent border and the folder path footer.

**Screenshot needed**: Single team card collapsed, cropped closely.

### FIGURE 5 — Agent Status Row (Close-Up)

A close-up view of individual agent status rows showing:
- Status dot in three states (active/idle/sleeping) — may need composite or three separate rows
- Agent name text
- Goal summary text (truncated)
- Kebab menu icon
- Hover highlight state (if capturable)

**Purpose**: Shows the distinctive ornamental design of the agent status row component at readable scale.

**Screenshot needed**: 3-4 agent rows at high zoom, ideally showing different status dot colors.

### FIGURE 6 — Team Controls Dropdown

A view showing the team kebab dropdown menu open, displaying:
- "Add Agent" with plus icon
- Divider line
- "Sleep All" with moon icon
- "Wake All" with sun icon
- "Kill All" with X icon
- Board link

**Purpose**: Shows the ornamental design of the team-level controls dropdown.

**Screenshot needed**: Team card with kebab menu open, showing all dropdown items.

### FIGURE 7 — Workspace Settings Menu

A view showing the workspace gear icon dropdown open, displaying:
- "Group by Team" toggle with checkmark (checked state)
- Divider
- "Sleep All" / "Wake All" bulk actions
- Theme options

**Purpose**: Shows the settings menu design including the team grouping toggle.

**Screenshot needed**: Sidebar header with gear dropdown open.

### FIGURE 8 — Sidebar Alternate Mode (Folder-First)

A view of the sidebar in folder-first grouping mode showing:
- Agents grouped by folder/directory
- Within each folder, agents on the same board shown as nested team sub-groups with accent borders
- The visual distinction between this mode and the default team-first mode

**Purpose**: Shows the alternate grouping mode's ornamental design, establishing that both modes are part of the claimed design.

**Screenshot needed**: Sidebar with "Group by Team" toggle OFF, showing folder-first organization.

---

## FILING NOTES

### Drawing Requirements (USPTO)

Design patent drawings must comply with 37 CFR 1.84:

1. **Format**: Black and white line drawings (no photographs unless exceptional circumstances). Alternatively, color drawings with petition and fee.
2. **Solid lines**: Claimed design elements (as specified above).
3. **Broken lines**: Environmental/contextual elements not claimed.
4. **Surface shading**: May be used to show contour, depth, or material. Stippling or hatching can indicate color or transparency.
5. **Views**: Must include sufficient views to fully disclose the design. For a GUI, typically a front elevational view plus detail views.
6. **Consistency**: All figures must show the same design — no contradictions between views.

### Recommended Approach

1. Capture all screenshots specified in the Figure Requirements document
2. Engage a patent illustrator experienced in GUI design patents to convert screenshots to formal line drawings
3. The illustrator should:
   - Trace the GUI elements in solid lines (claimed) vs broken lines (unclaimed)
   - Use consistent line weights
   - Add figure numbers and reference numerals as needed
   - Prepare drawings at standard USPTO sizes

### Related Design Variations

Consider filing continuation-in-part applications for significant UI variations:
- Dark mode variant (if distinct enough from light mode)
- Mobile/responsive layout variant
- Message board view design
- Agent interaction panel design (terminal/chat area)

### Prior Art to Distinguish

- VS Code / JetBrains IDEs: Different domain (code editors), different layout patterns
- Vercel / Railway / Render dashboards: Deployment monitoring, not agent orchestration — different visual hierarchy
- Slack / Discord: Communication interfaces, not agent monitoring dashboards
- GitHub Actions / Jenkins: CI/CD pipeline views, not real-time agent status monitoring

### Filing Timeline

- Recommended filing: Within 6 months of provisional utility patent filing
- U.S. grace period deadline: February 17, 2027
- Design patent term: 15 years from grant date
- Expected prosecution time: 12-18 months

### Cost Estimate

| Item | Estimated Cost |
|------|---------------|
| Patent illustrator (8 figures) | $1,500 - $2,500 |
| Attorney preparation & filing | $1,500 - $2,500 |
| USPTO filing fee (small entity) | $1,040 |
| **Total** | **$4,040 - $6,040** |

### Inventor(s)
[TO BE COMPLETED — Same as utility patent]

### Assignee
[TO BE COMPLETED — Same as utility patent]
