# AGENTS.md

## Purpose

This file is for coding agents working in `openclawssy`.
Prefer small, reviewable changes that preserve the project's safety-first runtime behavior.

## Repository Shape

- Primary language: Go.
- Main binary entrypoint: `cmd/openclawssy`.
- Core implementation lives under `internal/`.
- Dashboard assets and Playwright e2e tests live under `internal/channels/dashboard/ui`.
- Build/test automation is intentionally lightweight: `Makefile` + GitHub Actions.

## Rules Files Present

- No `.cursorrules` file found.
- No files found under `.cursor/rules/`.
- No `.github/copilot-instructions.md` file found.
- If any of those files are added later, treat them as higher-priority repository instructions.

## Default Workflow

1. Read the relevant package before editing.
2. Follow existing naming and validation patterns instead of inventing new abstractions.
3. Make the smallest safe change that solves the task.
4. Run focused tests first, then broader validation if the change touches shared behavior.
5. Do not weaken guardrails around policy, secrets, sandboxing, or path protection.

## Build Commands

- Build the main binary: `make build`
- Direct build equivalent: `go build -o ./bin/openclawssy ./cmd/openclawssy`
- Smoke the CLI after build: `./bin/openclawssy --help`
- Local diagnostics: `./bin/openclawssy doctor`

## Format And Lint Commands

- Format Go code: `make fmt`
- Direct format equivalent: `gofmt -w $(go list -f '{{ range .GoFiles }}{{ $$.Dir }}/{{ . }} {{ end }}' ./...)`
- Lint/vet: `make lint`
- Direct lint equivalent: `go vet ./...`
- CI also enforces formatting with `gofmt -l` on tracked `*.go` files.

## Test Commands

- Run all Go tests: `make test`
- Direct equivalent: `go test ./...`
- Run one package: `go test ./internal/runtime`
- Run one test in one package: `go test ./internal/channels/http -run TestServer_PostAndGetRun -count=1`
- Run one test by name across all packages: `go test ./... -run '^TestServer_PostAndGetRun$' -count=1`
- Run benchmarks when needed: `go test ./... -bench .`
- Run fuzz target explicitly: `go test ./internal/toolparse -run '^$' -fuzz Fuzz -fuzztime=10s`
- Live GLM test target: `make test-live-glm`
- Direct live GLM equivalent: `OPENCLAWSSY_LIVE_GLM=1 go test -tags liveglm -count=1 ./internal/runtime -run TestLiveGLMToolCallsCompleteWithoutLooping`

## Dashboard / E2E Commands

- Install Playwright browser only: `npm run e2e:install`
- Install Linux browser deps + browser: `npm run e2e:install:linux`
- Run dashboard e2e suite:
  - `cd internal/channels/dashboard/ui && npm run e2e:test`
- Run headed e2e suite:
  - `cd internal/channels/dashboard/ui && npm run e2e:test:headed`
- Run one Playwright file:
  - `cd internal/channels/dashboard/ui && npm run e2e:test -- tests/e2e/<file>.spec.js`
- Run one Playwright test by title:
  - `cd internal/channels/dashboard/ui && npm run e2e:test -- -g "test name"`
- Direct Playwright equivalent:
  - `cd internal/channels/dashboard/ui && npx playwright test tests/e2e/<file>.spec.js -g "test name"`

## CI Expectations

GitHub Actions currently runs:

1. `gofmt` check
2. `go vet ./...`
3. `go test ./...`
4. `go build -o ./bin/openclawssy ./cmd/openclawssy`
5. `./bin/openclawssy --help` and `./bin/openclawssy doctor`
6. Docker image build

If your change can break any of those, validate locally before finishing.

## High-Value Areas

- `internal/runtime/`: execution engine, tool orchestration, model integration, run tracing.
- `internal/tools/`: tool registration, capability checks, structured tool errors.
- `internal/config/`: defaults, validation, redaction, atomic persistence.
- `internal/channels/http/`: authenticated API surface, SSE events, request validation.
- `internal/channels/dashboard/`: admin UI handlers, scheduler/secrets/config management.
- `internal/policy/`: capability enforcement, path guarding, secret redaction.

## Code Style

### Formatting

