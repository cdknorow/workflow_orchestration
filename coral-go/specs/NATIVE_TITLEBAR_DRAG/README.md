# Native Titlebar Drag

## Problem

The macOS native app (coral-app) uses a transparent titlebar with
`NSWindowStyleMaskFullSizeContentView` so web content extends edge-to-edge.
Window dragging was previously handled by CSS `-webkit-app-region: drag` on
the top bar element.

This approach was removed during a platform CSS refactor and proved unreliable
when re-added:

1. **First-click-only drag:** CSS drag regions work on the first click (when
   the window gains focus) but stop working once the WKWebView captures focus.
   Subsequent drag attempts are consumed by the webview's event handler.

2. **Timing sensitivity:** The CSS rule requires `.native-app` class to be
   present before the first layout pass. Applying it via DOMContentLoaded
   is often too late. Applying on `<html>` synchronously via `w.Init()` helps
   but doesn't fix the focus issue.

3. **webview_go limitation:** The `webview_go` library doesn't forward
   `-webkit-app-region` events to the native window system after the webview
   has focus. This is a known limitation of embedded WKWebView.

## Decision

Replace CSS-based drag with a **native Cocoa drag overlay** — a transparent
`NSView` positioned over the titlebar region that handles drag events natively
before they reach the webview.

### Why native over CSS

| Approach | Pros | Cons |
|----------|------|------|
| CSS `-webkit-app-region: drag` | No native code, works in browsers | Unreliable in WKWebView after focus, timing-dependent |
| Native `NSView` overlay | Always works, native feel, double-click zoom | Requires Cocoa code, top N pixels are non-interactive for web content |

### How Electron solves this

Electron uses a native drag handler, not CSS. Their implementation intercepts
mouse events at the native level and forwards them to the window system. Our
approach is equivalent.

## Implementation

**File:** `cmd/coral-app/titlebar_darwin.go`

A transparent `DragOverlayView` (37px tall) is placed on top of the webview:

```objc
@interface DragOverlayView : NSView
@end

@implementation DragOverlayView
- (void)mouseDown:(NSEvent *)event {
    if (event.clickCount == 2) {
        [self.window zoom:nil];      // Double-click = zoom (standard macOS)
    } else {
        [self.window performWindowDragWithEvent:event];  // Native drag
    }
}
@end
```

**Configuration:**
- Height: 37px (matches the top bar height with traffic light buttons)
- Auto-resizes with the window (`NSViewWidthSizable | NSViewMinYMargin`)
- `setMovableByWindowBackground:NO` prevents conflicts with native drag
- Window retains `titlebarAppearsTransparent` and `fullSizeContentView`

**Trade-off:** The top 37px of the window is a native drag handle. Web-rendered
buttons in that zone won't receive clicks. This is acceptable because:
- macOS traffic light buttons occupy the left portion of this zone
- The top-bar navigation buttons are positioned below the drag zone
- The CSS `-webkit-app-region: no-drag` rules are no longer needed but kept
  as a safety net for browsers

## History

1. **Original:** CSS drag in `layout.css`, classes on `<body>` via
   `w.Init()` DOMContentLoaded — worked but was removed in CSS refactor
2. **Refactor:** CSS moved to `native.css`, classes on `<html>` synchronous —
   broke (first-click-only issue discovered)
3. **Final:** Native Cocoa overlay — reliable, no CSS dependency
