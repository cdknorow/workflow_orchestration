# Use Case Narrative: Multi-Agent Team Session

## Purpose

This document captures a real production session of the Coral orchestration system, demonstrating the operator-orchestrator-specialist team pattern described in the patent claims. This session produced real deliverables across legal, business, engineering, design, security, and documentation domains — all coordinated through the system's message board and managed via the web dashboard.

---

## Session Overview

| Parameter | Value |
|-----------|-------|
| Duration | ~2 days (March 21-23, 2026) |
| Total messages exchanged | 280+ |
| Team size (peak) | 9 agents + 1 human operator |
| Domains covered | UI/UX design, frontend engineering, backend engineering, legal/IP, business development, security, QA, documentation |
| Deliverables produced | 15+ (see below) |

## Team Composition

The session began with 5 agents and grew dynamically as the operator added specialists:

| Role | Agent Type | When Joined | Responsibility |
|------|-----------|-------------|----------------|
| **Operator** | Human | Session start | Strategic direction, final decisions, feedback |
| **Orchestrator** | AI Agent | Session start | Task delegation, coordination, status synthesis |
| **UI/UX Designer** | AI Agent | Session start | Design critique, layout recommendations, accessibility review |
| **Frontend Dev** | AI Agent | Session start | HTML/CSS/JS implementation, landing pages |
| **QA Engineer** | AI Agent | Session start | Test execution, bug verification, regression checks |
| **Lead Developer** | AI Agent | Session start | Backend architecture, code review, pip install verification |
| **Legal Advisor** | AI Agent | Added mid-session | Patent drafting, IP strategy, licensing analysis |
| **Documentation Expert** | AI Agent | Added mid-session | Technical writing, patent specification, README rewrite |
| **Business Development** | AI Agent | Added later | Pricing research, launch planning, go-to-market strategy |
| **Security Reviewer** | AI Agent | Added later | Vulnerability assessment, OWASP review, hardening recommendations |

This demonstrates **dynamic team composition** — the operator added agents with new specializations as the project scope expanded, without disrupting running agents.

## The Operator-Orchestrator-Specialist Pattern

### Layer 1: Operator (Human)

The human operator provided:
- Strategic direction ("Move agent teams out of folder scoping")
- Feedback on work products ("The patent is too implementation-specific — abstract it")
- Go/no-go decisions ("File the non-provisional patent tomorrow")
- Real-time course corrections ("Remove those three paragraphs — too detailed")

The operator communicated through the dashboard, sending instructions that the Orchestrator translated into specific agent assignments.

### Layer 2: Orchestrator (AI Agent)

The Orchestrator served as the coordination hub:
- **Task delegation**: Broke operator requests into specific assignments for specialist agents (e.g., "Frontend Dev — fix these 3 CSS issues; UI/UX Designer — review when done")
- **Status synthesis**: Tracked progress across all agents and reported back to the operator
- **Conflict resolution**: When the Legal Advisor and Documentation Expert were both editing the patent document, the Orchestrator coordinated who would handle which sections
- **Dynamic team management**: Recommended adding new agents when the scope expanded (e.g., "We need a Security Reviewer before public launch")

The Orchestrator processed 71 messages — more than any other agent — reflecting its role as the communication hub.

### Layer 3: Specialist Agents

Each specialist agent operated autonomously within its domain, communicating through the shared message board:

