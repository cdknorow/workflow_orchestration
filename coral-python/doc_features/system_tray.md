# Instructions: Writing MkDocs Documentation for the System Tray / macOS Menu Bar App

This file contains instructions for writing the MkDocs documentation page for the system tray feature. Follow these steps to create a doc that matches the existing Coral documentation style.

---

## Step 1: Understand the feature scope

The system tray feature allows users to run Coral as a macOS menu bar app instead of keeping a terminal window open. It has two usage paths:

**Path 1 — pip install (CLI):**
1. `pip install agent-coral[tray]` — installs `rumps` as an optional dependency
2. `coral-tray` — launches as a background process, returns the terminal immediately
3. `coral-tray --stop` — stops the running tray instance

**Path 2 — macOS .app bundle (DMG installer):**
1. Download `Coral.dmg` from GitHub Releases
2. Drag `Coral.app` to Applications
3. Double-click to launch — coral icon appears in menu bar, no terminal needed

**Menu bar features:**
- Coral icon in the macOS menu bar
- "Open Dashboard" — opens `http://localhost:8420` in the default browser
- "Quit" — stops the server and removes the tray icon

**Technical details:**
- Uses `rumps` library (PyObjC-based, macOS only)
- `LSUIElement: True` — menu bar only, no Dock icon
- Uvicorn runs in a daemon thread, rumps on the main thread (macOS NSApplication requirement)
- PID file at `~/.coral/tray.pid` for process management
- Logs at `~/.coral/tray.log`
- Graceful fallback: if rumps is not installed, falls back to standard `coral` behavior

Key source files:
- `src/coral/tray.py` — rumps menu bar app with background spawning
- `src/coral/tools/utils.py` — `get_package_dir()` for resolving paths in both pip and .app bundle modes
- `setup_app.py` — py2app configuration for building Coral.app
- `scripts/build_macos.sh` — build script for .icns, .app, and .dmg
- `pyproject.toml` — `coral-tray` entry point and `[tray]` optional dependency

## Step 2: Create the documentation file

Create `docs/docs/system-tray.md`.

## Step 3: Suggested outline

```markdown
# macOS Menu Bar App

Opening paragraph: "Coral can run as a macOS menu bar app, keeping
the dashboard accessible with a single click while staying out of your
way. No terminal window required — the server runs in the background
and the coral icon lives in your menu bar."

---

## Quick Start (pip)

Show the 3-step flow:
1. Install with tray support: `pip install agent-coral[tray]`
2. Launch: `coral-tray` — starts in background, terminal freed immediately
3. Click the coral icon in the menu bar to open the dashboard

Show expected terminal output:
```
$ coral-tray
Coral tray started in background (dashboard on port 8420)
  Logs: ~/.coral/tray.log
  Stop: coral-tray --stop
$
```

## Quick Start (DMG Installer)

Show the install flow:
1. Download `Coral.dmg` from the GitHub Releases page
2. Open the DMG and drag `Coral.app` to your Applications folder
3. Launch Coral from Applications or Spotlight
4. The coral icon appears in your menu bar

<!-- TODO: Screenshot - DMG window with drag-to-Applications -->
<!-- TODO: Screenshot - Menu bar icon with dropdown menu -->

---

## Menu Bar Actions

| Action | Description |
| **Open Dashboard** | Opens `http://localhost:8420` in your default browser |
| **Quit** | Stops the web server and removes the menu bar icon |

---

## CLI Reference

| Command | Description |
| `coral-tray` | Start the tray app in the background |
| `coral-tray --stop` | Stop a running tray instance |
| `coral-tray --port 9000` | Use a custom port |
| `coral-tray --host 127.0.0.1` | Bind to a specific host |
| `coral-tray --foreground` | Run in foreground (blocks terminal, useful for debugging) |

### Process Management

- PID file: `~/.coral/tray.pid`
- Log file: `~/.coral/tray.log`
- If `coral-tray` detects an already-running instance, it prints a warning instead of double-launching
- `coral-tray --stop` sends SIGTERM for graceful shutdown

---

## Building the macOS App (for developers)

### Prerequisites

```bash
pip install py2app rumps
brew install create-dmg  # optional, for .dmg creation
```

### Build

```bash
scripts/build_macos.sh
```

