# Coral Launch Modes: Standalone Server vs Tray App

## Overview

Coral can run in two distinct modes. Understanding the differences is critical
for debugging stability issues, since the tray app adds CGO/native layers that
can crash silently.

## Mode 1: Standalone Server (`cmd/coral/`)

**What it is:** Pure Go HTTP server with no native UI. Serves the Coral
dashboard as a web app accessible in any browser.

**Launch:**
```bash
cd coral-go && go run ./cmd/coral/ --dev --host 127.0.0.1 --port 8420
```

**Build & run:**
```bash
cd coral-go && go build -o coral ./cmd/coral/ && ./coral --dev --host 127.0.0.1 --port 8420
```

**Access:** Open `http://localhost:8420` in Chrome, Safari, or Firefox.

**What's included:**
- HTTP server with all API routes
- WebSocket endpoints (terminal streaming, live updates)
- SQLite database (sessions, history, settings)
- Background services (git poller, indexer, scheduler, etc.)
- tmux terminal backend
- License validation (skipped with `--dev`)

**What's NOT included:**
- No system tray icon
- No native macOS/Windows app window
- No health check overlay (browser handles connectivity natively)
- No CGO dependencies — pure Go binary
- No `Cocoa`, `systray`, or `WebView` frameworks

**When to use:**
- Stability testing (isolate server crashes from native layer crashes)
- Remote access (run on a server, access from another machine)
- Development (faster build, no CGO compilation)
- Linux deployment (no native UI needed)
- CI/CD testing

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | `127.0.0.1` | Bind address |
| `--port` | `8420` | HTTP port |
| `--dev` | `false` | Skip license validation |
| `--home` | `~/.coral` | Data directory |

---

## Mode 2: Tray App (`cmd/coral-tray/` + `cmd/coral-app/`)

**What it is:** The full native desktop experience. `coral-tray` runs the
server with a system tray icon, and `coral-app` provides the native window
(macOS WKWebView / Windows WebView2).

**Launch (foreground, for debugging):**
```bash
cd coral-go && go run ./cmd/coral-tray/ --foreground --dev --host 127.0.0.1 --port 8420
```

**Launch (background, normal mode):**
```bash
cd coral-go && go run ./cmd/coral-tray/ --dev --host 127.0.0.1 --port 8420
```

**Launch native app window (connects to running tray):**
```bash
cd coral-go && go run ./cmd/coral-app/ --url http://localhost:8420
```

**Normal usage:** Open `Coral.app` from `/Applications` — it launches both
`coral-tray` (background) and `coral-app` (window) automatically via
`launch-coral`.

**What's added on top of the standalone server:**
- System tray icon with menu (macOS menu bar / Windows system tray)
- Native app window (WKWebView on macOS, WebView2 on Windows)
- Health check polling (`/api/health` every 5s with disconnect overlay)
- External link interception (opens in system browser)
- Window drag regions (`-webkit-app-region: drag`)
- Platform-specific CSS (traffic light padding, titlebar spacing)
- Session reconciler (background service)
- CGO dependencies: `fyne.io/systray` (Cocoa/Win32), `webview/webview_go` (WebKit/WebView2)

**When to use:**
- Production desktop usage
- When you want the native app experience
- When you need the system tray icon for quick access

### Additional Flags (tray-specific)

| Flag | Default | Description |
|------|---------|-------------|
| `--foreground` | `false` | Run in foreground (don't daemonize) |
| `--backend` | `tmux` | Terminal backend: `pty` or `tmux` |
| `--no-browser` | `false` | Don't open browser on start |
| `--debug` | `false` | Enable debug logging + JS console redirect |

---

## Architecture Comparison

```
Standalone Server (cmd/coral/)          Tray App (cmd/coral-tray/ + cmd/coral-app/)
================================        ==========================================

  Browser (any)                           coral-app (WKWebView / WebView2)
      |                                       |
      | HTTP/WS                               | HTTP/WS (localhost)
      |                                       |
  +---v-----------+                       +---v-----------+
  | HTTP Server   |                       | HTTP Server   |  (same server code)
  | API Routes    |                       | API Routes    |
  | WebSocket     |                       | WebSocket     |
  | Background    |                       | Background    |
  | Services      |                       | Services      |
  | SQLite        |                       | SQLite        |
  +---------------+                       +-------+-------+
                                                  |
  Pure Go, no CGO                         +-------v-------+
                                          | System Tray   |  CGO: fyne.io/systray
                                          | (Cocoa/Win32) |
                                          +---------------+
                                                  +
                                          +---------------+
                                          | Native Window |  CGO: webview_go
                                          | (WKWebView)   |
                                          +---------------+
```

## Crash Isolation Strategy

When investigating crashes, run both modes to isolate the layer:

| Symptom | Standalone | Tray App | Root cause |
|---------|-----------|----------|------------|
| Server crashes | Crashes | Crashes | Core server bug (Go code) |
| Server stable | Stable | Crashes | Native layer (CGO/systray/webview) |
| UI blank | Works in browser | Blank in webview | WKWebView/WebView2 rendering issue |
| Drag broken | N/A | Broken | CSS `-webkit-app-region` or body class timing |

### Running Both for Comparison

```bash
# Terminal 1: Standalone server on port 8450
cd coral-go && go run ./cmd/coral/ --dev --host 0.0.0.0 --port 8450

# Terminal 2: Tray app on port 8420 (default)
cd coral-go && go run ./cmd/coral-tray/ --foreground --dev --host 127.0.0.1 --port 8420

# Compare: open http://localhost:8450 in browser vs Coral.app on port 8420
```

Use different ports to avoid conflicts. Both share the same `~/.coral/` data
directory (SQLite, settings, logs) by default — use `--home` to separate if needed.

## Log Files

| Mode | Log location | Contents |
|------|-------------|----------|
| Standalone | stdout/stderr | Server logs (no file by default) |
| Tray (background) | `~/.coral/tray.log` | Server + background service logs |
| Tray (foreground) | `~/.coral/tray.log` + stderr | Same, plus CGO crash traces |
| Native app | `~/.coral/app.log` | Webview lifecycle, startup/shutdown |
| Heartbeat | `~/.coral/heartbeat` | Last-known-alive timestamp + PID |

## Build Differences

| Aspect | Standalone | Tray App |
|--------|-----------|----------|
| CGO required | No | Yes |
| Build time | ~5s | ~15s (CGO compilation) |
| Binary size | ~25MB | ~35MB (includes native frameworks) |
| Cross-compile | Easy (GOOS/GOARCH) | Requires target OS SDK |
| Platforms | Any (Linux, macOS, Windows) | macOS + Windows only |
