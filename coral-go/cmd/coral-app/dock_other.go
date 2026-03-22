//go:build webview && !darwin

package main

// dockRaiseWindow is a no-op on non-macOS platforms.
func dockRaiseWindow() {}
