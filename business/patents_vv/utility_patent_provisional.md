# Provisional Patent Application

## System and Method for Multi-Agent AI Orchestration with Isolated Execution Environments and Collaborative Communication

**Disclaimer: This document is a draft template for informational purposes only. Consult with a qualified patent attorney before filing. Patent claims require professional review to ensure proper scope, format, and compliance with USPTO requirements.**

---

## CROSS-REFERENCE TO RELATED APPLICATIONS

This application claims priority to U.S. Provisional Patent Application No. [TO BE ASSIGNED], filed [FILING DATE].

## FIELD OF THE INVENTION

The present invention relates generally to artificial intelligence agent management systems, and more particularly to methods and systems for orchestrating multiple AI agents operating in parallel isolated execution environments with real-time monitoring and inter-agent communication for collaborative task execution.

## BACKGROUND OF THE INVENTION

### Technical Problem

Task-oriented workflows increasingly leverage AI assistants (e.g., large language models) to automate tasks such as code generation, research, content creation, project management, and analysis. However, current approaches typically involve a single AI agent operating in a single environment, which creates several technical limitations:

1. **Serialization bottleneck**: A single agent can only work on one task at a time. Complex projects requiring parallel workstreams (e.g., frontend and backend development, testing and implementation) cannot be efficiently distributed.

2. **Conflict risk**: Multiple agents operating on shared resources simultaneously may produce conflicting modifications, requiring manual resolution that negates the efficiency gains of automation.

3. **Lack of coordination**: When multiple agents are deployed, there is no standardized mechanism for them to communicate progress, share context, or coordinate work — leading to duplicated effort, incompatible implementations, or blocked dependencies.

4. **Monitoring opacity**: Operators have no real-time visibility into what multiple concurrent agents are doing, their confidence levels, or whether they need human intervention — requiring manual inspection of each agent's terminal session.

5. **State fragility**: Agent sessions are ephemeral. If an agent process is interrupted, all context about its assigned task, team membership, communication history, and progress is lost.

### Prior Art Limitations

Existing multi-agent frameworks (e.g., AutoGen, CrewAI, LangGraph) operate at the prompt/API level — they orchestrate LLM API calls within a single process. They do not address the problem of orchestrating independent, long-running agent processes that each have their own terminal session, file system access, and tool-use capabilities.

Existing terminal multiplexers provide process isolation but no agent-aware monitoring, communication, or state management.

Existing dashboards (e.g., CI/CD dashboards, project management tools) do not provide real-time monitoring of AI agent activity or support for agent-to-agent communication.

The present invention addresses these limitations by providing an integrated system for multi-agent orchestration that combines process isolation via parallel version-controlled working copies, real-time monitoring via an inline status protocol, cursor-based inter-agent communication, and persistent session state management.

## SUMMARY OF THE INVENTION

The present invention provides a computer-implemented system and method for orchestrating multiple artificial intelligence agents for collaborative task execution. The system comprises:

1. **Agent Orchestration Engine**: Spawns and manages AI agent processes in isolated version-controlled execution environments via a terminal multiplexer, enabling parallel task execution on shared resources without conflicts.

2. **Inline Status Extraction Protocol**: A mechanism that parses structured event markers (STATUS, SUMMARY, CONFIDENCE) from agent terminal output in real time, without requiring agents to use a structured API — enabling monitoring of any command-line-based AI agent.

3. **Message Board System**: A cursor-based inter-agent communication platform where each subscriber maintains an independent read position, enabling asynchronous team communication with guaranteed message delivery and no duplicate reads.

4. **Session Persistence Layer**: Stores agent configuration (type, execution environment, prompt, board subscription, read cursor) and supports sleep/wake operations that preserve and restore full team state, including re-subscribing agents to their communication channels and re-issuing behavior prompts.

5. **Web Dashboard**: A real-time monitoring interface with push-based connections displaying agent status, goals, confidence levels, team groupings, and activity timelines, with controls for sending commands, managing teams, and adjusting agent behavior.

