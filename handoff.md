# Handoff

Date: 2026-02-24

## Completed: Dashboard streaming/UI visibility fixes

## What Was Done

Five dashboard UI fixes implemented in `chat.js` and `styles.css`:

### Fix 1: Repetition detection indicator in Loop Risk card
- Modified `buildLoopRisk()` function to detect backend repetition guard messages
- Checks if any tool event in the analysis window has `errorText` containing "repetition detected"
- If detected, adds +3 to the loop risk score and adds reason "Backend repetition guard triggered."
- This makes backend-side repetition guards visible in the client-side loop risk analysis

### Fix 2: Iteration progress indicator (Tools: N)
- Added tool count display to the run status metadata line
- Shows `· Tools: N` (where N = `streamToolEvents.length`) when streaming or polling is active
- Provides a proxy for iteration count during active runs

### Fix 3: `formatStreamingJSON` function for partial JSON display
- Added new `formatStreamingJSON()` function after `tryFormatJSONText()`
- For complete JSON: delegates to `tryFormatJSONText()` (existing formatting)
- For partial JSON (structure.partial === true): returns raw text + `\n/* ... streaming ... */` suffix
- For invalid/non-JSON: returns empty string
- Does NOT modify `tryFormatJSONText()` behavior — separate function for streaming-specific display

### Fix 4: Orchestration warning indicators
- After the Loop Risk card's reasons list, checks for two warning conditions:
  - "repetition detected" in any tool event error → yellow "Repetition guard active" warning
  - "tool-iteration limit" in streaming text or any tool event error → red "Iteration limit reached" warning
- Warning elements use `orchestration-warning` CSS class with `repetition` or `iteration-limit` modifier

### Fix 5: CSS for orchestration warnings
- Added `.orchestration-warning` base class (padding, border-radius, font styling)
- Added `.orchestration-warning.repetition` (yellow/amber theme: #f5a623 on #3b2e00)
- Added `.orchestration-warning.iteration-limit` (red theme: #ff4444 on #3b0000)
- Placed after `.chat-loop-risk-reasons` and before `.runs-controls` in the stylesheet

## Verification Results

- `node --check chat.js` — passes (no syntax errors)
- `go build ./...` — builds cleanly (embedded assets OK)

## Files Modified

- `internal/channels/dashboard/ui/src/pages/chat.js` — All 4 JS fixes (loop risk detection, tool count, formatStreamingJSON, orchestration warnings)
- `internal/channels/dashboard/ui/styles.css` — Orchestration warning CSS classes

## Next Task

No next task queued in devplan.md.

## Previous Context

Previous Ralph implemented tool-parsing resilience fixes (closeOpenDelimiters, tagged tool call tests, depth limit). Before that: orchestration reliability fixes (no-progress cap, all-blocked fast-finalization, suffix stripping, shell.exec repetition cap).
