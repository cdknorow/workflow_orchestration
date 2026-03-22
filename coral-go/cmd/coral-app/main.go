//go:build webview

// Command coral-app opens the Coral dashboard in a native webview window.
//
// This is a lightweight wrapper around webview/webview_go that displays
// the Coral web dashboard without browser chrome (no URL bar, no tabs).
// The server must be running separately (via coral or coral-tray).
//
// Usage:
//
//	coral-app                           # Open dashboard at default URL
//	coral-app --url http://localhost:9000  # Custom server URL
//	coral-app --width 1400 --height 900   # Custom window size
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	webview "github.com/webview/webview_go"
)

func main() {
	url := flag.String("url", "http://localhost:8420", "Coral server URL")
	title := flag.String("title", "Coral", "Window title")
	width := flag.Int("width", 1200, "Window width")
	height := flag.Int("height", 800, "Window height")
	wait := flag.Bool("wait", false, "Wait for server to be ready before opening")
	flag.Parse()

	// Optionally wait for the server to be ready
	if *wait {
		if !waitForServer(*url, 15*time.Second) {
			fmt.Fprintf(os.Stderr, "Server at %s is not responding. Is Coral running?\n", *url)
			os.Exit(1)
		}
	}

	w := webview.New(false)
	defer w.Destroy()

	w.SetTitle(*title)
	w.SetSize(*width, *height, webview.HintNone)
	w.Navigate(*url)
	w.Run()
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