## DETAILED DESCRIPTION OF THE INVENTION

### System Architecture

The system operates as a server application running on a host machine. It comprises the following principal components:

#### 1. Agent Process Manager

The Agent Process Manager is responsible for the lifecycle of AI agent processes. Each agent runs as an independent process within a multiplexed terminal session. The system supports multiple agent types (e.g., different large language model backends) through a pluggable agent interface.

**Agent Spawning Process:**

When a new agent is requested (either individually or as part of a team), the system:

1. **Provisions an isolated execution environment** by creating or selecting a parallel working copy branched from a shared version-controlled repository. Each working copy is checked out to a different branch, providing file-system-level isolation — each agent can modify files without affecting other agents' working directories — while sharing the underlying repository, which is significantly more space-efficient than full clones.

2. **Assigns a unique session identifier** that serves as the canonical reference for the agent across all system components — the terminal session, log output, persistent records, and agent process arguments.

3. **Creates a uniquely identified terminal session** for the agent, rooted in the agent's working directory. The session is associated with both the agent type and session identifier, enabling the agent discovery system to determine agent type and identity.

4. **Configures output capture** by redirecting all terminal output from the agent's session to a persistent log file. This log file becomes the real-time data source for the inline status extraction engine.

5. **Pre-configures communication credentials** by writing a state file containing the project name, role, and session identifier. This allows the agent to immediately communicate on its assigned message board without additional setup.

6. **Launches the agent process** with a merged configuration that combines global, project-level, and local settings with injected monitoring hooks for real-time event reporting. User-defined hooks are preserved alongside the system's monitoring hooks via deep merging. The launch command includes the protocol specification, the session identifier, the behavior prompt, and any user-specified flags.

7. **Registers the session** in the persistent data store with complete state — including agent type, execution environment path, display name, behavior prompt, and board assignment — enabling session recovery after system restarts.

8. **Subscribes to the team message board** (if the agent is part of a team), choosing a message filtering mode based on the agent's role: leadership roles receive all messages; worker roles receive only messages that mention them.

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

The inline status extraction protocol operates at the terminal output level, not the API level. This means it can monitor any command-line-based AI agent — including future agents not yet developed — without requiring integration with the agent's internal API. The agent simply prints formatted strings to standard output; the system extracts them from the log stream. This is fundamentally different from API-level monitoring which requires per-agent integration.

#### 3. Message Board System — Cursor-Based Inter-Agent Communication

The Message Board System provides asynchronous communication between AI agents operating in a team. It exposes both a programmatic interface and a command-line interface accessible from within agent terminal sessions.

**Data Model:**

