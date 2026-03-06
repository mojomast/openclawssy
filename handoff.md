# Runtime Maintenance Handoff

Date: 2026-03-06

## Context

The implementation agent was trying to scaffold a browser app project under `/app/workspace/ussyflow/ussydub`. Multiple runtime issues prevented this. This session diagnosed and partially fixed them. Two critical issues remain unfinished.

## Architecture (must-know)

The runtime runs with `--sandbox-active --sandbox-provider docker`. This means:

- **File tools (`fs.read`, `fs.write`, `fs.list`, etc.) operate inside a Docker sandbox container**, not the main container's filesystem.
- The sandbox container for the `default` agent is `openclawssy_agent_default` with volume `openclawssy_ws_default` mounted at `/workspace`.
- Sub-agents (e.g. `ussyflow_builder`) each get their OWN sandbox container named `openclawssy_agent_<agent_id>` with their OWN separate `/workspace` volume.
- The main container's `/app/workspace/` is a DIFFERENT filesystem. Files there are NOT visible to the agent's tools.
- The `dockerPolicyEnforcer` resolves all tool paths against `/workspace` (container root), not `/app/workspace`.
- `DockerProvider.WriteFile` already does `mkdir -p` on parent directories automatically (`sandbox/docker.go:467-471`).

## What was completed

### A. Message-role serialization fix
- `internal/runtime/model.go`: `normalizeProviderMessageRole` now remaps `"tool"` to `"user"` for ALL providers, not just Hatz. The provider payload only includes `role` and `content` (no `tool_call_id`), so the `"tool"` role always caused 422 errors on strict providers.

### B. Retry handling improvements
- `internal/agent/run_state.go`: Increased `noChoicesRetryCap` (2->3), `transientModelRetryCap` (2->3). Added exponential backoff (500ms base, doubling, 4s cap). Extended `isTransientProviderModelError` to also match `"connection reset"`, `"unexpected eof"`, `"provider returned no choices"`.

### C. `fs.mkdir` tool added
- New tool registered in `internal/tools/builtins.go`, aliased in `model.go` and `toolparse/parser.go`, added to `allowedTools` in `engine.go`.
- Uses `policy.Enforcer.ResolveMkdirPath()` (new method in `pathguard.go`) which walks up to the nearest existing ancestor to validate workspace containment.
- `fs.write` also auto-creates parent directories when `ResolveWritePath` fails with "write parent does not exist" (falls back to `ResolveMkdirPath` + `os.MkdirAll`).
- Added to `subagent_defaults.allowed_tools` in config (code-level default and live config).
- Added to `orchestrator` persisted grants in `capabilities.json`.

### D. Structural blocker detection
- `internal/agent/run_state.go`: New `structuralBlockerCounts` map tracks error categories like `"missing_parent_directory"`, `"capability_denied"`, `"outside_workspace"`, `"protected_path"`. Hard-stops after 3 occurrences of the same category with an owner-facing escalation message instead of burning through the tool iteration cap.

### E. Token/timeout increases
- `maxResponseTokens`: 20000 -> 32000
- `defaultProviderTimeout`: 90s -> 120s
- Config validation and defaults updated to match.

### F. Live container fixes
- Created `/workspace/ussyflow/ussydub/` in the sandbox container (`openclawssy_agent_default`).
- Added `fs.mkdir` and `fs.append` to persisted capabilities and subagent defaults config.

## What remains UNFIXED (critical)

### Issue 1: `devplan.md` control-plane filename false positive

**File:** `internal/tools/builtins.go:25-32`

```go
var workspaceControlPlaneFilenames = map[string]bool{
    "SOUL.MD":     true,
    "RULES.MD":    true,
    "TOOLS.MD":    true,
    "HANDOFF.MD":  true,
    "DEVPLAN.MD":  true,
    "SPECPLAN.MD": true,
}
```

**The bug:** `guardWorkspaceControlPlaneFilename` (line 1138) checks `strings.ToUpper(filepath.Base(targetAbs))`. This blocks ANY file named `devplan.md` (case-insensitive) ANYWHERE in the workspace, including deep project subdirectories like `/workspace/ussyflow/ussydub/devplan.md`.

**The intent** was to prevent agents from writing these files at the workspace root, since the real control-plane files live at `.openclawssy/agents/<id>/DEVPLAN.MD`. But the guard fires on all paths.

**Fix needed:** The guard should only fire when the file is at the workspace root level (depth 0 or 1), not inside project subdirectories. Something like:

