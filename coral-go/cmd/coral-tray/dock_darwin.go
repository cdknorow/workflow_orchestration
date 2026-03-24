package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// hideFromDock sets the tray process to not appear in the macOS Dock.
// Uses dispatch_after with a 300ms delay to avoid a race with NSMenu rendering
// that collapses the first menu item's frame to zero height.
void hideFromDock() {
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 300 * NSEC_PER_MSEC),
        dispatch_get_main_queue(), ^{
            [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        });
}
*/
import "C"

// hideTrayFromDock hides the coral-tray process from the Dock.
// Called after systray has initialized NSApplication.
func hideTrayFromDock() {
	C.hideFromDock()
}