The system maintains three data collections: **subscribers** (recording each agent's board subscription and read position), **messages** (storing an ordered sequence of communications scoped to a project), and **groups** (enabling sub-group-based message filtering within a board).

**Cursor-Based Read Mechanism:**

Each subscriber maintains an independent read cursor — a position marker indicating the last message consumed by that agent. The cursor mechanism operates as follows:

- On each **read** request, the system returns all messages posted after the subscriber's cursor position, excluding the requesting agent's own messages. The cursor is then advanced to reflect the delivered messages.

- The cursor advancement accounts for the agent's own posted messages, ensuring that an agent's own posts do not block cursor progression. This prevents a deadlock scenario where a frequently-posting agent would never see responses from others.

- On each **post** request, a new message is appended to the sequence. No subscriber's cursor is advanced by a post — cursors advance only on read.

This mechanism provides the following guarantees:
- **Exactly-once delivery**: Each message is delivered to each subscriber at most once.
- **No message loss**: Messages posted while an agent is not reading accumulate and are delivered on the next read.
- **Independent pacing**: Each subscriber reads at its own rate without affecting others.
- **Self-message filtering**: Agents never receive their own posts.
- **Restart resilience**: The cursor is persisted and survives process restarts.

**Configurable Message Filtering:**

The system supports configurable filtering modes per subscriber, controlling which messages are delivered on read. Modes include: receiving all messages, receiving only messages that mention the subscriber by name or role, receiving only messages from designated group members, or suppressing delivery entirely. The default mode is assigned based on the agent's role within the team.

**Command-Line Interface:**

Agents interact with the message board via a dedicated command-line interface available within their terminal session. The interface supports subscribing, reading, posting, listing subscribers, and unsubscribing. Connection parameters are pre-configured via the state file written during agent provisioning, so the agent does not need to know server addresses or session identifiers.

**Subscription Transfer on Restart:**

When an agent session is restarted with a new identifier, the system transfers the existing subscription to the new identifier while preserving the read cursor position. This ensures the restarted agent receives messages posted during its downtime without re-reading previously delivered messages.

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

## CLAIMS

### Independent Claim 1 — Dynamic AI Agent Team Formation

1. A computer-implemented method for orchestrating a plurality of artificial intelligence agents for collaborative task execution, the method comprising:

   (a) receiving, via a graphical user interface, user input defining a team configuration comprising a team identifier, a plurality of agent definitions each having a role designation and a behavior prompt;

   (b) for each agent definition, provisioning an isolated execution environment by creating or selecting an isolated version-controlled working copy branched from a shared repository, and spawning an agent process within a terminal multiplexer session attached to said working copy;

   (c) automatically subscribing each spawned agent to a shared message board identified by the team identifier, wherein the message board maintains an independent read cursor for each subscribed agent;

   (d) transmitting the behavior prompt to each agent process, causing each agent to adopt its assigned role; and

   (e) rendering, in the graphical user interface, a unified team representation comprising a collapsible group element displaying the plurality of agents with collective control operations including sleep, wake, and terminate.

### Dependent Claims (Claim 1)

2. The method of claim 1, wherein provisioning the isolated execution environment further comprises configuring the terminal multiplexer session to pipe agent output to a log stream, and parsing said log stream in real time to extract structured status events using a predefined protocol format.

3. The method of claim 1, further comprising dynamically adding a new agent to an existing team by spawning a new agent process, subscribing it to the existing message board, and updating the unified team representation without disrupting running agents.

4. The method of claim 1, wherein the collective sleep operation suspends all agent processes in the team while preserving each agent's board subscription, read cursor position, behavior prompt, and execution environment assignment in persistent storage.

5. The method of claim 4, further comprising a collective wake operation that restores each agent process, re-subscribes each agent to the message board at its preserved cursor position, and re-transmits the stored behavior prompt.

6. The method of claim 1, wherein the plurality of agents comprises agents of heterogeneous types, each type corresponding to a different underlying AI model, and wherein each agent type implements a common interface for command construction.

7. The method of claim 1, wherein each agent's terminal session is associated with a unique session identifier, ensuring uniqueness across system restarts and enabling the system to discover and correlate agent processes with their persistent session records.

### Independent Claim 2 — Real-Time Agent Status Extraction via Inline Protocol

8. A computer-implemented method for monitoring the state of an artificial intelligence agent executing in a terminal environment, the method comprising:

   (a) configuring a terminal multiplexer to capture output of an agent process into a log stream by redirecting terminal output to a persistent file;

   (b) maintaining a read position marker for the log stream, tracking the position of the last scan;

   (c) on each scan cycle, reading only content appended to the log stream since the stored read position, stripping terminal escape sequences to obtain clean text;

   (d) applying a pattern match to the clean text to detect structured event markers conforming to a protocol format comprising predefined delimiter sequences, an event type identifier from a predefined set, and a payload string;

   (e) upon detecting an event marker of type STATUS, updating a real-time status indicator in a graphical dashboard to display the payload as the agent's current task description;

   (f) upon detecting an event marker of type SUMMARY, updating a goal indicator in the graphical dashboard to display the payload as the agent's high-level objective;

   (g) upon detecting an event marker of type CONFIDENCE, associating a confidence level and reason with the agent's current state, and rendering a visual confidence indicator in the dashboard;

   (h) broadcasting detected events to connected dashboard clients via a real-time push connection, with deduplication such that STATUS and SUMMARY events are only transmitted when the payload value has changed; and

   (i) computing an activity metric based on the recency of log file modification, and upon determining that the agent has been idle beyond a configurable threshold, triggering an automated notification action.

### Dependent Claims (Claim 2)

9. The method of claim 8, wherein the structured event markers are embedded inline within natural language agent output, and the pattern matching uses a compiled text pattern that identifies the delimiter sequences without requiring the agent to use a structured output format or API.

10. The method of claim 8, further comprising persisting detected events to a persistent data store with timestamps, enabling historical activity timeline reconstruction and full-text search across agent status history.

11. The method of claim 8, wherein the automated notification action comprises creating a webhook delivery record containing the agent identifier, event type, and a human-readable summary, and dispatching the webhook to one or more configured external endpoint URLs.

12. The method of claim 8, wherein upon detecting that the log stream has been truncated, the method resets the read position marker to handle agent process restarts.

### Independent Claim 3 — Cursor-Based Inter-Agent Communication

13. A computer-implemented method for facilitating asynchronous communication between a plurality of artificial intelligence agents, the method comprising:

   (a) maintaining a message board data structure comprising an ordered sequence of messages stored in a persistent data store, each message having a unique monotonically increasing integer identifier, a sender identifier corresponding to an agent session, a timestamp, and a content payload;

   (b) maintaining, for each subscribed agent, an independent read cursor stored as an integer field in a subscriber record, the cursor indicating the last message identifier read by that agent;

   (c) in response to a read request from a first agent, querying for all messages having identifiers greater than the first agent's read cursor where the sender is not the first agent, returning the results, and advancing the cursor to account for both the delivered messages and the subscriber's own posted messages, thereby preventing the subscriber's own posts from blocking cursor progression;

   (d) in response to a post request from a second agent, appending a new message to the ordered sequence with the next sequential identifier, without advancing any agent's read cursor;

   (e) providing a command-line interface accessible from within the agent's terminal environment, the interface reading connection configuration from a pre-written state file that eliminates the need for the agent to know server URLs or session identifiers; and

   (f) upon agent session restart with a new session identifier, transferring the existing subscription to the new session identifier while preserving the read cursor position, enabling the agent to receive messages posted during its downtime without re-reading previously delivered messages.

### Dependent Claims (Claim 3)

14. The method of claim 13, further comprising configurable receive modes per subscriber, wherein the receive mode determines which messages are delivered: all messages, only messages containing mention patterns matching the subscriber's identifier or role, messages from specified group members, or no messages.

15. The method of claim 14, wherein the mention pattern matching comprises case-insensitive string matching against patterns including a broadcast keyword, the subscriber's session identifier, and the subscriber's role title.

16. The method of claim 13, further comprising a programmatic interface providing equivalent operations to the command-line interface, enabling access from external systems and dashboard clients.

17. The method of claim 13, wherein multiple message boards coexist, each scoped to a project identifier, and agents may be subscribed to at most one board at a time, with subscription transfer preserving cross-board state.

### Independent Claim 4 — Agent Session Persistence with Team State Replay

18. A computer-implemented method for persisting and restoring artificial intelligence agent sessions within a team context, the method comprising:

   (a) storing, for each agent session in a persistent data store record, a session state comprising: an agent type identifier specifying the AI model backend, an execution environment path corresponding to an isolated version-controlled working copy, a behavior prompt defining the agent's role, a message board subscription identifier linking the agent to a team communication channel, and a session state flag;

   (b) in response to a sleep command targeting one or more agents, transitioning each targeted session's state flag to suspended, sending an interrupt signal to each agent process via the terminal multiplexer, and preserving the complete session record including message board cursor position in the persistent data store;

   (c) in response to a wake command targeting one or more previously suspended agents, for each targeted agent:
      (i) spawning a new agent process in the stored execution environment via the terminal multiplexer,
      (ii) re-subscribing the agent to its stored message board with cursor position preserved at the pre-sleep value,
      (iii) re-transmitting the stored behavior prompt to the new agent process to re-establish role context, and
      (iv) transitioning the session state flag to active;

   (d) upon wake, making available to each restored agent all messages posted to its message board during the suspension period, delivered through the cursor-based read mechanism without duplicate delivery; and

   (e) reflecting the restored session state in the graphical dashboard with preserved team grouping, hierarchical position, status indicators, and collective control operations.

### Dependent Claims (Claim 4)

19. The method of claim 18, wherein the sleep and wake commands may target individual agents or entire teams, and wherein team-level operations iterate over all agents sharing the same message board subscription.

20. The method of claim 18, further comprising a session history system that records completed sessions with full-text search indexing, enabling operators to search past agent activity across all sessions.

---

## ABSTRACT

A computer-implemented system and method for orchestrating multiple artificial intelligence agents operating in parallel on shared task-oriented workflows. The system provisions isolated execution environments using version-controlled parallel working copies and multiplexed terminal sessions, enabling concurrent work without conflicts. A real-time inline status extraction protocol parses structured event markers from agent terminal output to provide live monitoring of agent status, goals, and confidence levels without requiring agent API integration. A cursor-based message board system enables asynchronous inter-agent communication with independent read positions, configurable receive modes, and guaranteed delivery. Session persistence supports suspend/restore operations that preserve and restore full agent state including team membership, communication history, and behavior prompts. A web dashboard with real-time push connections provides visualization and control of all agents, with hierarchical team organization and collective operations.

---

## DRAWINGS (To Be Prepared)

See `figure_requirements.md` for detailed specifications of all required figures.

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
| Initial prompt delivery | CLI positional argument passed directly to the agent process |
| Subsequent command delivery | tmux send-keys with bracket paste mode |
| Communication state file | JSON file on local filesystem |
| Inline protocol delimiter format | `\|\|PULSE:<EVENT_TYPE> <payload>\|\|` (double-pipe delimiters) |
| Unique session identifier | UUID v4 encoded in terminal session name as `{agent_type}-{uuid}` |
| Cursor advancement algorithm | max(current_cursor, highest_delivered_id, own_highest_posted_id) |

These implementation choices are illustrative and not limiting. The methods and systems described herein may be implemented using any suitable combination of process multiplexing, version control, data persistence, web serving, and real-time communication technologies.

---

## FILING NOTES

### Priority Date
- First public disclosure: February 17, 2026 (GitHub repository made public)
- U.S. grace period deadline: February 17, 2027
- Non-provisional filing target: March 2026

### International Considerations
- Public disclosure on February 17, 2026 may bar patent rights in absolute novelty jurisdictions (EU, Japan, China, Korea)
- PCT filing may still be possible if provisional is filed before the bar date in each jurisdiction, using the provisional's priority date
- Consult patent attorney on international filing strategy

### Prior Art to Distinguish
- AutoGen (Microsoft): API-level multi-agent orchestration — no process isolation, no terminal monitoring, no persistent sessions
- CrewAI: Role-based agent framework — operates within a single process, no execution environment isolation, no real-time status extraction from terminal output
- Terminal multiplexers (tmux, screen, etc.): Process isolation — no agent-aware monitoring, communication, or state management
- Messaging platforms (Slack, Discord, etc.): Message-based communication — not designed for AI agent inter-process communication with cursor-based delivery
- CI/CD dashboards (GitHub Actions, Jenkins, etc.): Pipeline monitoring — not real-time AI agent status monitoring with inline protocol extraction

### Inventor(s)
[TO BE COMPLETED — Name(s) and address(es) of inventor(s)]

### Assignee
[TO BE COMPLETED — Entity name if assigned]
