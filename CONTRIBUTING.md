# Contributing

Thanks for contributing to Openclawssy.

This project moves fast, but we still optimize for safety, debuggability, and predictable behavior.

## Contribution workflow

1. Fork and create a branch from `main`.
2. Make focused changes (feature/fix/docs), with tests where relevant.
3. Run local validation (below).
4. Open a PR with clear scope, rationale, and verification notes.

## Local setup

```bash
make build
./bin/openclawssy setup
./bin/openclawssy doctor -v
```

## Required validation

Run before opening a PR:

```bash
make fmt
make lint
make test
```

At minimum, `go test ./...` must pass.

## Dashboard/frontend contributions

Frontend code lives under:

- `internal/channels/dashboard/ui`

Run e2e checks for dashboard changes:

```bash
cd internal/channels/dashboard/ui
npm install
npm run e2e:install:linux
npm run e2e:test
```

If browser dependencies are already installed:

```bash
npm run e2e:install
npm run e2e:test
```

## How to add a tool

When adding a new tool, wire all layers in one change so behavior and policy stay consistent.

1. Add the tool handler and registration in `internal/tools/`.
   - Define `ToolSpec` with required args and argument types.
   - Enforce safety constraints in handler logic (path bounds, allowlists, limits, redaction).
2. Ensure capability enforcement applies (registry already calls policy checks).
3. Add canonical names/aliases in both:
   - `internal/runtime/model.go`
   - `internal/toolparse/parser.go`
4. Include the tool in runtime allowlist/docs surfaces:
   - `internal/runtime/engine.go` (`allowedTools`, runtime context docs, tool best-practices docs)
5. Add tests:
   - `internal/tools/tools_test.go` for handler behavior and policy denial
   - `internal/runtime/*_test.go` and `internal/toolparse/*_test.go` for wiring/alias parsing
6. Update docs:
   - `README.md` examples
   - `docs/TOOL_CATALOG.md`
   - related docs/specs when config/API behavior changes

## PR quality bar

- Keep PRs reviewable: one theme per PR.
- Explain the "why" in the PR description.
- Include manual verification steps for UX/runtime changes.
- Add migration notes when behavior/config defaults change.

## Safety expectations

- Keep workspace boundaries strict for file-like operations.
- Do not leak secret values in logs, audit records, UI, or error messages.
- Prefer explicit limits (timeouts, pagination, response size caps) for potentially unbounded operations.
- Preserve backwards-compatible aliases when renaming tool names.
- Maintain deny-by-default posture for new capabilities.
