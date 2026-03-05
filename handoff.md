# Handoff

Date: 2026-03-05

## Completed: Prompt hardening and obvious runtime/sandbox fixes

### What I worked on

Tightened the baked-in prompt stack so new and existing agents get clearer execution guidance with fewer brittle prompt-generation paths, and fixed a few obvious config/sandbox correctness issues found during review.

### Prompt work

1. Reworked runtime-generated prompt docs in `internal/runtime/engine.go`
   - Replaced fragile `strings.Replace`-based prompt assembly with explicit bullet-list generation
   - Kept `RUNTIME_CONTEXT.md` and `TOOL_CALLING_BEST_PRACTICES.md` content, but made it easier to maintain and less likely to drift or break
   - Strengthened instructions around direct execution, valid tool syntax, chaining tool calls, and not leaking internal control-plane prompt filenames

2. Improved scaffolded agent prompts in `internal/agentdocs/scaffold.go`
   - `SOUL.md` now pushes direct execution, repo/context reading first, brief tradeoff mention, verification, and explicit reporting of remaining risk/follow-up
   - `RULES.md` now makes the "do obvious safe work first" and "ask at most one precise question" behavior explicit
   - `TOOLS.md` now emphasizes tool categories and correct tool selection instead of a single long unstructured list

3. Synced identity bootstrap prompt generation in `internal/tools/agent_tools.go`
   - `agent.identity.set` now writes SOUL content aligned with the updated scaffold guidance

### Obvious fixes included in the same change set

1. `internal/config/config.go`
   - Fixed `ApplyDefaults()` so partial `subagent_defaults` configs no longer lose explicitly provided fields when `allowed_tools` is omitted

2. `internal/sandbox/provider.go`
   - Local sandbox file ops now respect provider start/stop lifecycle and cancellation instead of bypassing run context

3. `internal/sandbox/docker.go`
   - Docker `ReadFile` now rejects directory reads clearly
   - Docker `WriteFile` now returns parent-dir creation and `chmod` errors instead of silently swallowing them

### Tests updated/added

- `internal/config/config_test.go`
  - Added coverage proving explicit subagent default fields survive `ApplyDefaults()`
- `internal/sandbox/provider_test.go`
  - Updated local provider file-op tests to start the provider before use
- `internal/tools/tools_test.go`
  - Updated scaffold overwrite assertion to match the new default `TOOLS.md` content

### Verification run

- `go test ./...`

### Files modified

- `handoff.md`
- `internal/agentdocs/scaffold.go`
- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/runtime/engine.go`
- `internal/sandbox/docker.go`
- `internal/sandbox/provider.go`
- `internal/sandbox/provider_test.go`
- `internal/tools/agent_tools.go`
- `internal/tools/tools_test.go`

### Notes for next person

- Prompt behavior is now easier to tune in one place: `internal/runtime/engine.go` for runtime docs and `internal/agentdocs/scaffold.go` for seeded agent docs
- If you want the prompts shorter, the next high-value pass is token-efficiency: trim repeated tool examples/tool-name lists while preserving the stronger behavioral instructions
- The working tree should now be ready to commit as one combined prompt+hardening change