This will:
1. Generate `Coral.icns` from `coral.png` (using `sips` and `iconutil`)
2. Build `dist/Coral.app` using py2app
3. Create `dist/Coral.dmg` with drag-to-Applications layout (if `create-dmg` is installed)

### How the .app bundle works

- Entry point: `src/coral/tray.py` (the rumps menu bar app)
- `LSUIElement: True` in the plist — no Dock icon, menu bar only
- Static files, templates, and bundled themes are packaged into `Contents/Resources/coral/`
- `get_package_dir()` in `coral/tools/utils.py` detects the bundle via the `RESOURCEPATH` environment variable (set by py2app) and resolves paths accordingly
- All Python dependencies (FastAPI, uvicorn, Jinja2, etc.) are bundled inside the .app

### Configuration (setup_app.py)

The py2app configuration in `setup_app.py` includes:
- All coral submodules as explicit includes (prevents lazy import issues)
- Static assets, templates, docs, and bundled themes as DATA_FILES
- Plist settings for menu-bar-only behavior and Retina support

---

## Troubleshooting

### Tray icon doesn't appear
- Check `~/.coral/tray.log` for errors
- Ensure rumps is installed: `pip install agent-coral[tray]`
- On macOS 13+, you may need to grant accessibility permissions

### Dashboard doesn't load
- Check if the port is already in use: `lsof -i :8420`
- Try a different port: `coral-tray --port 9000`
- Check logs: `tail -f ~/.coral/tray.log`

### Stopping a stuck instance
- If `coral-tray --stop` doesn't work, check the PID file: `cat ~/.coral/tray.pid`
- Kill manually: `kill $(cat ~/.coral/tray.pid)`
- Remove stale PID file: `rm ~/.coral/tray.pid`

---

## How It Works (Technical)

- `coral-tray` (without `--foreground`) spawns a detached child process via `subprocess.Popen(start_new_session=True)` and exits immediately
- The child process runs with `--foreground`, which:
  1. Writes PID to `~/.coral/tray.pid`
  2. Starts uvicorn in a daemon thread
  3. Runs the rumps NSApplication event loop on the main thread (required by macOS)
- The daemon thread ensures the server dies if the tray process exits
- `atexit` and explicit cleanup in the Quit handler remove the PID file
- In .app bundle mode, `RESOURCEPATH` env var points to `Contents/Resources/`, and `get_package_dir()` uses this to find templates, static files, etc.
```

## Step 4: Update mkdocs.yml

Add the new page to the `nav` section in `docs/mkdocs.yml`:

```yaml
nav:
  - Home: index.md
  - Multi-Agent Orchestration: multi-agent-orchestration.md
  - Live Sessions: live-sessions.md
  - Message Board: message-board.md
  - System Tray: system-tray.md   # <-- add here
  - Changed Files & Diff Viewer: changed-files-diff-viewer.md
  - Button Macros: button-macros.md
  # ... rest of nav
```

Place it after "Message Board" — it's a UX/accessibility feature rather than a core orchestration feature.

## Step 5: Style guidelines to follow

1. **Match the webhooks.md pattern** — It's the best example of a complete feature doc. Study `docs/docs/webhooks.md` for structure.

2. **Opening paragraph formula**: "[Feature name] lets you [action]. [Why it matters in 1 sentence]."

3. **Use admonitions sparingly**:
   - `!!! tip` for productivity hints (e.g., "Use `--foreground` for debugging")
   - `!!! info` for technical clarifications (e.g., the RESOURCEPATH mechanism)
   - `!!! warning` for gotchas (e.g., macOS permissions)

4. **Keep the two paths clear**: Always distinguish between the pip/CLI path and the DMG installer path. Users should be able to follow either one without confusion.

5. **Screenshots**: Add `<!-- TODO: Screenshot - ... -->` placeholders for:
   - DMG window with drag-to-Applications layout
   - Menu bar icon (zoomed in) showing the coral icon
   - Menu bar dropdown with "Open Dashboard" and "Quit"
   - Terminal showing `coral-tray` output (started + stopped)

6. **Keep it scannable**: Lead with Quick Start (two paths), then CLI Reference, then Building (for developers). Technical details at the end.

## Step 6: Build and preview

```bash
cd docs
pip install mkdocs-material  # if not installed
mkdocs serve
# Open http://localhost:8000 to preview
```
