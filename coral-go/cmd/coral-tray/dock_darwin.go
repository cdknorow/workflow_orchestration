package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// hideFromDock sets the tray process to not appear in the macOS Dock.
void hideFromDock() {
    dispatch_async(dispatch_get_main_queue(), ^{
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
