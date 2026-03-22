//go:build webview

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// showInDock sets the app to appear in the macOS Dock as a regular application.
// CLI binaries default to Accessory/Prohibited policy — this overrides to Regular.
void showInDock() {
    [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
}

// raiseWindow brings the app window to front (called when Dock icon is clicked).
void raiseWindow() {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSWindow *window = [[NSApplication sharedApplication] keyWindow];
        if (!window) {
            NSArray *windows = [[NSApplication sharedApplication] windows];
            for (NSWindow *w in windows) {
                if ([w isVisible]) {
                    window = w;
                    break;
                }
            }
        }
        if (window) {
            [window makeKeyAndOrderFront:nil];
        }
        [NSApp activateIgnoringOtherApps:YES];
    });
}
*/
import "C"

func init() {
	C.showInDock()
}

// RaiseWindow brings the app window to front. Exported for use from main.
func dockRaiseWindow() {
	C.raiseWindow()
}
