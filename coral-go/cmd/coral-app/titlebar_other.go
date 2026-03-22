//go:build webview && !darwin

package main

// setupNativeTitlebar is a no-op on non-macOS platforms.
func setupNativeTitlebar() {}
