# Utility Patent Application

## System and Method for Multi-Agent AI Orchestration for Collaborative Task Execution

---

## FIELD OF THE INVENTION

The present invention relates generally to artificial intelligence agent management systems, and more particularly to methods and systems for orchestrating multiple AI agents operating in parallel execution environments with real-time monitoring via an inline status extraction protocol, cursor-based inter-agent communication, and persistent session state management for collaborative task execution.

---

## BACKGROUND OF THE INVENTION

### Technical Problem

Task-oriented workflows increasingly leverage AI assistants (e.g., large language models) to automate tasks such as code generation, research, content creation, project management, and analysis. However, current approaches typically involve a single AI agent operating in a single environment, which creates several technical limitations:

1. **Serialization bottleneck**: A single agent can only work on one task at a time. Complex projects requiring parallel workstreams (e.g., frontend and backend development, testing and implementation) cannot be efficiently distributed.

2. **Conflict risk**: Multiple agents operating on shared resources simultaneously may produce conflicting modifications, requiring manual resolution that negates the efficiency gains of automation.

3. **Lack of coordination**: When multiple agents are deployed, there is no standardized mechanism for them to communicate progress, share context, or coordinate work — leading to duplicated effort, incompatible implementations, or blocked dependencies.

4. **Monitoring opacity**: Operators have no real-time visibility into what multiple concurrent agents are doing, their confidence levels, or whether they need human intervention — requiring manual inspection of each agent's terminal session.

5. **State fragility**: Agent sessions are ephemeral. If an agent process is interrupted, all context about its assigned task, team membership, communication history, and progress is lost.

### Prior Art and Limitations

**Multi-Agent AI Frameworks:**

Several multi-agent frameworks have been developed for AI orchestration, including AutoGen (Microsoft, September 2023), CrewAI (crewAI Inc., late 2023), LangGraph (LangChain, January 2024), MetaGPT (DeepWisdom, August 2023, NeurIPS 2023 workshop), CAMEL (CAMEL-AI, March 2023, NeurIPS 2023), and ChatDev (Tsinghua University, July 2023). These frameworks operate at the prompt/API level — they orchestrate LLM API calls within a single process. They do not address the problem of orchestrating independent, long-running agent processes that each have their own terminal session, file system access, and tool-use capabilities. AutoGen supports event-driven interaction patterns and OpenTelemetry observability but operates within a single process. CrewAI provides a role-based agent architecture with hierarchical delegation (manager/worker model) and a global message bus, but likewise executes within a single process boundary. LangGraph uses graph-based orchestration with state checkpointing but without process-level isolation. MetaGPT and ChatDev simulate software companies with specialized agent roles but communicate entirely through API-level structured communication within a single process. None of these frameworks provide process-level agent isolation, terminal output monitoring via structured inline markers, cursor-based inter-agent messaging with independent read positions, or session persistence with team state replay.

**IDE-Based Parallel Agent Systems:**

Cursor IDE (Anysphere Inc., released October 29, 2025 with Cursor 2.0) introduced parallel AI coding agents using git worktrees to provide isolated execution environments for up to 8 concurrent agents. Each agent operates on its own branch and working directory without file conflicts. However, Cursor's agents do not communicate with each other via a cursor-based message board with independent read positions and exactly-once delivery, do not emit structured status markers for inline extraction via a monitoring protocol, do not support session persistence with sleep/wake and team state replay, and do not provide a standalone real-time web dashboard with agent confidence levels and goals. Cursor operates within a single IDE, not as a standalone server application.

**Open-Source Multi-Agent Orchestration Tools:**

ccswarm (nwiizo, first released January 6, 2025) provides multi-agent orchestration using Claude Code with git worktree isolation, session persistence, intelligent task delegation, specialized agent pools (Frontend, Backend, DevOps, QA), and a Terminal UI for monitoring. However, ccswarm does not provide a cursor-based message board with independent read positions and exactly-once delivery, does not use an inline status extraction protocol parsing structured markers from terminal output, does not support configurable message filtering modes (all/mention/group), and does not support sleep/wake operations with board subscription and cursor preservation. Similarly, the blog post "Swarming the Codebase: Orchestrated Execution with Multiple Claude Code Agents" (Helio Medeiros, November 2025) describes orchestrating multiple Claude Code agents with git worktrees, demonstrating that the concept of parallel AI agent execution with worktree isolation was gaining community traction prior to the present invention's disclosure.

**Built-In Agent Team Features:**

Claude Code Agent Teams (Anthropic, released February 5, 2026 with Claude Opus 4.6) introduced built-in multi-agent capability where one agent acts as a Team Lead and others as Teammates. Agents message each other, claim tasks from shared lists, and work collaboratively using worktree isolation. This feature was described as experimental. However, Claude Code Agent Teams operates within Claude Code's built-in architecture and uses a fundamentally different communication mechanism — it does not provide a cursor-based message board with persistent independent read positions and a deadlock-free cursor advancement algorithm. It does not include an inline status extraction protocol that parses structured event markers from terminal output for real-time monitoring. It does not support configurable receive modes or mention-based message filtering. It does not provide session sleep/wake operations that preserve and restore board subscriptions, cursor positions, and behavior prompts. And it does not offer a standalone web dashboard with real-time push connections, agent confidence level display, and collective team operations (sleep all, wake all, terminate all). The present invention is not affiliated with Anthropic, and Claude Code Agent Teams constitutes prior art under 35 USC 102(a)(1). The present invention is distinguished by the combination of features that Claude Code Agent Teams lacks: cursor-based messaging with independent read positions, inline status extraction protocol, configurable receive modes with differentiated notification, session persistence with team state replay, and a real-time web monitoring dashboard.

**Terminal Monitoring Tools:**

TMAI — Tmux Multi Agents Interface (trust-delta, 2025) provides multi-agent monitoring to track Claude Code, OpenCode, Codex CLI, and Gemini CLI across tmux panes with real-time preview and ANSI color support. However, TMAI monitors raw terminal output without structured protocol extraction — it displays terminal content without parsing structured status markers, extracting confidence levels, or providing event deduplication and broadcast. TMAI provides no orchestration, team formation, agent spawning, inter-agent communication, or session persistence capabilities.

**Existing Terminal Multiplexers:**

Existing terminal multiplexers (e.g., tmux, first released 2007; GNU Screen, first released 1987) provide process isolation, session management, detach/reattach, output capture (e.g., pipe-pane), and key-sending mechanisms, but no agent-aware monitoring, communication, or state management.

**Message Queue Systems and Cursor-Based Messaging Patents:**

Established message queue systems including Apache Kafka (Apache Software Foundation, 2011), Apache Pulsar (originally Yahoo, 2016), and cursor-based messaging patents such as US7945631B2 (Microsoft, filed November 3, 2008; granted May 17, 2011) demonstrate that cursor/offset-based independent read positions and durable message consumption tracking are well-known in enterprise messaging. US7945631B2 teaches cursor components that maintain independent consumer state separately from the message log, supporting exactly-once, at-most-once, and at-least-once delivery guarantees in both queue and pub/sub messaging patterns. Apache Kafka uses consumer group offsets stored durably in an internal topic, and Apache Pulsar maintains independent subscription cursors stored in BookKeeper that survive consumer disconnection and broker restarts. However, these systems are designed for high-throughput distributed enterprise messaging, not lightweight AI agent inter-process communication. None of them provide self-message filtering (agents not receiving their own posts), the specific cursor advancement algorithm that computes the new position as the maximum of the current cursor, the highest delivered message identifier, and the subscriber's own highest posted message identifier to prevent deadlock from interleaved self-posts, subscription transfer on session restart with cursor preservation at the pre-restart value, pre-configured command-line interfaces with state files for AI agents that require no knowledge of server addresses or communication protocols, or configurable receive modes based on mention patterns for role-based notification filtering.