- Use `gofmt`; do not hand-format Go files.
- Keep imports in standard Go grouping/order via `gofmt`.
- Prefer short, readable functions, but follow existing file organization when logic is already centralized.
- Preserve trailing newline at EOF.
- For JSON written to disk, use `json.MarshalIndent(..., "", "  ")` and append a newline when the file is human-edited.

### Imports

- Use standard library imports first, then internal module imports.
- Use aliases only when needed to avoid collisions or improve clarity, e.g. `httpchannel`, `memorystore`.
- Avoid unused helper imports introduced by speculative refactors.

### Naming

- Exported names use Go conventions; unexported helpers are lowerCamelCase.
- Prefer descriptive names over abbreviations unless the abbreviation is already idiomatic (`cfg`, `ctx`, `req`, `res`).
- Error codes and sentinel-style names are explicit and stable.
- Test names should describe behavior, not implementation details.
- When adding aliases or user-facing names, keep backward compatibility where the codebase already does so.

### Types And Data Shapes

- Prefer concrete structs for config, request payloads, and persisted data.
- Use `map[string]any` only at JSON/tool boundaries or other dynamic interfaces.
- Keep JSON struct tags explicit.
- When returning structured data, match existing field names and response shapes rather than inventing near-duplicates.
- Zero values are used intentionally in many configs; preserve that pattern.

### Control Flow

- Validate early and return early.
- Trim and normalize external input aggressively with `strings.TrimSpace`, `strings.ToLower`, and related helpers.
- Keep defaulting behavior close to input parsing.
- Prefer small helper functions for repeated normalization/validation logic.

### Error Handling

- Return errors; do not panic in normal runtime paths.
- Wrap errors with context using `fmt.Errorf("...: %w", err)`.
- Use `errors.Is` and `errors.As` for branching on known error types.
- Surface actionable, user-safe errors for HTTP/tool/CLI boundaries.
- Preserve structured error codes where they already exist.
- Never leak secret values in errors, logs, traces, audit events, or API responses.

### Context, Time, And Concurrency

- Thread `context.Context` through networked, long-running, or cancelable work.
- Use explicit timeouts for server shutdown, model calls, retries, and long-running operations.
- When using goroutines, keep cleanup explicit (`defer cancel()`, `defer Stop()`, close stores/providers carefully).
- Be careful with shared state; this repo uses mutexes/channels where needed instead of clever lock-free code.

### File And Config Safety

- Use atomic writes for persisted config/state when a helper already exists, e.g. `config.Save`, `WriteAtomic`.
- Respect workspace and control-plane boundaries.
- Do not bypass path guards, secret stores, or policy checks for convenience.
- Keep `.openclawssy` control-plane semantics intact.

### HTTP / API Conventions

- Validate method first.
- Decode JSON with explicit request structs.
- Return consistent JSON error payloads where a package already has helpers.
- Normalize IDs, pagination, and filter params before use.
- Default to loopback-safe behavior unless a config explicitly broadens access.

### Testing Conventions

- Prefer focused package tests before full-suite runs.
- Use table-driven tests when many input/output variations exist.
- Use `t.Helper()` in test helpers.
- Use `t.Setenv()` and `t.TempDir()` for isolation.
- Use `httptest` for HTTP behavior.
- Assert on observable behavior, status codes, persisted output, and structured fields.
- Add regression coverage for bug fixes, especially around policy, parsing, config validation, and secrets.

## Project-Specific Guidance

- Safety beats convenience.
- Deny-by-default behavior is intentional.
- Backwards-compatible aliases are important in tool parsing and runtime surfaces.
- Redaction is a feature, not polish.
- Many user inputs are intentionally normalized before validation; keep that behavior consistent.
- Prefer extending existing registries, validators, and helpers over introducing parallel systems.

## When Changing Tools

- Update handler/registration in `internal/tools/`.
- Update canonical names and aliases in both `internal/runtime/model.go` and `internal/toolparse/parser.go`.
- Update runtime allowlist/docs surfaces if the tool should be agent-callable.
- Add tests in tool, runtime, and parser layers as needed.
- Update docs such as `README.md` or `docs/TOOL_CATALOG.md` when behavior changes.

## Before Finishing

- Run `make fmt` if you changed Go code.
- Run the narrowest relevant `go test` command first.
- Run `make test` for shared/runtime/tooling changes when feasible.
- For dashboard changes, run the relevant Playwright coverage.
- Mention any validation you could not run.
