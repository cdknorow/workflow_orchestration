//go:build webview

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// configureTitlebar makes the title bar transparent and extends content into it.
// This creates a Slack-style merged title bar where the app's top bar replaces
// the native window chrome. Traffic light buttons (close/minimize/fullscreen)
// are overlaid on top of the content.
void configureTitlebar() {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSWindow *window = [[NSApplication sharedApplication] mainWindow];
        if (!window) {
            // Try keyWindow as fallback
            window = [[NSApplication sharedApplication] keyWindow];
        }
        if (!window) {
            // Try getting any window
            NSArray *windows = [[NSApplication sharedApplication] windows];
            for (NSWindow *w in windows) {
                if ([w isVisible]) {
                    window = w;
                    break;
                }
            }
        }
        if (window) {
            window.titlebarAppearsTransparent = YES;
            window.titleVisibility = NSWindowTitleHidden;
            window.styleMask |= NSWindowStyleMaskFullSizeContentView;
        }
    });
}
*/
import "C"

import "time"

// setupNativeTitlebar configures the macOS window for a transparent title bar.
// Must be called after the webview window is created and visible.
func setupNativeTitlebar() {
	// Give the window time to appear before configuring
	go func() {
		time.Sleep(200 * time.Millisecond)
		C.configureTitlebar()
	}()
}
