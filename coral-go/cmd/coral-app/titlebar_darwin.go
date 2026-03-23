//go:build webview

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// Drag state for title bar window dragging.
// WKWebView does not support -webkit-app-region: drag, so we use
// NSEvent local monitors to detect drags in the title bar region
// and call performWindowDragWithEvent: to move the window natively.
static NSEvent * __strong _titleBarMouseDown;
static BOOL _titleBarDragArmed;

static NSWindow *findAppWindow() {
    NSWindow *window = [[NSApplication sharedApplication] keyWindow];
    if (!window) window = [[NSApplication sharedApplication] mainWindow];
    if (!window) {
        for (NSWindow *w in [[NSApplication sharedApplication] windows]) {
            if ([w isVisible]) { window = w; break; }
        }
    }
    return window;
}

static void setupWindowDrag(NSWindow *window) {
    window.titlebarAppearsTransparent = YES;
    window.titleVisibility = NSWindowTitleHidden;
    window.styleMask |= NSWindowStyleMaskFullSizeContentView;

    // Height of the app's top bar that should be draggable.
    CGFloat dragZoneHeight = 42;

    // Arm drag when mouse goes down in the title bar region.
    [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskLeftMouseDown handler:^NSEvent *(NSEvent *event) {
        NSWindow *w = event.window;
        if (!w || !w.contentView) return event;
        NSPoint loc = event.locationInWindow;
        CGFloat windowHeight = w.contentView.frame.size.height;
        if (loc.y > windowHeight - dragZoneHeight) {
            _titleBarMouseDown = event;
            _titleBarDragArmed = YES;
        }
        return event;
    }];

    // Once the mouse has moved more than 3px, initiate a native window drag.
    [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskLeftMouseDragged handler:^NSEvent *(NSEvent *event) {
        if (_titleBarDragArmed && _titleBarMouseDown) {
            NSPoint cur   = event.locationInWindow;
            NSPoint start = _titleBarMouseDown.locationInWindow;
            CGFloat dx = cur.x - start.x;
            CGFloat dy = cur.y - start.y;
            if (dx * dx + dy * dy > 9) {
                _titleBarDragArmed = NO;
                [event.window performWindowDragWithEvent:_titleBarMouseDown];
                _titleBarMouseDown = nil;
            }
        }
        return event;
    }];

    // Disarm on mouse up (was a click, not a drag — let WKWebView handle it).
    [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskLeftMouseUp handler:^NSEvent *(NSEvent *event) {
        _titleBarDragArmed = NO;
        _titleBarMouseDown = nil;
        return event;
    }];
}

void configureTitlebar() {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSWindow *window = findAppWindow();
        if (window) {
            setupWindowDrag(window);
        } else {
            // Retry once if window isn't ready yet.
            dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 500 * NSEC_PER_MSEC),
                           dispatch_get_main_queue(), ^{
                NSWindow *w = findAppWindow();
                if (w) setupWindowDrag(w);
            });
        }
    });
}
*/
import "C"

import "time"

// setupNativeTitlebar configures the macOS window for a transparent title bar
// and installs native event monitors for window dragging (since WKWebView
// does not support the -webkit-app-region CSS property).
func setupNativeTitlebar() {
	// Give the window time to appear before configuring
	go func() {
		time.Sleep(200 * time.Millisecond)
		C.configureTitlebar()
	}()
}
