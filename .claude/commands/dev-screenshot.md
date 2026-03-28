# Dev Screenshot: Build, Reload, and Capture UI

Use the dev screenshot script to iterate on UI changes. This builds the Go server with dev tags, starts it on an isolated port, opens the browser, and captures a screenshot for visual verification.

## Steps

1. Run the dev screenshot script:
```bash
PATH="/usr/local/go/bin:$PATH" ./scripts/dev-screenshot.sh
```

2. Wait for the script to complete (builds, starts server on port 8450, opens browser, takes screenshot)

3. Read the screenshot to verify visual changes:
```
Read /tmp/coral-screenshot.png
```

4. Compare against the reference design or previous screenshot. Note any issues.

5. When done iterating, stop the dev server:
```bash
kill $(lsof -ti :8450) 2>/dev/null
```

## Details

- **Port:** 8450 (avoids conflicts with main app on 8420)
- **Data dir:** /tmp/.coral-dev/ (fully isolated, won't touch ~/.coral/)
- **Build tags:** `-tags dev` (skips EULA and license)
- **Screenshot:** Saved to /tmp/coral-screenshot.png
- **Browser:** Auto-opens and navigates to first available session

## Usage Pattern for UI Iteration

1. Make CSS/HTML changes
2. Run `/dev-screenshot` to rebuild and capture
3. Read the screenshot to verify
4. Repeat until the UI matches the design
