# Handoff

## Status: Tool-call loop fix complete and deployed

## Summary

Fixed a systemic bug where the LLM agent could repeatedly call the same tool (e.g., `scheduler.list`)
with identical arguments, getting identical results, without being stopped. The system's 6-layer
loop-prevention mechanism had two interacting gaps that allowed uncacheable tools to loop up to the
120-iteration hard cap.

## Root Cause

### Gap 1: "Double gap" tools
Tools that were both **uncacheable** (cache key returns `"|"`) AND had **no repetition cap**
(fell through `repeatedCallRepetitionKey` with `false`). These tools could loop to 120 iterations:
- `fs.list` — always uncacheable, no repetition cap
- `fs.read` on non-special paths (anything not journal/build_log/spec) — uncacheable, no cap
- `scheduler.list`, `policy.list`, `config.get`, `session.list`, `agent.list`, `run.list`,
  `run.get`, `metrics.get` — all uncacheable, no caps

### Gap 2: `hadFreshExecution` treated identical results as "progress"
In `executeTools()` (run_state.go), every uncacheable tool execution unconditionally set
`hadFreshExecution = true`, which reset `noProgressIterations` to 0. A tool returning
`{"jobs":[],"paused":false}` 100 times in a row was treated as "making progress" every time.
The no-progress detector was permanently defeated for uncacheable tools.

**No output-comparison mechanism existed** — zero code compared current tool output to previous output.

## Fixes Applied

### Fix 1 (Systemic): Stable-output detection
New `lastUncacheableOutputs` map on `runState` tracks previous output per uncacheable tool+args key.
If output is identical to previous invocation, `hadFreshExecution` stays false, allowing the
no-progress counter to advance. After 3 no-progress iterations the run finalizes. This catches
ALL uncacheable tools systemically.

**Location:** `internal/agent/run_state.go` lines 87-93 (field), lines 633-667 (detection logic)

### Fix 2 (Belt-and-suspenders): Missing repetition caps
Added explicit caps for previously uncapped tools:
- `stateReaderRepetitionCap = 3` for scheduler.list, policy.list, config.get, session.list,
  agent.list, run.list, run.get, metrics.get
- `defaultRepeatedFileListCap = 4` for fs.list (per directory path)
- `defaultRepeatedFileReadCap = 6` for fs.read on non-special paths (per file path)

**Location:** `internal/agent/run_state.go` lines 35-37 (constants), lines 1396-1406 (state reader),
lines 1420-1429 (fs.list), lines 1495-1498 (fs.read default)

## Test Changes

### Updated 6 existing tests
Mock tools returned identical output, which now triggers stable-output detection before the
repetition cap. Fixed by making mocks return varying outputs per call:
1. `TestRunnerBlocksJournalReadAfterCap` — unique attempt numbers in error messages
2. `TestRunnerBlocksBuildLogReadAfterCap` — explicit results map with unique outputs per call ID
3. `TestRunnerBlocksSpecReadAfterCap` — same pattern
4. `TestRunnerBlocksSpecReadWithFileAliasAfterCap` — same pattern
5. `TestRunnerBecomussyReadToolsGetHigherRepetitionCap` — explicit results map
6. `TestDelegationModeFullAutoKeepsPromptOnlyTriggerAdvisory` — different paths to avoid
   FailureScore + LoopScore pushing complexity to High

### Added 5 new tests
1. `TestStableOutputDetectionStopsLoop` — verifies identical uncacheable output triggers no-progress
2. `TestStableOutputDetectionAllowsProgressWhenOutputChanges` — verifies varying output is not blocked
3. `TestRunnerBlocksSchedulerListAfterCap` — cap = 3 for scheduler.list
4. `TestRunnerBlocksFsListAfterCap` — cap = 4 for fs.list per path
5. `TestRunnerBlocksDefaultFsReadAfterCap` — cap = 6 for fs.read on non-special paths

## Validation

- All 30 Go test packages pass (`make test`)
- `go vet ./...` clean
- Binary builds successfully (`make build`)
- Docker container rebuilt and deployed on port 8081
- **Live verification**: Test run sent "What jobs are currently scheduled? Also list the workspace
  root directory." — agent called `scheduler.list` once and `fs.list` once, got results, composed
  response, completed in ~40s. Previously this would have looped 20+ times on `scheduler.list`.

## Files Modified

### Core fix:
- `internal/agent/run_state.go` — stable-output detection + missing repetition caps

### Tests:
- `internal/agent/runner_test.go` — 6 test updates + 5 new tests
- `internal/agent/planner_test.go` — 1 test update (different paths)

## Docker Deployment Notes

- The compose-managed container is `openclawssy-openclawssy-1`
- There was a manually-run container named `openclawssy` holding port 8081 that had to be
  force-stopped before the compose container could start
- Bearer token is `change-me` (set via `OPENCLAWSSY_TOKEN` env var in docker-compose.yml)
- Dashboard: http://localhost:8081/dashboard
- API: http://localhost:8081/v1/runs (requires `Authorization: Bearer change-me`)

## Previous Session Context

Prior session (documented in earlier versions of this file) fixed:
- Delegation bugs (planner taking over too early, interrupted subagent treated as success)
- Dashboard keyboard shortcuts
- Live config may differ from code defaults (`delegation_mode` might be `full_autonomous`)

## Next Steps

- Changes are uncommitted — commit when ready
- Consider whether `delegation_mode` should be verified in the live config
- Monitor for any edge cases where stable-output detection might be too aggressive
  (e.g., a tool that legitimately returns the same output but where calling it is still meaningful)