```go
func guardWorkspaceControlPlaneFilename(workspace, targetAbs, agentID string) error {
    base := strings.ToUpper(filepath.Base(targetAbs))
    if !workspaceControlPlaneFilenames[base] {
        return nil
    }
    // Only guard files at or near workspace root, not deep inside projects
    within, err := isWithinWorkspace(workspace, targetAbs)
    if err != nil || !within {
        return err
    }
    rel, err := filepath.Rel(workspace, targetAbs)
    if err != nil {
        return nil // can't determine depth, allow
    }
    depth := len(strings.Split(filepath.ToSlash(rel), "/"))
    if depth > 2 {
        return nil // deep inside a project subdir, allow
    }
    // ... existing error message ...
}
```

There are existing tests for this guard in `tools_test.go` (search `TestFsWriteRejectsWorkspaceControlPlaneFilename`). Add a companion test that confirms `ussyflow/ussydub/devplan.md` is ALLOWED.

### Issue 2: Sandbox container isolation causes invisible writes

**Root cause:** When a sub-agent like `ussyflow_builder` runs, it gets its own sandbox container (`openclawssy_agent_ussyflow_builder`) with a fresh `/workspace` volume. The main workspace content (from `openclawssy_agent_default`) is not shared. So:

1. The orchestrator (running as `default` agent) can see files in `openclawssy_agent_default`'s `/workspace`.
2. A sub-agent writes files into ITS container's `/workspace` (which starts empty).
3. The orchestrator lists the directory and sees nothing (it reads from the `default` container).
4. Files appear to vanish.

**This is a design question, not a simple bug.** Options:

1. **Share a single workspace volume across all agents** - simplest, but reduces isolation
2. **Copy workspace content into sub-agent containers on Start** - preserves isolation, adds overhead
3. **Mount the same volume for agents that share a project** - middle ground, needs config

The quickest operational fix: make the `ussyflow_builder` sub-agent work run through the `default` agent (not as a separate sandbox), or change the config so sub-agents inherit the parent's sandbox container. Check how `sandbox.NewProviderForAgent` creates the provider and whether there's a way to specify a shared container/volume.

## Files changed (uncommitted)

All changes pass `make fmt && make lint && make test` and `make build && ./bin/openclawssy doctor`.

Key files:
- `internal/agent/run_state.go` - structural blocker detection, retry improvements
- `internal/agent/runner.go` - blocker escalation formatter, retry constants
- `internal/agent/runner_test.go` - updated assertions for new retry caps + blocker test
- `internal/config/config.go` - defaults for tokens, timeout, subagent tools
- `internal/config/config_test.go` - updated assertions
- `internal/policy/pathguard.go` - `ResolveMkdirPath` method
- `internal/policy/policy_test.go` - mkdir path tests
- `internal/runtime/engine.go` - `fs.mkdir` in allowedTools, docker policy method
- `internal/runtime/engine_test.go` - role assertion update
- `internal/runtime/model.go` - role normalization, token/timeout, fs.mkdir alias
- `internal/runtime/model_test.go` - updated assertions
- `internal/toolparse/parser.go` - fs.mkdir alias
- `internal/tools/builtins.go` - fs.mkdir handler, fs.write auto-parent, isParentMissingError
- `internal/tools/registry.go` - ResolveMkdirPath in Policy interface
- `internal/tools/tools_test.go` - fakePolicy update, mkdir/auto-parent tests

## Verification sequence for next agent

After fixing the two remaining issues, run this sequence to confirm everything works:

```bash
# 1. Build and test
make fmt && make lint && make test
make build && ./bin/openclawssy doctor

# 2. Rebuild and restart container
docker compose build && docker compose up -d

# 3. Verify sandbox state
docker exec openclawssy_agent_default ls /workspace/ussyflow/ussydub/

# 4. Test fs.write with devplan.md in a subdirectory (via agent or direct API call)
# Should succeed after the control-plane guard fix

# 5. Test that a sub-agent can write and then read back files
# (requires fixing the sandbox isolation issue or using default agent)
```

## Container topology

```
openclawssy-openclawssy-1  (main server, runs as root, sandbox-provider=docker)
  |
  +-- openclawssy_agent_default  (sandbox container, volume: openclawssy_ws_default -> /workspace)
  |     has: /workspace/ussyflow/ussydub/ (empty, ready for files)
  |
  +-- (sub-agent containers created on demand, each with own volume, start empty)

devussy-ircd  (IRC daemon, unrelated)
```
