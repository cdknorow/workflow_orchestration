//go:build webview

// Command coral-app opens the Coral dashboard in a native webview window.
//
// This is a lightweight wrapper around webview/webview_go that displays
// the Coral web dashboard without browser chrome (no URL bar, no tabs).
// The server must be running separately (via coral or coral-tray).
//
// Usage:
//
//	coral-app                             # Open dashboard at default URL
//	coral-app --url http://localhost:9000  # Custom server URL
//	coral-app --width 1400 --height 900   # Custom window size
//	coral-app --debug                     # Enable debug logging to ~/.coral/app.log
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	webview "github.com/webview/webview_go"
)

var debugMode bool

func main() {
	url := flag.String("url", "http://localhost:8420", "Coral server URL")
	title := flag.String("title", "Coral", "Window title")
	width := flag.Int("width", 1200, "Window width")
	height := flag.Int("height", 800, "Window height")
	wait := flag.Bool("wait", false, "Wait for server to be ready before opening")
	debug := flag.Bool("debug", false, "Enable debug logging to ~/.coral/app.log")
	flag.Parse()

	debugMode = *debug || os.Getenv("CORAL_DEBUG") == "1"

	if debugMode {
		setupDebugLogging()
		log.Println("coral-app starting in debug mode")
		log.Printf("  url=%s title=%s size=%dx%d wait=%v", *url, *title, *width, *height, *wait)
	}

	// Optionally wait for the server to be ready
	if *wait {
		if debugMode {
			log.Printf("Waiting for server at %s...", *url)
		}
		if !waitForServer(*url, 15*time.Second) {
			fmt.Fprintf(os.Stderr, "Server at %s is not responding. Is Coral running?\n", *url)
			os.Exit(1)
		}
		if debugMode {
			log.Println("Server is ready")
		}
	}

	if debugMode {
		log.Println("Creating webview (debug=true for DevTools)")
	}

	// In debug mode, enable DevTools (pass true to webview.New)
	w := webview.New(debugMode)
	defer w.Destroy()

	w.SetTitle(*title)
	w.SetSize(*width, *height, webview.HintNone)

	// Configure native title bar (macOS: transparent, content extends into title bar)
	setupNativeTitlebar()

	// Inject native app flag so frontend can adjust styling (title bar padding, drag regions)
	w.Init(`window.__CORAL_APP__ = true; document.addEventListener('DOMContentLoaded', function() {
		document.body.classList.add('native-app');
		if (navigator.platform && navigator.platform.indexOf('Mac') !== -1) document.body.classList.add('native-macos');
		if (navigator.platform && navigator.platform.indexOf('Win') !== -1) document.body.classList.add('native-windows');
	});`)

	// Bind JS console.log to Go logger in debug mode
	if debugMode {
		w.Bind("_coralLog", func(level, msg string) {
			log.Printf("[JS %s] %s", level, msg)
		})

		// Inject JS that redirects console.log/warn/error to Go
		w.Init(`
			(function() {
				const origLog = console.log;
				const origWarn = console.warn;
				const origError = console.error;
				console.log = function() {
					origLog.apply(console, arguments);
					try { _coralLog('LOG', Array.from(arguments).join(' ')); } catch(e) {}
				};
				console.warn = function() {
					origWarn.apply(console, arguments);
					try { _coralLog('WARN', Array.from(arguments).join(' ')); } catch(e) {}
				};
				console.error = function() {
					origError.apply(console, arguments);
					try { _coralLog('ERROR', Array.from(arguments).join(' ')); } catch(e) {}
				};

				// Log navigation and WebSocket events
				window.addEventListener('hashchange', function(e) {
					_coralLog('NAV', 'hashchange: ' + location.hash);
				});
				window.addEventListener('popstate', function(e) {
					_coralLog('NAV', 'popstate: ' + location.href);
				});

				// Monitor WebSocket connections
				const origWS = window.WebSocket;
				window.WebSocket = function(url, protocols) {
					_coralLog('WS', 'connecting: ' + url);
					const ws = new origWS(url, protocols);
					ws.addEventListener('open', function() { _coralLog('WS', 'connected: ' + url); });
					ws.addEventListener('close', function(e) { _coralLog('WS', 'closed: ' + url + ' code=' + e.code); });
					ws.addEventListener('error', function() { _coralLog('WS', 'error: ' + url); });
					return ws;
				};
				window.WebSocket.prototype = origWS.prototype;
				window.WebSocket.CONNECTING = origWS.CONNECTING;
				window.WebSocket.OPEN = origWS.OPEN;
				window.WebSocket.CLOSING = origWS.CLOSING;
				window.WebSocket.CLOSED = origWS.CLOSED;
			})();
		`)

		log.Printf("Navigating to %s", *url)
	}

	w.Navigate(*url)

	if debugMode {
		log.Println("Starting webview event loop")
	}

	w.Run()

	if debugMode {
		log.Println("Webview closed")
	}
}

// setupDebugLogging redirects log output to ~/.coral/app.log.
func setupDebugLogging() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	logDir := filepath.Join(home, ".coral")
	os.MkdirAll(logDir, 0755)
	logFile := filepath.Join(logDir, "app.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	log.SetOutput(f)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	fmt.Fprintf(os.Stderr, "Debug logging to %s\n", logFile)
}

// waitForServer polls the server URL until it responds or timeout is reached.
func waitForServer(url string, timeout time.Duration) bool {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}