**Existing Dashboards:**

Existing dashboards (e.g., CI/CD dashboards, project management tools) do not provide real-time monitoring of AI agent activity, inline status extraction from terminal output, or support for agent-to-agent communication.

The present invention addresses these limitations by providing an integrated system for multi-agent orchestration that combines process-level execution environments associated with version-controlled repositories, real-time monitoring via an inline status extraction protocol operating at the terminal output level, cursor-based inter-agent communication with independent read positions and deadlock-free cursor advancement, and persistent session state management with team-level sleep/wake operations.

---

## SUMMARY OF THE INVENTION

The present invention provides a computer-implemented system and method for orchestrating multiple artificial intelligence agents for collaborative task execution. The system comprises:

1. **Agent Orchestration Engine**: Spawns and manages AI agent processes in execution environments associated with a version-controlled repository via a terminal multiplexer, enabling parallel task execution on shared resources. In one embodiment, each agent operates in an isolated version-controlled working copy branched from a shared repository, providing file-system-level isolation. In another embodiment, agents operate on a shared working copy with orchestrator-mediated sequencing to prevent conflicts.

2. **Inline Status Extraction Protocol**: A mechanism that parses structured event markers (STATUS, SUMMARY, CONFIDENCE) from agent terminal output in real time, without requiring agents to use a structured API — enabling monitoring of any command-line-based AI agent, including agent types not yet developed at the time of system deployment. The protocol specification is injected into the agent's behavior prompt, requiring no modification to the agent's underlying model, runtime, or tool-use framework.

3. **Message Board System**: A cursor-based inter-agent communication platform where each subscriber maintains an independent read position, with a cursor advancement algorithm that computes the new position as the maximum of the current cursor value, the highest delivered message identifier, and the subscriber's own highest posted message identifier, thereby preventing deadlock from self-posted messages. The system supports configurable receive modes, exactly-once delivery, and subscription transfer on agent restart with cursor preservation at the pre-restart value.

4. **Session Persistence Layer**: Stores agent configuration (type, execution environment, prompt, board subscription, read cursor) and supports sleep/wake operations that preserve and restore full team state, including re-subscribing agents to their communication channels at their preserved cursor positions and re-issuing behavior prompts to re-establish agent roles.

5. **Web Dashboard**: A real-time monitoring interface with push-based connections displaying agent status, goals, confidence levels, team groupings, and activity timelines, with controls for sending commands, managing teams, and adjusting agent behavior.

6. **Hierarchical Coordination**: A differentiated notification pattern wherein orchestrator agents receive all team messages while specialist agents receive only directed messages, enabling scalable multi-agent coordination with task decomposition, progressive discovery, and real-time conflict arbitration.

---

## BRIEF DESCRIPTION OF THE DRAWINGS

- FIG. 1 is a screenshot of a preferred embodiment of the web dashboard showing the system in operation. The left sidebar displays a hierarchical team view with multiple AI agents organized under a team heading, each showing real-time status indicators, activity timestamps, and goal summaries. The center panel shows an agent's terminal session with inline status extraction protocol markers visible in the output stream. The right panel shows the inter-agent message board with cursor-based communication between team members. The dashboard demonstrates the integrated monitoring and control interface described herein.

- FIG. 2 is a block diagram showing the major system components: Operator Interface (Web Dashboard), Orchestration Engine (Agent Process Manager, Inline Status Extraction Engine, Session Persistence Layer, Message Board System), Agent Execution Layer (execution environments with terminal sessions and log streams), Persistent Data Store, and Version-Controlled Repository with parallel working copies.

- FIG. 3 is a method flowchart for the process of provisioning a team of agents, from receiving team configuration through spawning execution environments, assigning identifiers, configuring output capture, writing state files, subscribing to the message board with appropriate receive modes, transmitting behavior prompts, and rendering the unified team interface.

- FIG. 4 is a method flowchart for the real-time inline status extraction cycle, including log stream scanning, escape sequence stripping, structured event marker pattern matching, false positive filtering, event type classification and deduplication, persistence, and push-based dashboard broadcast.

