---
name: fullstack-worker
description: Builds features spanning both Go backend APIs and React frontend in a single coordinated effort
---

# Fullstack Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

Use for features that require **coordinated** backend API + React UI changes in a single feature, where splitting would create incomplete intermediate states. Examples:
- New API endpoint + its corresponding React page/component
- Config schema extension + Settings UI form fields
- Backend data model change that requires simultaneous frontend adaptation

## Work Procedure

### 1. Understand the Feature

Read the feature description, preconditions, expectedBehavior, and verificationSteps. This feature spans backend and frontend — understand both sides before starting.

Read:
- Relevant Go packages for the backend portion
- Relevant React components/pages for the frontend portion
- `.factory/library/` for architectural context
- API contracts: what endpoints exist, what shape responses have

### 2. Write Tests First — Both Layers

**Backend tests:**
1. Create/update Go test files with table-driven tests
2. Cover happy path, validation errors, edge cases
3. Run to confirm failure: `go test ./internal/<package> -run TestName -count=1`

**Frontend tests:**
1. Create/update Playwright e2e tests
2. Use API route mocking for the new endpoints
3. Cover page render, form interactions, data display, error states
4. Run to confirm failure: `cd internal/channels/dashboard/ui && npx playwright test tests/e2e/<file>.spec.ts`

### 3. Implement Backend First

Build the Go backend following backend-worker conventions:
- Struct definitions with JSON tags
- Validate-early pattern
- Handler registration in existing mux
- Error wrapping with context
- Config integration if needed

Verify backend works in isolation:
1. `go test ./internal/<package> -v -count=1` — all pass
2. `go vet ./...` — clean
3. `go build -o ./bin/openclawssy ./cmd/openclawssy` — compiles
4. Manual curl verification of endpoints

### 4. Implement Frontend

Build React components following frontend-worker conventions:
- TypeScript, shadcn/ui, Zustand
- API client integration for new endpoints
- Hash-based routing

Verify frontend:
1. `cd internal/channels/dashboard/ui && npx tsc --noEmit` — clean
2. `cd internal/channels/dashboard/ui && npx vite build` — succeeds
3. Playwright tests pass

### 5. Integration Verification

1. Build the full binary: `go build -o ./bin/openclawssy ./cmd/openclawssy`
2. Start the server: `./bin/openclawssy serve` (or use Docker)
3. Open the dashboard page that uses the new API
4. Verify end-to-end: UI → API → backend logic → response → UI update
5. Check console for errors, network tab for correct calls
6. Record each integration check as an `interactiveChecks` entry
7. Run full test suites: `go test ./...` and `cd internal/channels/dashboard/ui && npx playwright test`

### 6. Cleanup

- Stop any servers you started
- No orphaned processes
- Run `gofmt` check

## Example Handoff

```json
{
  "salientSummary": "Built the Delegation Policy Editor: backend PATCH endpoint for delegation config fields (mode, threshold, cooldown, auto_delegate) with validation + React page with form controls and save. All 8 Go tests and 4 Playwright tests pass; manually verified end-to-end that changing delegation mode via UI persists correctly.",
  "whatWasImplemented": "Backend: Added delegation config validation in internal/config/config.go, new PATCH handler fields for delegation settings. Frontend: DelegationPolicyEditor.tsx page with mode selector, threshold slider, cooldown input, auto-delegate toggle. Zustand delegationStore. API client methods. 4 Playwright e2e tests.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      { "command": "go test ./internal/config -run TestDelegation -v -count=1", "exitCode": 0, "observation": "8 tests passed" },
      { "command": "go vet ./...", "exitCode": 0, "observation": "Clean" },
      { "command": "go build -o ./bin/openclawssy ./cmd/openclawssy", "exitCode": 0, "observation": "Build succeeded" },
      { "command": "cd internal/channels/dashboard/ui && npx playwright test tests/e2e/delegation.spec.ts", "exitCode": 0, "observation": "4 tests passed" },
      { "command": "go test ./...", "exitCode": 0, "observation": "All tests pass" }
    ],
    "interactiveChecks": [
      { "action": "Opened /#/delegation, changed mode to 'approve_plan', clicked Save", "observed": "PATCH /api/admin/config sent with delegation fields, success toast shown" },
      { "action": "Refreshed page", "observed": "Mode shows 'approve_plan' — persisted correctly" },
      { "action": "Set threshold to -1, clicked Save", "observed": "Validation error: 'threshold must be >= 0'" }
    ]
  },
  "tests": {
    "added": [
      {
        "file": "internal/config/config_test.go",
        "cases": [
          { "name": "TestDelegationConfig_ValidModes", "verifies": "All 4 delegation modes accepted" },
          { "name": "TestDelegationConfig_InvalidMode", "verifies": "Unknown mode rejected with error" },
          { "name": "TestDelegationConfig_ThresholdBounds", "verifies": "Negative threshold rejected" }
        ]
      },
      {
        "file": "internal/channels/dashboard/ui/tests/e2e/delegation.spec.ts",
        "cases": [
          { "name": "renders delegation policy form", "verifies": "Page loads with current settings" },
          { "name": "saves mode change", "verifies": "Mode selection persists via API" },
          { "name": "validates threshold", "verifies": "Invalid threshold shows error" },
          { "name": "toggles auto-delegate", "verifies": "Toggle state persists" }
        ]
      }
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- Backend and frontend have conflicting requirements
- A shared component or API dependency doesn't exist
- Integration fails in a way that suggests architectural mismatch
- Requirements are ambiguous about data flow between layers