- **UI/UX Designer** produced detailed design critiques with specific recommendations (Hick's Law compliance, F-pattern reading, Nielsen heuristic references), which the Frontend Dev implemented
- **Frontend Dev** implemented UI changes and posted completion confirmations for QA verification
- **Legal Advisor** drafted patent claims and reviewed them for Alice/Mayo compliance
- **Documentation Expert** wrote technical specifications grounded in actual codebase analysis
- **QA Engineer** ran test suites and verified fixes independently
- **Security Reviewer** performed a comprehensive audit and categorized findings by severity

## Cross-Domain Collaboration Examples

### Example 1: Patent Filing (Legal + Documentation + Engineering)

1. Orchestrator assigned the Legal Advisor and Documentation Expert to collaborate on patent documents
2. Legal Advisor drafted claims and legal structure, posted to the board requesting technical specification input
3. Documentation Expert read the codebase (session_manager.py, pulse_detector.py, messageboard/store.py), wrote detailed technical specifications, and posted them for legal review
4. Legal Advisor verified technical accuracy of the specifications against the claims
5. Lead Developer reviewed the final document for remaining tool-specific language
6. Three rounds of abstraction were coordinated through the board, with each agent handling their area of expertise

### Example 2: Landing Page (Business + Design + Engineering + QA)

1. Business Development produced a pricing analysis and recommended tier structure
2. Orchestrator assigned Frontend Dev to build the landing page with pricing from the analysis
3. UI/UX Designer reviewed the draft and produced a prioritized list of design improvements
4. Orchestrator translated the design review into specific implementation tasks for Frontend Dev
5. QA Engineer independently verified all fixes and flagged a critical bug (wrong pip package name)
6. The cycle repeated for a "What is a Super Harness?" section: Business Dev + Designer drafted content, Frontend Dev implemented, Designer approved

### Example 3: Security Hardening (Security + Engineering + QA)

1. Security Reviewer performed a comprehensive audit and posted findings with severity ratings
2. Orchestrator triaged findings into "fix now" vs "design decision needed"
3. Lead Developer and Frontend Dev split the fixes based on backend/frontend responsibility
4. QA Engineer verified each fix after implementation
5. Security Reviewer confirmed the fixes addressed the vulnerabilities

## Deliverables Produced

This single session produced the following tangible outputs:

### Product & Marketing
- Landing page for coralai.ai (responsive, dark theme, pricing tiers)
- Company page for subgentic.ai
- coral-desktop README for the desktop app repo
- README.md rewrite as a marketing document

### Business Strategy
- Competitive pricing analysis across AI coding tools
- 4-tier pricing recommendation (Community/Pro/Team/Enterprise)
- 30-day launch plan with week-by-week milestones
- Show HN post draft + 5 Reddit post variations
- Binary distribution strategy (GitHub Releases + GoReleaser)

### Engineering
- Security audit report (14 findings, 5 critical/high)
- Security hardening commit (auth, XSS, command injection, file permissions)
- Prompt delivery refactor (CLI argument instead of tmux send-keys)
- pip install verification and --help fix

### Design
- Sidebar restructuring (team-first grouping with accent borders)
- Multiple UI refinements (activity panel, kebab menus, sleeping agents, toolbar buttons)
- "What is a Super Harness?" section content and layout

## Message Board Dynamics

The cursor-based message board enabled several coordination patterns:

1. **Parallel work streams**: While the Legal Advisor drafted patent claims, the Frontend Dev built the landing page, and the QA Engineer ran tests — all simultaneously, all posting updates to the same board.

2. **Review chains**: Designer posts critique → Orchestrator translates to tasks → Developer implements → Designer confirms → QA verifies. Each step was a board message, creating an auditable trail.

3. **Dynamic agent addition**: When the Security Reviewer joined mid-session, they could read the full board history to understand context, then immediately begin their audit. No onboarding delay.

4. **Conflict avoidance**: When the Legal Advisor and Documentation Expert were both editing the patent document, the Orchestrator used the board to assign non-overlapping sections, preventing edit conflicts.

## Significance for Patent Claims

This session demonstrates every element of the patent claims in production use:

- **Claim 1 (Team Formation)**: Team was configured with roles, behavior prompts, and a shared board. Agents were added dynamically without disrupting running agents.
- **Claim 2 (Status Extraction)**: The dashboard showed real-time status of all 9 agents throughout the session.
- **Claim 3 (Cursor-Based Communication)**: 280+ messages delivered with exactly-once semantics. Agents read at their own pace. No messages lost during agent additions.
- **Claim 4 (Session Persistence)**: Agents were available across the multi-day session with preserved context.

The operator-orchestrator-specialist hierarchy is the system's primary usage pattern, and the patent claims are broad enough to cover it without prescribing the specific role names or domain specializations.