- FIG. 5 is a dual-operation flowchart showing the read operation (cursor retrieval, message query with self-exclusion, cursor advancement computed as the maximum of the current cursor, the highest delivered message identifier, and the subscriber's own highest posted message identifier, result delivery) and the post operation (message append with sequential identifier, no cursor side effects), with delivery guarantee summary.

- FIG. 6 is a state diagram showing the agent session lifecycle with Active, Sleeping, and Terminated states and transition conditions. Details preserved state on sleep (session record, behavior prompt, board subscription, read cursor, execution environment path) and restored state on wake (new terminal session, output capture, board subscription transfer with cursor preservation, behavior prompt replay, message catch-up).

- FIG. 7 is a sequence diagram illustrating the subscription transfer mechanism during session restart: normal operation, process interruption with message accumulation, restart with subscription transfer preserving the read cursor, and catch-up read delivering all messages posted during the suspension period.

- FIG. 8 is a diagram illustrating the self-contained behavior prompt construction showing multiple inputs — role designation, task scope, communication instructions, and collaboration protocol — merged into a single prompt that enables agent operation without system knowledge; the state file pre-configuration enabling simple CLI commands that resolve server address, session identifier, and project name automatically; and server-side cursor management enabling transparent handling of cursor retrieval, message query with self-filtering, and cursor advancement.

---

## DETAILED DESCRIPTION OF THE INVENTION

### System Architecture

The system operates as a server application running on a host machine. It comprises the following principal components:

#### 1. Agent Process Manager

The Agent Process Manager is responsible for the lifecycle of AI agent processes. Each agent runs as an independent process within a multiplexed terminal session. The system supports multiple agent types (e.g., different large language model backends) through a pluggable agent interface.

**Agent Spawning Process:**

When a new agent is requested (either individually or as part of a team), the system:

1. **Provisions an execution environment** associated with a version-controlled repository. In a preferred embodiment, this comprises creating or selecting a parallel working copy branched from a shared version-controlled repository. Each working copy is checked out to a different branch, providing file-system-level isolation — each agent can modify files without affecting other agents' working directories — while sharing the underlying repository, which is substantially more space-efficient than full clones. In an alternative embodiment, multiple agents may operate on a shared working copy, with an orchestrator agent mediating sequencing to prevent conflicts.

2. **Assigns a unique session identifier** that serves as the canonical reference for the agent across all system components — the terminal session, log output, persistent records, and agent process arguments.

3. **Creates a uniquely identified terminal session** for the agent, rooted in the agent's working directory. The session is associated with both the agent type and session identifier, enabling the agent discovery system to determine agent type and identity.

4. **Configures output capture** by redirecting all terminal output from the agent's session to a persistent log file. This log file becomes the real-time data source for the inline status extraction engine.

5. **Pre-configures communication credentials** by writing a state file containing the project name, role, and session identifier. This allows the agent to immediately communicate on its assigned message board without additional setup.

6. **Launches the agent process** with a merged configuration that combines global, project-level, and local settings with injected monitoring hooks for real-time event reporting. User-defined hooks are preserved alongside the system's monitoring hooks via deep merging. The launch command includes the protocol specification, the session identifier, the behavior prompt, and any user-specified flags.

7. **Registers the session** in the persistent data store with complete state — including agent type, execution environment path, display name, behavior prompt, and board assignment — enabling session recovery after system restarts.

8. **Subscribes to the team message board** (if the agent is part of a team), choosing a message filtering mode based on the agent's role: leadership roles are notified of all messages; worker roles are notified only of messages that mention them.

**Agent Type Abstraction:**

Each supported agent type implements a common interface for constructing launch and resume commands. This abstraction allows the system to support heterogeneous teams where different agents use different underlying AI models.

#### 2. Inline Status Extraction Protocol — Real-Time Agent Monitoring

The inline status extraction protocol is a mechanism for extracting structured status information from AI agent output without requiring the agent to use a structured API or output format.

**Protocol Format:**

Agents emit structured event markers using a delimiter-bounded inline format. The delimiters are chosen to be distinctive sequences unlikely to appear in natural language or code output, minimizing false positive matches. The protocol defines three event types:

- **STATUS** — The agent's current task (e.g., "Implementing auth middleware", "Running test suite"). Agents emit this before and after each subtask.

- **SUMMARY** — The agent's high-level goal (e.g., "Refactoring the database layer to use the repository pattern"). Emitted once at the start of work or when the goal changes significantly.

- **CONFIDENCE** — The agent's certainty about its current approach, comprising a level indicator and a specific reason. Low confidence flags uncertainty requiring human review; high confidence indicates non-obvious confidence with evidence.

**Detection Mechanism:**

The system incrementally scans each agent's output stream, extracting structured event markers that match the protocol format. The scanning pipeline:

1. **Incremental Scanning**: Tracks a read position for each agent's output stream. On each scan cycle, only newly appended content is processed, ensuring efficient operation. When the output stream is reset (e.g., after a process restart), the read position resets automatically.

2. **False Positive Filtering**: Filters out instruction echoes — instances where the agent reproduces the protocol documentation in its output rather than emitting actual events.

3. **Deduplication and Broadcast**: For STATUS and SUMMARY events, tracks the last known value per agent and only pushes changed values to connected dashboard clients, reducing network traffic.

4. **Persistent Storage**: Records all detected events in the persistent data store with timestamps and session identifiers, enabling historical activity timeline reconstruction and full-text search.

**Idle Detection Integration:**

The idle detector operates as a background service polling at configurable intervals. For each live agent, it queries the most recent event type from the persistent data store. When an agent's latest event indicates the agent is waiting for user input and the log file's modification timestamp indicates staleness exceeding a configurable threshold, the system:

1. Creates a notification delivery record containing the agent name, session ID, event type, and a human-readable summary including the waiting duration.
2. Dispatches the notification to all configured external endpoints matching the agent (or all agents if no filter is set).
3. Records the notification in an internal tracking set to prevent duplicate alerts for the same waiting period. The set is cleared when the agent leaves the idle state.
4. Suspended (sleeping) sessions are explicitly excluded from idle detection, as they are intentionally inactive.

**Key Technical Advantage:**

The inline status extraction protocol operates at the terminal output level, not the API level. This means it can monitor any command-line-based AI agent — including future agents not yet developed — without requiring integration with the agent's internal API. The agent simply prints formatted strings to standard output; the system extracts them from the log stream. This is fundamentally different from API-level monitoring which requires per-agent integration. The protocol specification is transmitted to the agent process as part of a behavior prompt, causing the agent to emit the structured event markers as inline text within its natural language output, requiring no modification to the agent's underlying model, runtime, or tool-use framework.

#### 3. Message Board System — Cursor-Based Inter-Agent Communication

The Message Board System provides asynchronous communication between AI agents operating in a team. It exposes both a programmatic interface and a command-line interface accessible from within agent terminal sessions.

**Data Model:**

The system maintains three data collections: **subscribers** (recording each agent's board subscription and read position), **messages** (storing an ordered sequence of communications scoped to a project), and **groups** (enabling sub-group-based message filtering within a board).

**Cursor-Based Read Mechanism:**

Each subscriber maintains an independent read cursor — a position marker indicating the last message consumed by that agent. The cursor mechanism operates as follows:

- On each **read** request, the system returns all messages posted after the subscriber's cursor position, excluding the requesting agent's own messages. The cursor is then advanced by computing a cursor advancement value as the maximum of: (i) the current cursor value, (ii) the highest message identifier among the delivered messages, and (iii) the highest message identifier posted by the requesting agent itself. This cursor advancement algorithm prevents the requesting agent's own interleaved posts from blocking cursor progression, ensuring deadlock-free consumption in high-frequency posting scenarios.

- On each **post** request, a new message is appended to the sequence. No subscriber's cursor is advanced by a post — cursors advance only on read.

This mechanism provides the following guarantees:
- **Exactly-once delivery**: Each message is delivered to each subscriber at most once.
- **No message loss**: Messages posted while an agent is not reading accumulate and are delivered on the next read.
- **Independent pacing**: Each subscriber reads at its own rate without affecting others.
- **Self-message filtering**: Agents never receive their own posts.
- **Restart resilience**: The cursor is persisted and survives process restarts.

**Configurable Message Filtering:**

The system supports configurable filtering modes per subscriber, controlling which messages are counted as unread for notification purposes. Modes include: counting all messages, counting only messages that mention the subscriber by name or role, counting only messages from designated group members, or suppressing unread counts entirely. The read operation delivers all messages regardless of the configured mode, ensuring agents have access to the full communication history. The default mode is assigned based on the agent's role within the team.

**Command-Line Interface:**

Agents interact with the message board via a dedicated command-line interface available within their terminal session. The interface supports subscribing, reading, posting, listing subscribers, and unsubscribing. Connection parameters are pre-configured via the state file written during agent provisioning, so the agent does not need to know server addresses, communication protocols, or cursor state. The command-line interface abstracts away connection parameters including server address, session identifier, and project name by resolving them from the pre-written state file, and further abstracts away cursor management by performing read position tracking and advancement server-side, such that agents interact with the message board using simple read and post commands without knowledge of the underlying communication protocol, cursor state, or network configuration.

**Subscription Transfer on Restart:**

When an agent session is restarted with a new identifier, the system transfers the existing subscription to the new identifier while preserving the read cursor position at its pre-restart value. This ensures the restarted agent receives messages posted during its downtime without re-reading previously delivered messages and without requiring re-subscription by external coordination.

#### 4. Session Persistence and Team State Management

The system persists agent session state in a local persistent data store configured for concurrent read/write access.

**Session Record:**

Each live session record captures the complete state needed to restore the agent: a unique session identifier, the execution environment path, the agent type, the behavior prompt defining the agent's role, the message board assignment (if any), a human-readable display name, and a flag indicating whether the session is suspended.

**Suspend/Restore Operations:**

The suspend (sleep) operation:
1. Terminates the agent process and marks the session as suspended in the persistent data store.
2. Removes the agent from live discovery so it no longer appears as an active agent.
3. Preserves the complete session record, including the message board subscription and cursor position.

The restore (wake) operation:
1. Creates a new terminal session in the stored execution environment.
2. Configures output capture for the inline status extraction protocol.
3. Launches a new agent process, optionally resuming the prior conversation (for agents that support conversation continuity).
4. Transfers the board subscription to the new session identifier, preserving the read cursor position so the agent receives messages posted during suspension.
5. Re-sends the stored behavior prompt to re-establish the agent's role context.
6. Marks the session as active in the persistent data store.

**Team-Level Operations:**

Teams (groups of agents on the same board) support collective operations:
- **Sleep All**: Sleeps all agents in the team.
- **Wake All**: Wakes all agents in the team with prompt replay and board re-subscription.
- **Kill All**: Terminates all agent processes in the team.
- **Add Agent**: Dynamically spawns a new agent and joins it to the existing team's board.

#### 5. Web Dashboard — Real-Time Monitoring and Control

The web dashboard is implemented as a web application serving dynamically-rendered templates with client-side scripting for real-time updates via push-based connections.

**Dashboard Architecture:**

- **Backend**: A web server with template rendering, persistent data store access, and real-time push connection broadcast.
- **Frontend**: Client-side scripting with modular components for rendering, controls, and activity state display.
- **Real-time updates**: Push-based connections deliver agent status changes, new events, and session state updates to all connected clients without polling.

**Sidebar Organization:**

The sidebar displays agents in a hierarchical structure:

- **Team-first mode** (default): Agent teams appear as top-level collapsible cards with accent-colored left borders. Each team card shows the team name, member count, and collective controls. Individual agents within the team show status dots, folder paths, and goal summaries. Standalone agents (not on a team) are grouped by folder below the teams.

- **Folder-first mode** (toggle): Agents are grouped by working directory. Within each folder, agents on the same board are shown as nested team sub-groups.

The user can toggle between these modes via a workspace settings menu, with the preference stored in client-side local storage.

**Agent Interaction:**

The dashboard provides a command input for each selected agent. Commands are delivered to the agent's terminal session via the terminal multiplexer's key-sending mechanism, using bracket paste mode for safe multi-line text delivery. The bracket paste sequences ensure that pasted text is treated as literal content rather than being interpreted as terminal commands.

---

## DETAILED EXAMPLE OF OPERATION — EXAMPLE 1

### Cross-Domain Multi-Agent Collaboration with Dynamic Team Formation

The following example describes a production session demonstrating the system's capabilities, including dynamic team composition, hierarchical agent coordination, and cross-domain collaboration.

#### Session Overview

A human operator used the system to coordinate a team of AI agents across legal, business, engineering, design, security, QA, and documentation domains. The session spanned approximately two days, during which agents exchanged over 280 messages on a shared message board and produced over 15 deliverables.

#### Dynamic Team Formation

The session began with five agents (Orchestrator, UI/UX Designer, Frontend Developer, QA Engineer, Lead Developer) and expanded dynamically as the project scope grew. A Legal Advisor was added to handle intellectual property strategy. A Documentation Expert joined to collaborate on technical writing. A Business Development agent was added for pricing and go-to-market planning. A Security Reviewer was added before the public launch to audit the codebase. Each new agent was spawned into its own isolated execution environment, subscribed to the existing message board, and given a behavior prompt defining its role — all without disrupting the agents already running.

#### Operator-Orchestrator-Specialist Hierarchy

The session demonstrated a three-layer coordination pattern:

**Layer 1 — Operator (Human):** Provided strategic direction, feedback on deliverables, and go/no-go decisions. The operator communicated through the dashboard, sending instructions that the Orchestrator interpreted and distributed.

**Layer 2 — Orchestrator (AI Agent):** Served as the coordination hub. The Orchestrator decomposed operator requests into specific assignments for specialist agents, tracked progress across all agents, coordinated when multiple agents needed to work on the same deliverable, and recommended adding new agents when the scope expanded. The Orchestrator was configured with a receive mode that counted all messages from all team members as unread, giving it full visibility into team communications.

**Layer 3 — Specialist Agents:** Each specialist operated autonomously within its domain, communicating through the shared message board. Specialists were configured with a receive mode that counted only messages directed to them (via mention patterns) as unread, reducing notification noise while ensuring they were alerted to relevant assignments and feedback.

#### Cross-Domain Collaboration

The session produced deliverables requiring collaboration across multiple domains:

**Technical compliance document:** The Legal Advisor drafted the document's legal structure and requirements. The Documentation Expert read the source code, wrote technical specifications, and posted them for legal review. The Lead Developer reviewed the final document for technical accuracy. Three rounds of refinement were coordinated through the message board, with each agent handling their area of expertise.

**Landing page:** Business Development produced a pricing analysis. The Orchestrator assigned the Frontend Developer to build the page. The UI/UX Designer reviewed the draft and produced prioritized improvements. The QA Engineer independently verified fixes and flagged a critical bug. The cycle repeated for each new section.

**Security hardening:** The Security Reviewer performed a comprehensive audit and posted findings with severity ratings. The Orchestrator triaged findings. The Lead Developer and Frontend Developer split the fixes. The QA Engineer verified each fix. The Security Reviewer confirmed the fixes addressed the vulnerabilities.

#### System Capabilities Demonstrated

This session exercised every major system capability:
- **Team formation with role-based behavior prompts** — each agent adopted and maintained its assigned expertise
- **Dynamic agent addition** — four agents were added mid-session without disrupting running agents
- **Cursor-based message delivery** — 280+ messages delivered with exactly-once semantics across all agents
- **Configurable receive modes** — the Orchestrator was notified of all messages; specialists were notified only of directed messages
- **Real-time status monitoring** — the dashboard showed the status of all nine agents throughout the session
- **Cross-domain coordination** — agents from legal, engineering, design, business, security, and documentation domains collaborated on shared deliverables through the message board

---

## DETAILED EXAMPLE OF OPERATION — EXAMPLE 2

### Large-Scale Shared-Codebase Coordination with Task Claiming and Conflict Resolution

The following example describes a production session demonstrating the system's capabilities in a shared-filesystem configuration without isolated execution environments, including hierarchical coordination at scale, task-based assignment via the message board, real-time conflict arbitration, progressive task discovery, and late-joining agent integration.

#### Session Overview

A human operator used the system to coordinate a team of ten AI agents tasked with simplifying and improving a shared codebase. All agents operated on the same working directory without git worktree isolation. The session produced over 140 messages on the shared message board, completed over 25 tasks, fixed 5 security vulnerabilities, and improved over 500 lines of code in approximately 25 minutes of active work.

#### Team Composition and Hierarchy

The team comprised ten agents, all using the same underlying AI model (demonstrating homogeneous team support): an Orchestrator, a Go Expert, a QA Engineer, a Frontend Developer, a Security Reviewer, a UI Engineer, a Windows App Expert, a Documentation Expert, a Performance Analyst, and a secondary Security Reviewer. The Orchestrator was configured with a receive mode counting all messages as unread; specialist agents were configured with mention-only filtering.

#### Task-Based Assignment via Message Board

The Orchestrator decomposed the operator's directive ("simplify the codebase") into an initial set of 7 analysis tasks and posted a numbered task backlog to the message board. Specialist agents claimed tasks by posting messages such as "Claiming Task #3" to the board. The Orchestrator tracked claims, applied first-come-first-served assignment rules, and redirected displaced agents to alternative tasks when conflicts arose.

As analysis tasks completed and agents reported findings, the Orchestrator generated additional implementation tasks. The task backlog grew dynamically across two rounds: Round 1 produced 25 implementation tasks from the initial analysis, and Round 2 produced 11 additional tasks from further analysis findings. This progressive task discovery pattern — where initial analysis generates findings that generate implementation tasks that generate further analysis — demonstrates the system's support for multi-round decomposition.

#### Conflict Resolution in Shared Filesystem

Operating without git worktree isolation, multiple agents made concurrent edits to the same codebase. Five task-claim conflicts occurred (involving Tasks #4, #17, #21, #29, and #30), and multiple agents flagged build conflicts from concurrent file modifications. The Orchestrator resolved each conflict within one to two messages by applying consistent rules: first-come-first-served for task claims, and sequencing directives for file modification conflicts (instructing agents to hold until dependencies landed). All builds passed with zero regressions despite the absence of filesystem isolation, demonstrating that the system functions effectively both with and without isolated execution environments.

#### Late-Joining Agent with Full History Access

The Go Expert agent joined the session approximately 40 minutes after the session began, after over 100 messages had already been exchanged. Upon joining, the Go Expert was granted access to the full message history of the team's message board through the cursor-based delivery mechanism. The agent retrieved prior messages encompassing all decisions, completed work, and ongoing tasks without requiring existing agents to pause or provide a briefing. The Go Expert was immediately productive, claiming two analysis assignments and delivering findings within minutes — demonstrating near-instant onboarding through the persistent communication record.

#### Secondary Reviewer Pattern

The Orchestrator designated standby agents (UI Engineer, Windows App Expert, and the secondary Security Reviewer) as secondary reviewers to validate findings from primary agents. The secondary Security Reviewer confirmed an auth bypass vulnerability identified by the primary Security Reviewer via the message board, providing peer validation without the Orchestrator performing domain-specific review.

#### System Capabilities Demonstrated

This session exercised system capabilities including:
- **Shared-filesystem operation** — 10 agents on a shared codebase without worktree isolation, with orchestrator-mediated conflict resolution
- **Task-based assignment via message board** — numbered task backlog, agent claiming, conflict resolution
- **Progressive task discovery** — multi-round decomposition from analysis to implementation
- **Real-time conflict arbitration** — 5 claim conflicts resolved by the Orchestrator in real time
- **Late-joining agent integration** — full message history access enabling immediate productivity
- **Secondary reviewer pattern** — peer validation via designated reviewers
- **Cursor-based message delivery** — 140+ messages delivered across 10+ agents with zero message loss
- **Hierarchical coordination at scale** — Orchestrator managing 10 specialist agents simultaneously
- **Homogeneous team support** — all agents using the same underlying AI model

---

## CLAIMS

### Independent Claim 1 — Dynamic AI Agent Team Formation

1. A computer-implemented method for orchestrating a plurality of artificial intelligence agents for collaborative task execution, the method comprising:

   (a) receiving, via a user interface, user input defining a team configuration comprising a team identifier, a plurality of agent definitions each having a role designation and a behavior prompt;

   (b) for each agent definition, provisioning an execution environment associated with a version-controlled repository, and spawning an agent process within a terminal multiplexer session attached to said execution environment;

   (c) automatically subscribing each spawned agent to a shared message board identified by the team identifier, wherein the message board maintains an independent read cursor for each subscribed agent;

   (d) transmitting the behavior prompt to each agent process, causing each agent to adopt its assigned role; and

   (e) rendering, in the user interface, a unified team representation comprising a collapsible group element displaying the plurality of agents with collective control operations including sleep, wake, and terminate.

### Dependent Claims on Claim 1

2. The method of claim 1, wherein provisioning the execution environment further comprises configuring the terminal multiplexer session to pipe agent output to a log stream, and parsing said log stream in real time to extract structured status events using a predefined protocol format.

3. The method of claim 1, further comprising dynamically adding a new agent to an existing team by spawning a new agent process, subscribing it to the existing message board, and updating the unified team representation without disrupting running agents.

4. The method of claim 1, wherein the collective sleep operation suspends all agent processes in the team while preserving each agent's board subscription, read cursor position, behavior prompt, and execution environment assignment in persistent storage.

5. The method of claim 4, further comprising a collective wake operation that restores each agent process, re-subscribes each agent to the message board at its preserved cursor position, and re-transmits the stored behavior prompt.

6. The method of claim 1, wherein the plurality of agents comprises agents of heterogeneous types, each type corresponding to a different underlying AI model, and wherein each agent type implements a common interface for command construction.

7. The method of claim 1, wherein each agent's terminal session is associated with a unique session identifier, ensuring uniqueness across system restarts and enabling the system to discover and correlate agent processes with their persistent session records.

8. The method of claim 1, wherein provisioning the execution environment comprises creating or selecting an isolated version-controlled working copy branched from a shared repository, such that each agent modifies files within its own working copy without affecting other agents' working directories, providing file-system-level isolation while sharing the underlying repository for space efficiency.

9. The method of claim 1, wherein provisioning the execution environment comprises assigning multiple agents to a shared working copy of the version-controlled repository, with an orchestrator agent mediating sequencing of file modifications to prevent conflicts between concurrently operating agents.

### Independent Claim 2 — Real-Time Agent Status Extraction via Inline Protocol

10. A computer-implemented method for monitoring the state of an artificial intelligence agent executing in a terminal environment, the method comprising:

   (a) configuring a terminal multiplexer to capture output of an agent process into a log stream by redirecting terminal output to a persistent file;

   (b) maintaining a read position marker for the log stream, tracking the position of the last scan;

   (c) on each scan cycle, reading only content appended to the log stream since the stored read position, stripping terminal escape sequences to obtain clean text;

   (d) applying a pattern match to the clean text to detect structured event markers conforming to a protocol format comprising predefined delimiter sequences, an event type identifier from a predefined set, and a payload string;

   (e) upon detecting an event marker of type STATUS, updating a real-time status indicator in a dashboard to display the payload as the agent's current task description;

   (f) upon detecting an event marker of type SUMMARY, updating a goal indicator in the dashboard to display the payload as the agent's high-level objective;

   (g) upon detecting an event marker of type CONFIDENCE, associating a confidence level and reason with the agent's current state, and rendering a visual confidence indicator in the dashboard; and

   (h) broadcasting detected events to connected dashboard clients via a real-time push connection, with deduplication such that STATUS and SUMMARY events are only transmitted when the payload value has changed.

### Dependent Claims on Claim 10

11. The method of claim 10, wherein the structured event markers are embedded inline within natural language agent output, and the pattern matching uses a compiled text pattern that identifies the delimiter sequences without requiring the agent to use a structured output format or API.

12. The method of claim 10, further comprising persisting detected events to a persistent data store with timestamps, enabling historical activity timeline reconstruction and full-text search across agent status history.

13. The method of claim 10, further comprising computing an activity metric based on the recency of log file modification, and upon determining that the agent has been idle beyond a configurable threshold, triggering an automated notification action comprising creating a webhook delivery record containing the agent identifier, event type, and a human-readable summary, and dispatching the webhook to one or more configured external endpoint URLs, wherein suspended agent sessions are excluded from idle detection.

14. The method of claim 10, wherein upon detecting that the log stream has been truncated, the method resets the read position marker to handle agent process restarts.

### Independent Claim 3 — Cursor-Based Inter-Agent Communication

15. A computer-implemented method for facilitating asynchronous communication between a plurality of artificial intelligence agent processes executing in execution environments, the method comprising:

   (a) maintaining a message board data structure comprising an ordered sequence of messages stored in a persistent data store, each message having a unique monotonically increasing integer identifier, a sender identifier corresponding to an agent session, a timestamp, and a content payload;

   (b) maintaining, for each subscribed agent process, an independent read cursor stored as an integer field in a subscriber record in the persistent data store, the cursor indicating the last message identifier consumed by that agent;

   (c) in response to a read request from a first agent process, querying for all messages having identifiers greater than the first agent's read cursor where the sender is not the first agent, returning the results, and advancing the cursor by computing a cursor advancement value as the maximum of: (i) the current cursor value, (ii) the highest message identifier among the delivered messages, and (iii) the highest message identifier posted by the requesting agent itself, thereby preventing the requesting agent's own interleaved posts from blocking cursor progression and ensuring deadlock-free consumption in high-frequency posting scenarios;

   (d) in response to a post request from a second agent process, appending a new message to the ordered sequence with the next sequential identifier, without advancing any agent's read cursor;

   (e) providing a command-line interface pre-configured for each agent process via a state file written to the agent's execution environment prior to agent launch, the state file containing connection parameters including a project identifier, a role designation, and a session identifier, such that the agent process communicates on the message board using simple read and post commands without knowledge of server addresses, communication protocols, or cursor state; and

   (f) upon agent process restart with a new session identifier, transferring the existing subscription record to the new session identifier while preserving the read cursor position at its pre-restart value, enabling the restarted agent process to receive all messages posted during its downtime without re-reading previously delivered messages and without requiring re-subscription by external coordination.

### Dependent Claims on Claim 15

16. The method of claim 15, further comprising configurable receive modes per subscriber, wherein the receive mode determines which messages are counted as unread for notification purposes: all messages, only messages containing mention patterns matching the subscriber's identifier or role, messages from specified group members, or no messages, while the read operation delivers all messages regardless of receive mode.

17. The method of claim 16, wherein the mention pattern matching comprises case-insensitive string matching against patterns including a broadcast keyword, the subscriber's session identifier, and the subscriber's role title.

18. The method of claim 15, further comprising a programmatic interface providing equivalent operations to the command-line interface, enabling access from external systems and dashboard clients.

19. The method of claim 15, wherein multiple message boards coexist, each scoped to a project identifier, and agents may be subscribed to at most one board at a time, with subscription transfer preserving cross-board state.

### Independent Claim 4 — Agent Session Persistence with Team State Replay

20. A computer-implemented method for persisting and restoring artificial intelligence agent sessions within a team context, the method comprising:

   (a) storing, for each agent session in a persistent data store record, a session state comprising: an agent type identifier specifying the AI model backend, an execution environment path corresponding to a version-controlled working copy, a behavior prompt defining the agent's role, a message board subscription identifier linking the agent to a team communication channel, and a session state flag;

   (b) in response to a sleep command targeting one or more agents, transitioning each targeted session's state flag to suspended, sending an interrupt signal to each agent process via the terminal multiplexer, and preserving the complete session record including message board cursor position in the persistent data store;

   (c) in response to a wake command targeting one or more previously suspended agents, for each targeted agent:
      (i) spawning a new agent process in the stored execution environment via the terminal multiplexer,
      (ii) re-subscribing the agent to its stored message board with cursor position preserved at the pre-sleep value,
      (iii) re-transmitting the stored behavior prompt to the new agent process to re-establish role context, and
      (iv) transitioning the session state flag to active;

   (d) upon wake, making available to each restored agent all messages posted to its message board during the suspension period, delivered through the cursor-based read mechanism without duplicate delivery; and

   (e) reflecting the restored session state in a dashboard with preserved team grouping, hierarchical position, status indicators, and collective control operations.

### Dependent Claims on Claim 20

21. The method of claim 20, wherein the sleep and wake commands may target individual agents or entire teams, and wherein team-level operations iterate over all agents sharing the same message board subscription.

22. The method of claim 20, further comprising a session history system that records completed sessions with full-text search indexing, enabling operators to search past agent activity across all sessions.

### Independent Claim 5 — Hierarchical Multi-Agent Coordination via Differentiated Notification

23. A computer-implemented method for coordinating a plurality of artificial intelligence agents in a hierarchical team structure, the method comprising:

   (a) spawning a plurality of agent processes in execution environments, each agent process configured with a behavior prompt defining a domain specialization;

   (b) subscribing all agent processes to a shared message board maintaining an ordered sequence of messages with independent read cursors per subscriber;

   (c) designating at least one agent process as an orchestrator agent and configuring the orchestrator agent with a first message filtering mode that counts all messages from all subscribers as unread, providing the orchestrator agent with full visibility into team communications;

   (d) designating the remaining agent processes as specialist agents and configuring each specialist agent with a second message filtering mode that counts only messages containing mention patterns matching the specialist agent's identifier or role as unread, reducing notification volume while ensuring delivery of directed assignments;

   (e) receiving, from a human operator via a user interface, a task directive, and delivering the task directive to the orchestrator agent;

   (f) wherein the orchestrator agent decomposes the task directive into domain-specific sub-tasks and distributes each sub-task to an appropriate specialist agent via the message board, each sub-task accompanied by context synthesized from prior team communications; and

   (g) wherein the read operation on the message board delivers all messages to all agents regardless of filtering mode, ensuring each agent has access to the full communication history while being notified only of relevant messages.

### Dependent Claims on Claim 23

24. The method of claim 23, wherein the specialist agents collaborate on a shared deliverable by posting contributions and reviews to the message board, each specialist agent applying its domain-specific expertise as defined by its behavior prompt, and wherein the orchestrator agent coordinates multi-round refinement cycles across specialist agents.

25. The method of claim 23, wherein a human operator communicates with the orchestrator agent via the user interface, and the orchestrator agent coordinates the specialist agents, thereby establishing a three-layer hierarchy comprising the human operator, the orchestrator agent, and the specialist agents.

26. The method of claim 25, wherein the orchestrator agent receives instructions from the operator via the user interface, decomposes said instructions into sub-tasks, and distributes the sub-tasks to specialist agents via the message board, each sub-task accompanied by context derived from the orchestrator agent's synthesis of prior team communications.

27. The method of claim 23, wherein the orchestrator agent posts a numbered task backlog to the message board, and wherein specialist agents claim individual tasks by posting claim messages to the message board referencing task numbers, the orchestrator agent tracking task claims and resolving conflicting claims wherein multiple specialist agents claim the same task by applying an assignment rule and redirecting displaced agents to alternative unclaimed tasks via the message board.

28. The method of claim 23, wherein the orchestrator agent designates one or more specialist agents as secondary reviewers, and wherein the secondary reviewers validate findings or work product posted by a primary specialist agent by reviewing the primary agent's message board posts and posting a confirmation or correction, thereby providing multi-agent peer validation of work product without requiring the orchestrator agent to perform domain-specific review.

29. The method of claim 23, wherein the orchestrator agent performs progressive task discovery comprising: (i) distributing an initial set of analysis tasks to specialist agents, (ii) receiving findings from the specialist agents via the message board, (iii) generating additional implementation tasks based on the received findings, and (iv) distributing the additional tasks to specialist agents via the message board, such that the task backlog grows dynamically across multiple rounds of analysis and implementation.

30. The method of claim 23, wherein the orchestrator agent performs real-time conflict arbitration comprising: detecting, from message board posts, that two or more specialist agents have claimed the same task or are modifying overlapping resources, and posting a resolution directive to the message board that reassigns at least one specialist agent to a non-conflicting task or instructs the specialist agents to sequence their modifications, thereby preventing concurrent conflicting modifications to shared resources.

### Additional Dependent Claims

31. The method of claim 1, wherein the plurality of agents comprises agents with distinct domain specializations defined by their respective behavior prompts, and wherein the agents collaborate on a shared deliverable by posting contributions and reviews to the message board, each agent applying its domain-specific expertise to the shared work product.

32. The method of claim 16, wherein the read operation accepts an optional filtering parameter that, when set, causes the read operation to return only messages containing mention patterns directed at the requesting agent, enabling agents to selectively retrieve only messages addressed to them while still advancing the read cursor past all intervening messages to maintain cursor consistency.

33. The method of claim 15, wherein, upon dynamically adding a new agent to an existing team, the newly added agent is granted access to the full message history of the team's message board, enabling the new agent to retrieve prior messages encompassing decisions, completed work, and ongoing tasks without requiring existing agents to pause or provide a briefing, thereby achieving near-instant onboarding through the persistent communication record.

34. The method of claim 1, wherein the behavior prompt transmitted to each agent comprises a self-contained instruction set that encapsulates the agent's role identity, task scope, and collaboration instructions, such that the agent can operate in its assigned role without requiring knowledge of the underlying orchestration system, other agents' configurations, or communication infrastructure, thereby enabling agents with no prior system knowledge to immediately participate in coordinated team work upon receiving the behavior prompt.

35. The method of claim 15, wherein the command-line interface abstracts away connection parameters including server address, session identifier, and project name by resolving them from the pre-written state file, and further abstracts away cursor management by performing read position tracking and advancement server-side, such that agents interact with the message board using simple read and post commands without knowledge of the underlying communication protocol, cursor state, or network configuration, thereby preventing communication errors and ensuring reliable inter-agent communication.

### Independent Claim 6 — Agent-Agnostic Terminal Monitoring via Inline Protocol

36. A computer-implemented method for monitoring the operational state of any command-line-based artificial intelligence agent without requiring integration with the agent's internal API, the method comprising:

   (a) injecting, into an agent process's launch configuration, a protocol specification defining a set of structured event marker formats, each format comprising a distinctive delimiter sequence selected to avoid collision with natural language and code output, an event type identifier from a predefined set, and a payload field;

   (b) capturing the agent process's terminal output into a log stream via a terminal multiplexer output redirection, independently of the agent's internal architecture or API;

   (c) incrementally scanning the log stream by maintaining a read position marker and processing only content appended since the last scan cycle;

   (d) applying pattern matching to the scanned content to detect structured event markers conforming to the protocol specification, and filtering out false positives caused by instruction echoes wherein the agent reproduces protocol documentation rather than emitting actual events;

   (e) extracting, from detected event markers, real-time operational state comprising the agent's current task, high-level goal, and confidence assessment; and

   (f) rendering the extracted operational state in a monitoring interface accessible to a human operator;

   wherein the method operates at the terminal output level such that any command-line-based AI agent that emits the structured event markers is monitorable without per-agent API integration, including agent types not yet developed at the time of system deployment.

### Dependent Claims on Claim 36

37. The method of claim 36, wherein the protocol specification is transmitted to the agent process as part of a behavior prompt, causing the agent to emit the structured event markers as inline text within its natural language output, requiring no modification to the agent's underlying model, runtime, or tool-use framework.

### Independent Claim 7 — System Claim

38. A system for orchestrating a plurality of artificial intelligence agents for collaborative task execution, the system comprising:

   a processor; and

   a non-transitory memory coupled to the processor and storing instructions that, when executed by the processor, cause the system to:

   (a) receive, via a user interface, user input defining a team configuration comprising a team identifier and a plurality of agent definitions each having a role designation and a behavior prompt;

   (b) for each agent definition, provision an execution environment associated with a version-controlled repository, and spawn an agent process within a terminal multiplexer session attached to said execution environment;

   (c) configure the terminal multiplexer session to capture output of each agent process into a log stream, and parse said log stream in real time to extract structured event markers conforming to an inline protocol format comprising predefined delimiter sequences, an event type identifier, and a payload string;

   (d) maintain a message board data structure for inter-agent communication, the message board comprising an ordered sequence of messages stored in a persistent data store, and maintaining for each subscribed agent an independent read cursor, wherein cursor advancement is computed as the maximum of the current cursor value, the highest delivered message identifier, and the agent's own highest posted message identifier;

   (e) transmit the behavior prompt to each agent process, causing each agent to adopt its assigned role;

   (f) store, for each agent session in the persistent data store, a session state comprising the agent type, execution environment path, behavior prompt, message board subscription, and a session state flag; and

   (g) render, in the user interface, a real-time monitoring display comprising agent status, goals, confidence levels, and team groupings derived from the extracted event markers, with controls for collective team operations including suspend, restore, and terminate.

### Dependent Claims on Claim 38

39. The system of claim 38, wherein the instructions further cause the system to, in response to a suspend command, terminate a targeted agent process and preserve the complete session state including message board cursor position, and in response to a restore command, spawn a new agent process in the stored execution environment, transfer the message board subscription to the new process with cursor position preserved, and re-transmit the stored behavior prompt.

40. The system of claim 38, wherein the plurality of agent processes comprise heterogeneous agent types corresponding to different underlying AI models, each implementing a common interface for command construction.

### Independent Claim 8 — Computer-Readable Medium Claim

41. A non-transitory computer-readable medium storing instructions that, when executed by a processor, cause the processor to perform a method for orchestrating a plurality of artificial intelligence agents for collaborative task execution, the method comprising:

   (a) receiving user input defining a team configuration comprising a team identifier and a plurality of agent definitions each having a role designation and a behavior prompt;

   (b) for each agent definition, provisioning an execution environment associated with a version-controlled repository, and spawning an agent process within a terminal multiplexer session attached to said execution environment;

   (c) configuring output capture for each agent process and parsing the captured output in real time to extract structured event markers conforming to an inline protocol format, the event markers comprising status indicators, goal summaries, and confidence assessments;

   (d) maintaining a cursor-based message board for asynchronous inter-agent communication, wherein each subscribed agent maintains an independent read cursor and cursor advancement prevents self-message blocking;

   (e) persisting session state for each agent process, the session state comprising the agent type, execution environment path, behavior prompt, and message board subscription with cursor position; and

   (f) supporting suspend and restore operations that preserve and restore complete agent state including message board subscriptions, cursor positions, and behavior prompts, enabling agents to resume collaborative work after interruption with access to messages posted during suspension.

### Dependent Claims on Claim 41

42. The non-transitory computer-readable medium of claim 41, wherein the method further comprises broadcasting extracted event markers to connected monitoring clients via real-time push connections, with deduplication such that status and goal events are transmitted only when the payload value has changed.

---

## ABSTRACT

A computer-implemented system and method for orchestrating multiple artificial intelligence agents operating in parallel on shared task-oriented workflows. The system provisions execution environments associated with a version-controlled repository and multiplexed terminal sessions, enabling concurrent work. In one embodiment, agents operate in isolated version-controlled working copies; in another, agents share a working copy with orchestrator-mediated sequencing. A real-time inline status extraction protocol parses structured event markers from agent terminal output to provide live monitoring of agent status, goals, and confidence levels without requiring agent API integration, enabling monitoring of any command-line-based AI agent including future agent types. A cursor-based message board system enables asynchronous inter-agent communication with independent read positions, a deadlock-free cursor advancement algorithm, configurable receive modes, and guaranteed delivery. Session persistence supports suspend/restore operations preserving full agent state including team membership, communication history, and behavior prompts. A hierarchical coordination pattern with differentiated notification enables orchestrator agents to manage specialist agents at scale with task decomposition, progressive discovery, and conflict arbitration.

---

## DRAWING DESCRIPTIONS

### FIG. 1 — Web Dashboard Screenshot
Screenshot of a preferred embodiment of the web dashboard showing the system in operation. The left sidebar displays a hierarchical team view with multiple AI agents organized under a team heading, each showing real-time status indicators, activity timestamps, and goal summaries. The center panel shows an agent's terminal session with inline status extraction protocol markers visible in the output stream. The right panel shows the inter-agent message board with cursor-based communication between team members.

### FIG. 2 — System Architecture Diagram
Block diagram showing the major system components: Operator Interface (Web Dashboard), Orchestration Engine (Agent Process Manager, Inline Status Extraction Engine, Session Persistence Layer, Message Board System), Agent Execution Layer (execution environments with terminal sessions and log streams), Persistent Data Store, and Version-Controlled Repository with parallel working copies.

### FIG. 3 — Agent Spawning Flowchart
Method flowchart for Claim 1: The process of provisioning a team of agents, from receiving team configuration through spawning execution environments, assigning identifiers, configuring output capture, writing state files, subscribing to the message board with appropriate receive modes, transmitting behavior prompts, and rendering the unified team interface.

### FIG. 4 — Inline Status Protocol Detection Flowchart
Method flowchart for Claim 10: The real-time inline status extraction cycle, including log stream scanning, escape sequence stripping, structured event marker pattern matching, false positive filtering, event type classification and deduplication, persistence, and push-based dashboard broadcast.

### FIG. 5 — Message Board Cursor Mechanism Flowchart
Dual-operation flowchart for Claim 15: The read operation (cursor retrieval, message query with self-exclusion, cursor advancement computed as the maximum of the current cursor, the highest delivered message identifier, and the subscriber's own highest posted message identifier, result delivery) and the post operation (message append with sequential identifier, no cursor side effects). Includes delivery guarantee summary.

### FIG. 6 — Session Sleep/Wake State Diagram
State diagram for Claim 20: Agent session lifecycle showing Active, Sleeping, and Terminated states with transition conditions. Details preserved state on sleep (session record, behavior prompt, board subscription, read cursor, execution environment path) and restored state on wake (new terminal session, output capture, board subscription transfer with cursor preservation, behavior prompt replay, message catch-up).

### FIG. 7 — Subscription Transfer Sequence Diagram
Sequence diagram illustrating the mechanism described in Claim 15(f), showing the subscription transfer during session restart: normal operation, process interruption with message accumulation, restart with subscription transfer preserving the read cursor, and catch-up read delivering all messages posted during the suspension period.

### FIG. 8 — Agent Communication Abstraction Layer
Diagram illustrating Claims 34 and 35: The self-contained behavior prompt construction (Claim 34) showing multiple inputs — role designation, task scope, communication instructions, and collaboration protocol — merged into a single prompt that enables agent operation without system knowledge. The state file pre-configuration (Claim 35) showing connection parameters written before agent launch, enabling simple CLI commands that resolve server address, session identifier, and project name automatically. Server-side cursor management (Claim 35) showing transparent handling of cursor retrieval, message query with self-filtering, and cursor advancement, such that agents interact via simple read and post commands without knowledge of the underlying protocol.

---

## PREFERRED EMBODIMENT NOTES

The following implementation-specific details are provided for completeness and describe a preferred embodiment. The claims are not limited to these specific technologies:

| Abstract Term | Preferred Embodiment |
|---|---|
| Terminal multiplexer / process multiplexer | tmux |
| Isolated version-controlled execution environment | Git worktrees |
| Parallel working copy | Git worktree branched from shared repository |
| Persistent data store | SQLite database with WAL mode |
| Web server | FastAPI (Python) |
| Real-time push connections | WebSocket protocol |
| Template rendering | Jinja2 |
| AI agent backends | Claude (Anthropic), Gemini (Google) |
| Client-side scripting | Vanilla JavaScript (ES modules) |
| Output capture mechanism | tmux pipe-pane |
| Command delivery mechanism | tmux send-keys with bracket paste mode |
| Communication state file | JSON file on local filesystem |
| Inline protocol delimiter format | `||PULSE:<EVENT_TYPE> <payload>||` (double-pipe delimiters) |
| Unique session identifier | UUID v4 encoded in terminal session name as `{agent_type}-{uuid}` |
| Cursor advancement algorithm | max(current_cursor, highest_delivered_id, own_highest_posted_id) |

These implementation choices are illustrative and not limiting. The methods and systems described herein may be implemented using any suitable combination of process multiplexing, version control, data persistence, web serving, and real-time communication technologies.

---

## FILING NOTES

### Priority Date
- First public disclosure: February 17, 2026 (GitHub repository made public)
- U.S. grace period deadline: February 17, 2027
- Recommended provisional filing: Immediately (every day of delay reduces strategic options)

### International Considerations
- Public disclosure on February 17, 2026 bars patent rights in most absolute novelty jurisdictions (EU, Japan, China, Korea)
- Some jurisdictions have grace period exceptions (Canada has a 1-year grace period similar to US; Australia has limited exceptions)
- PCT filing may still be possible for grace period jurisdictions if provisional is filed promptly
- Consult patent attorney on jurisdiction-by-jurisdiction analysis

### Inventor-Anthropic Relationship
- **CONFIRMED: Inventor has NO affiliation with Anthropic**
- Claude Code Agent Teams (February 5, 2026) is therefore prior art under 35 USC 102(a)(1)
- Claims 1 and 20 require combination defense: the integrated system goes beyond what Claude Code Agent Teams provides
- Claims 10, 15, 23, and 36 are unaffected — Claude Code Agent Teams does not teach inline status extraction, cursor-based messaging, hierarchical coordination with differentiated notification, or agent-agnostic terminal monitoring

### Prior Art to Distinguish
- Claude Code Agent Teams (Anthropic, Feb 5, 2026): Team formation with worktree isolation — lacks cursor-based messaging, inline status extraction, configurable receive modes, session sleep/wake with state replay, web dashboard
- Cursor IDE (Anysphere, Oct 29, 2025): Parallel agents with worktree isolation — lacks inter-agent messaging, status extraction, session persistence, standalone dashboard
- ccswarm (nwiizo, Jan 6, 2025): Multi-agent orchestration with worktrees and session persistence — lacks cursor-based messaging, inline status extraction, configurable filtering, sleep/wake with cursor preservation
- "Swarming the Codebase" (Helio Medeiros, Nov 2025): Orchestrating multiple Claude Code agents with worktrees — community blog post demonstrating concept
- TMAI (trust-delta, 2025): Terminal monitoring of AI agents — raw display only, no structured protocol extraction
- AutoGen (Microsoft, Sep 2023): API-level multi-agent framework — no process isolation, no terminal monitoring, no persistent sessions
- CrewAI (crewAI Inc., late 2023): Role-based agent framework — single process, no execution environment isolation, no inline extraction
- LangGraph (LangChain, Jan 2024): Graph-based orchestration — single process, state checkpointing differs from session persistence
- MetaGPT (DeepWisdom, Aug 2023): Software company simulation — API-level, single process
- ChatDev (Tsinghua, Jul 2023): Software company simulation — API-level, single process
- CAMEL (CAMEL-AI, Mar 2023): Communicative agents — API-level, single process
- US7945631B2 (Microsoft, filed 2008): Cursor-based message state — enterprise messaging, no AI agent features, no self-blocking prevention
- Apache Kafka (2011): Consumer group offsets — distributed streaming, no AI agent features
- Apache Pulsar (2016): Subscription cursors — distributed messaging, no AI agent features
- tmux/GNU Screen: Terminal multiplexing — no agent awareness

### Inventor(s)
Christopher Knorowski
9729 NW Randal Ln
Portland, OR 97229


---

**Document Statistics:**
- Total Claims: 42
- Independent Claims: 8 (Claims 1, 10, 15, 20, 23, 36, 38, 41)
- Dependent Claims: 34
- Claim Types: Method (6), System/Apparatus (1), Computer-Readable Medium (1)
- Figures: 8
- Detailed Examples of Operation: 2

