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
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	webview "github.com/webview/webview_go"
)

var debugMode bool

// webviewInstance holds the webview reference for signal-triggered shutdown.
// Set after webview creation so the signal handler can call Terminate()
// instead of os.Exit, allowing deferred cleanup (webview.Destroy) to run.
var webviewInstance webview.WebView

// init locks the main goroutine to OS thread 0 before any scheduling.
// This must happen in init(), not main(), because Go may reschedule the
// main goroutine to a different OS thread before main() runs. macOS
// Cocoa/WKWebView requires thread 0.
func init() {
	runtime.LockOSThread()
}

func main() {

	url := flag.String("url", "http://localhost:8420", "Coral server URL")
	title := flag.String("title", "Coral", "Window title")
	width := flag.Int("width", 1200, "Window width")
	height := flag.Int("height", 800, "Window height")
	wait := flag.Bool("wait", false, "Wait for server to be ready before opening")
	debugFlag := flag.Bool("debug", false, "Enable debug logging to ~/.coral/app.log")
	flag.Parse()

	debugMode = *debugFlag || os.Getenv("CORAL_DEBUG") == "1"

	// Always log to file so we catch crashes — debug mode adds JS console redirect
	setupDebugLogging()

	// Global panic recovery — log the stack trace before crashing
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[FATAL] panic recovered: %v", r)
			log.Printf("[FATAL] stack trace:\n%s", debug.Stack())
			os.Exit(1)
		}
	}()

	// Ignore SIGHUP — macOS sends it during sleep/wake transitions
	signal.Ignore(syscall.SIGHUP)

	// Catch SIGTERM/SIGINT for clean shutdown — terminate the webview event
	// loop so deferred Destroy() runs, preventing leaked WKWebView processes.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		log.Printf("[SHUTDOWN] received signal: %v", sig)
		log.Printf("[SHUTDOWN] goroutines: %d", runtime.NumGoroutine())
		if w := webviewInstance; w != nil {
			log.Println("[SHUTDOWN] terminating webview event loop")
			w.Terminate()
		} else {
			log.Println("[SHUTDOWN] no webview yet, exiting")
			os.Exit(1)
		}
	}()

	log.Printf("[STARTUP] coral-app starting pid=%d", os.Getpid())
	log.Printf("[STARTUP] args=%v", os.Args)
	log.Printf("[STARTUP] go=%s os=%s arch=%s goroutines=%d", runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumGoroutine())
	log.Printf("[STARTUP] url=%s title=%s size=%dx%d wait=%v debug=%v", *url, *title, *width, *height, *wait, debugMode)

	// Optionally wait for the server to be ready
	if *wait {
		log.Printf("[STARTUP] waiting for server at %s...", *url)
		if !waitForServer(*url, 15*time.Second) {
			log.Printf("[STARTUP] server not responding after 15s — exiting")
			fmt.Fprintf(os.Stderr, "Server at %s is not responding. Is Coral running?\n", *url)
			os.Exit(1)
		}
		log.Println("[STARTUP] server is ready")
	}

	log.Println("[WEBVIEW] calling showInDock()")
	showInDock()
	log.Println("[WEBVIEW] showInDock() dispatched")

	log.Println("[WEBVIEW] installing Edit menu")
	installEditMenu()
	log.Println("[WEBVIEW] Edit menu dispatched")

	log.Printf("[WEBVIEW] creating webview (devtools=%v)", debugMode)
	// In debug mode, enable DevTools (pass true to webview.New)
	w := webview.New(debugMode)
	webviewInstance = w
	log.Println("[WEBVIEW] webview created successfully")
	defer func() {
		log.Println("[WEBVIEW] destroying webview")
		w.Destroy()
		log.Println("[WEBVIEW] webview destroyed")
	}()

	log.Printf("[WEBVIEW] setting title=%q size=%dx%d", *title, *width, *height)
	w.SetTitle(*title)
	w.SetSize(*width, *height, webview.HintNone)

	// Configure native title bar (macOS: transparent, content extends into title bar)
	log.Println("[WEBVIEW] setting up native titlebar")
	setupNativeTitlebar()
	log.Println("[WEBVIEW] titlebar setup complete")

	// Inject native app flag and classes on <html> synchronously, and on
	// <body> at DOMContentLoaded. Both are needed:
	// - <html> classes: available before first CSS layout pass
	// - <body> classes: required for .native-app.native-macos selectors
	//   (same-element compound selectors only match on <body>)
	log.Println("[WEBVIEW] injecting native app flag and classes")
	w.Init(`window.__CORAL_APP__ = true;
		document.documentElement.classList.add('native-app');
		if (navigator.platform.indexOf('Mac') !== -1) document.documentElement.classList.add('native-macos');
		if (navigator.platform.indexOf('Win') !== -1) document.documentElement.classList.add('native-windows');
		document.addEventListener('DOMContentLoaded', function() {
			document.body.classList.add('native-app');
			if (navigator.platform.indexOf('Mac') !== -1) document.body.classList.add('native-macos');
			if (navigator.platform.indexOf('Win') !== -1) document.body.classList.add('native-windows');
		});`)

	// Bind JS console.log to Go logger in debug mode
	log.Println("[WEBVIEW] setting up JS console redirect and WS monitoring")
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

	log.Printf("[WEBVIEW] navigating to %s", *url)
	w.Navigate(*url)

	log.Printf("[WEBVIEW] starting event loop (goroutines=%d)", runtime.NumGoroutine())
	w.Run()
	log.Printf("[WEBVIEW] event loop exited (goroutines=%d)", runtime.NumGoroutine())
	log.Println("[SHUTDOWN] coral-app exiting normally")
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
