//go:build webview

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// setupEditMenu creates a standard Edit menu so that Cmd+A, Cmd+C, Cmd+V, Cmd+X
// work inside the WKWebView. Without this menu, macOS doesn't route those
// keyboard shortcuts to the web content.
void setupEditMenu() {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSMenu *mainMenu = [[NSApplication sharedApplication] mainMenu];
        if (!mainMenu) {
            mainMenu = [[NSMenu alloc] initWithTitle:@""];
            [NSApp setMainMenu:mainMenu];
        }

        // Edit menu
        NSMenuItem *editMenuItem = [[NSMenuItem alloc] initWithTitle:@"Edit" action:nil keyEquivalent:@""];
        NSMenu *editMenu = [[NSMenu alloc] initWithTitle:@"Edit"];

        [editMenu addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];
        [editMenu addItemWithTitle:@"Redo" action:@selector(redo:) keyEquivalent:@"Z"];
        [editMenu addItem:[NSMenuItem separatorItem]];
        [editMenu addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
        [editMenu addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
        [editMenu addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
        [editMenu addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];

        [editMenuItem setSubmenu:editMenu];
        [mainMenu addItem:editMenuItem];
    });
}
*/
import "C"

func init() {
	C.setupEditMenu()
}
