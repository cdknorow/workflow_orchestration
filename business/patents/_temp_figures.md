# Patent Drawings

## System and Method for Multi-Agent AI Orchestration

---

## DRAWINGS

### Figure U1 — System Architecture Diagram
![System Architecture](/Users/cknorowski/Software/coral/coral/excluded_files/patents/figures/U1_system_architecture.png)

Block diagram showing the major system components: Operator Interface (Web Dashboard), Orchestration Engine (Agent Process Manager, Inline Status Extraction Engine, Session Persistence Layer, Message Board System), Agent Execution Layer (isolated execution environments with terminal sessions and log streams), Persistent Data Store, and Version-Controlled Repository with parallel working copies.

### Figure U2 — Agent Spawning Flowchart
![Agent Spawning](/Users/cknorowski/Software/coral/coral/excluded_files/patents/figures/U2_agent_spawning.png)

Method flowchart for Claim 1: The process of provisioning a team of agents, from receiving team configuration through spawning isolated execution environments, assigning identifiers, configuring output capture, writing state files, subscribing to the message board with appropriate receive modes, transmitting behavior prompts, and rendering the unified team interface.

### Figure U3 — Inline Status Protocol Detection Flowchart
![Status Protocol Detection](/Users/cknorowski/Software/coral/coral/excluded_files/patents/figures/U3_status_protocol_detection.png)

Method flowchart for Claim 2: The real-time inline status extraction cycle, including log stream scanning, escape sequence stripping, structured event marker pattern matching, false positive filtering, event type classification and deduplication, persistence, push-based dashboard broadcast, and idle detection with webhook dispatch.

### Figure U4 — Message Board Cursor Mechanism Flowchart
![Message Board Cursor](/Users/cknorowski/Software/coral/coral/excluded_files/patents/figures/U4_message_board_cursor.png)

Dual-operation flowchart for Claim 3: The read operation (cursor retrieval, message query with self-exclusion, cursor advancement computed as the maximum of the current cursor, the highest delivered message identifier, and the subscriber's own highest posted message identifier, result delivery) and the post operation (message append with sequential identifier, no cursor side effects). Includes delivery guarantee summary.

### Figure U5 — Session Sleep/Wake State Diagram
![Session Sleep/Wake](/Users/cknorowski/Software/coral/coral/excluded_files/patents/figures/U5_session_sleep_wake.png)

State diagram for Claim 4: Agent session lifecycle showing Active, Sleeping, and Terminated states with transition conditions. Details preserved state on sleep (session record, behavior prompt, board subscription, read cursor, execution environment path) and restored state on wake (new terminal session, output capture, board subscription transfer with cursor preservation, behavior prompt replay, message catch-up).

### Figure U6 — Subscription Transfer Sequence Diagram
![Subscription Transfer](/Users/cknorowski/Software/coral/coral/excluded_files/patents/figures/U6_subscription_transfer.png)

Sequence diagram illustrating the mechanism described in Claim 13(f), showing the subscription transfer during session restart: normal operation, process interruption with message accumulation, restart with subscription transfer preserving the read cursor, and catch-up read delivering all messages posted during the suspension period.

---
