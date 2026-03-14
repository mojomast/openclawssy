---
name: backend-worker
description: Builds Go backend features including schemas, APIs, engines, CLI commands, and tests
---

# Backend Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

Use for features involving:
- Go struct definitions and schema design
- API endpoint handlers (admin dashboard API or runtime API)
- Engine logic (resolution engines, assembly engines, routing logic)
- CLI subcommands (eval harness)
- Go unit and integration tests
- Config validation and defaults

## Work Procedure

### 1. Understand the Feature

Read the feature description, preconditions, expectedBehavior, and verificationSteps carefully. Read `mission.md` and `.factory/library/` for architectural context.

Read the existing code in the relevant package to understand:
- Existing patterns for struct definitions, validation, defaults
- How similar endpoints are registered and handled
- Test patterns used in the package
- Import conventions

### 2. Write Tests First (TDD)

Before writing implementation code:

1. Create or update test file(s) in the relevant package (e.g., `internal/contract/contract_test.go`)
2. Write table-driven tests covering expectedBehavior cases
3. Use `t.Helper()`, `t.TempDir()`, `t.Setenv()` for isolation
4. For HTTP endpoints: use `httptest.NewServer` or `httptest.NewRecorder`
5. Run tests to confirm they fail (red phase): `go test ./internal/<package> -run TestName -count=1`

If the feature arrives with pre-implemented staged changes from a prior interrupted run, treat strict red-phase ordering as best-effort: add/adjust regression tests first where practical, then run focused + full validators and clearly document this deviation in handoff.

**Test coverage requirements:**
- Happy path for each expected behavior
- Error/validation cases
- Edge cases (empty input, nil values, boundary conditions)
- For resolution/merge logic: test inheritance, override, fallback

### 3. Implement

Follow these conventions from the codebase:

**Struct design:**
- Concrete structs with explicit JSON tags
- Zero values are used intentionally in configs
- Validation happens in dedicated `Validate()` methods
- Defaults set in `ApplyDefaults()` or `Default()` functions

**API handlers:**
- Validate method first (`if r.Method != http.MethodGet { ... }`)
- Decode JSON with explicit request structs
- Return consistent JSON error payloads using existing helpers
- Normalize/trim input with `strings.TrimSpace`, `strings.ToLower`
- Register routes in the existing mux setup

**Error handling:**
- Return errors, never panic
- Wrap with context: `fmt.Errorf("resolve contract for %s: %w", agentID, err)`
- Use `errors.Is`/`errors.As` for branching
- Never leak secrets in errors

**Config integration:**
- New config sections go in `internal/config/config.go`
- Add defaults in `Default()` function
- Add validation in existing validation flow
- Add `ApplyDefaults()` logic for zero-value fallback

**CLI commands:**
- Follow existing pattern in `cmd/openclawssy/main.go`
- Use raw `os.Args` dispatch matching existing style
- Create service struct wrapping `*runtime.Engine`

### 4. Verify

1. Run focused tests: `go test ./internal/<package> -run TestName -count=1 -v`
2. All tests must pass (green phase)
3. Run `go vet ./...` — must pass with no warnings
4. Run `gofmt -l $(find . -name '*.go' -not -path './vendor/*')` — no files should be listed
5. Run `go build -o ./bin/openclawssy ./cmd/openclawssy` — must compile
6. For API features: use `curl` to manually verify endpoint behavior against the running server
7. Run broader test suite: `go test ./...`

### 5. Manual Verification

For API endpoints:
1. Start the server if needed: `./bin/openclawssy serve`
2. Use `curl` to test each endpoint behavior
3. Verify response shape, status codes, error cases
4. Record each check as an `interactiveChecks` entry

For CLI commands:
1. Build: `go build -o ./bin/openclawssy ./cmd/openclawssy`
2. Run the command with various arguments
3. Verify output, exit codes, error messages
4. Record each check as an `interactiveChecks` entry

If mission guidance explicitly allows non-interactive/manual-skip validation for the current feature, provide equivalent evidence-backed checks (targeted automated command/API probes plus logs/artifacts) and document the substitution in `interactiveChecks`.

## Example Handoff

```json
{
  "salientSummary": "Implemented AgentContract schema and resolution engine in internal/contract/. Added GET /api/admin/agents/{id}/resolved endpoint. Resolution merges global config → agent profile → subagent overrides with field-level source tracking. All 14 tests pass; verified via curl that resolved contract returns complete state with correct inheritance.",
  "whatWasImplemented": "internal/contract/contract.go (AgentContract struct with 9 policy sections), internal/contract/resolver.go (resolution engine merging 3 layers with source annotations), internal/contract/resolver_test.go (14 table-driven tests), handler registration in internal/channels/dashboard/handler.go for GET /api/admin/agents/{id}/resolved.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      { "command": "go test ./internal/contract -v -count=1", "exitCode": 0, "observation": "14 tests passed, including inheritance, override, and fallback cases" },
      { "command": "go vet ./...", "exitCode": 0, "observation": "No warnings" },
      { "command": "go build -o ./bin/openclawssy ./cmd/openclawssy", "exitCode": 0, "observation": "Build successful" },
      { "command": "go test ./...", "exitCode": 0, "observation": "All package tests pass" }
    ],
    "interactiveChecks": [
      { "action": "curl -H 'Authorization: Bearer change-me' http://localhost:8081/api/admin/agents/default/resolved | jq .", "observed": "200 OK, complete contract with all 9 policy sections, each field has 'source' annotation" },
      { "action": "curl http://localhost:8081/api/admin/agents/nonexistent/resolved", "observed": "404 with {\"error\": \"agent not found\"}" },
      { "action": "curl http://localhost:8081/api/admin/agents/default/resolved | jq '.model_policy.source'", "observed": "\"global\" — confirming inheritance from global config" }
    ]
  },
  "tests": {
    "added": [
      {
        "file": "internal/contract/resolver_test.go",
        "cases": [
          { "name": "TestResolve_GlobalDefaults", "verifies": "Agent with no overrides gets all global defaults" },
          { "name": "TestResolve_ProfileOverride", "verifies": "Agent profile values override global defaults" },
          { "name": "TestResolve_SubagentInheritance", "verifies": "Subagent inherits from parent agent with additional restrictions" },
          { "name": "TestResolve_ZeroValueFallback", "verifies": "Zero-value fields fall back to parent level" },
          { "name": "TestResolve_SourceTracking", "verifies": "Each field's inheritance source is correctly annotated" }
        ]
      }
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- Feature depends on another package/API that doesn't exist yet
- Config schema changes conflict with existing validation
- Existing tests break due to schema changes and the fix is non-obvious
- Requirements are ambiguous about resolution precedence or merge behavior
- The feature needs concurrent access patterns not covered by existing mutex/channel usage
