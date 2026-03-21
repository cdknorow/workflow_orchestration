# CLAUDE.md - Coral Go Parity Project

## Mission

This repo's sole purpose is to bring `coral-go/` to **full feature parity** with the Python reference implementation in the `coral` submodule.

**CRITICAL RULES:**
1. **NEVER modify anything inside the `coral/` submodule.** It is the read-only source of truth.
2. Only modify code under `coral-go/` and `tests/`.
3. When in doubt about expected behavior, read the Python implementation in `coral/` — it is authoritative.

## How to Validate Parity

### Parity Test Harness
The test harness spins up both Python and Go backends, runs identical API calls against each, and compares responses and database state.

```bash
# Run the full parity harness (requires both Python and Go servers)
python tests/parity_harness.py

# Run just the API scenario tests against already-running servers
python tests/parity/test_scenarios.py <py-port> <go-port>
```

### DB Compare Tool
Compares SQLite databases created by both backends after a test run.

```bash
cd coral-go && go build -o db-compare ./cmd/db-compare/
./db-compare <python-db> <go-db> [board-py-db] [board-go-db]
```

### Go Unit Tests
```bash
cd coral-go && go test ./...
```

## Project Structure

```
coral/                  # Python reference implementation (SUBMODULE — DO NOT MODIFY)
coral-go/               # Go rewrite (this is what you work on)
  cmd/                  # CLI entry points (coral, launch-coral, coral-board, db-compare)
  internal/             # Core packages
    agent/              # Agent implementations (claude, gemini)
    background/         # Background services (git poller, indexer, scheduler, etc.)
    board/              # Message board store
    config/             # Configuration
    jsonl/              # JSONL log reader
    license/            # License checking
    pulse/              # Pulse event parser
    ptymanager/         # PTY/tmux session management
    server/             # HTTP server, routes, frontend assets
    store/              # SQLite storage layer
    tmux/               # Tmux client
  go.mod / go.sum       # Go module dependencies
tests/                  # Parity test harness
  parity_harness.py     # Main harness — starts both servers, runs scenarios, compares DBs
  parity/
    test_scenarios.py   # API scenario tests (tags, settings, webhooks, board, etc.)
Casks/                  # Homebrew Cask definition
Formula/                # Homebrew Formula
scripts/                # macOS build script
icons/                  # App icons and screenshots
```

## Workflow for Adding Parity

1. Identify a feature or endpoint in `coral/` (the Python submodule) that is missing or differs in `coral-go/`.
2. Read the Python implementation to understand expected behavior.
3. Implement or fix the Go version under `coral-go/`.
4. Add or update parity test scenarios in `tests/parity/test_scenarios.py`.
5. Run the parity harness to confirm both backends behave identically.
6. Run `go test ./...` in `coral-go/` to ensure no regressions.

## Key Reference Points in the Python Submodule

When implementing Go parity, consult these files in `coral/`:
- `coral/src/coral/web_server.py` — all API routes and their behavior
- `coral/src/coral/api/` — route modules (the expected request/response contracts)
- `coral/src/coral/store/` — SQLite schema and queries (the expected DB structure)
- `coral/src/coral/background_tasks/` — background service behavior
- `coral/src/coral/messageboard/` — message board store and API
- `coral/src/coral/tools/` — core utilities

## Development

### Building
```bash
cd coral-go && go build -o coral ./cmd/coral/
```

### Running
```bash
cd coral-go && go run ./cmd/coral/ --host 127.0.0.1 --port 8420
```

### Database
- SQLite with WAL mode, stored at `~/.coral/sessions.db` (matches Python)
- Message board DB at `~/.coral/messageboard.db`
