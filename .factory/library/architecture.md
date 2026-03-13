# Architecture

Architectural decisions, patterns, and conventions for the openclawssy operator-grade harness mission.

**What belongs here:** Architectural decisions, package organization, data flow patterns, design rationale.
**What does NOT belong here:** Service ports/commands (use `services.yaml`), env vars (use `environment.md`).

---

## Project Structure

- **Go backend**: Single binary at `cmd/openclawssy/`, core logic under `internal/`
- **Dashboard UI**: React SPA at `internal/channels/dashboard/ui/`, embedded via `go:embed`
- **Config**: `internal/config/config.go` — single Config struct, atomic persistence, validation
- **Runtime engine**: `internal/runtime/engine.go` — execution loop, tool orchestration, model calls
- **Agent system**: `internal/agent/` — run state, delegation, complexity scoring, decomposition
- **Tools**: `internal/tools/` — registry, builtins, capability checks
- **Policy**: `internal/policy/` — deny-by-default enforcement, path guards, secret redaction
- **Dashboard API**: `internal/channels/dashboard/handler.go` — admin API endpoints (3474 lines, single file)
- **Runtime API**: `internal/channels/http/server.go` — /v1/runs, /v1/chat, SSE events

## Key Patterns

### Config Resolution (Existing)
Global config → agent profile (zero-value fallback) → subagent restrictions (override + defaults merge).
`agentSubAgentRunnerAdapter.resolveRestrictions()` in engine.go handles the merge.

### Agent Delegation (Existing)
Complexity-driven: `ComputeComplexity()` scores → triggers at thresholds → modes: prompt_only (≥2), tool_gated (≥4), auto_execute (≥6). `DecomposeTask()` does pattern + signal based decomposition. Topological sort execution via `executeDelegatedTasks()`.

### Dashboard Embedding
UI assets compiled by Vite → output to dist/ → Go embeds via `//go:embed ui/*` in dashboard package → served at /dashboard/.

### API Authentication
Bearer token via middleware. Token from config (`OPENCLAWSSY_TOKEN` env or config file).

## New Subsystems (This Mission)

### Agent Contract (`internal/contract/`)
New package. `AgentContract` struct with 9 policy sections. `Resolver` merges global → agent → subagent with field-level source tracking. API endpoints on dashboard handler.

### Prompt Stack (`internal/promptstack/`)
New package. 5-layer model. `Assembler` merges layers. `VersionStore` tracks history. `Linter` checks for issues. API endpoints on dashboard handler.

### Typed Roles (`internal/roles/`)
New package. `RoleTemplate` struct with constraints. `Router` selects roles for tasks. Built-in templates + custom via config/API.

### Delegation Redesign
Extends `internal/agent/`. `Planner` generates `DecompositionPlan` with typed nodes, confidence, rationale. `DecisionLedger` records per-run decisions. New delegation modes.

### Eval Harness (`internal/eval/`)
New package. `Suite` definitions, `Runner` executes, `Metrics` computes, `Store` persists to SQLite. CLI via new `eval` subcommand.

## React Frontend Architecture

- **Framework**: React 18+ with TypeScript
- **Build**: Vite (outputs to dist/ for go:embed)
- **Routing**: React Router with HashRouter (/#/page for go:embed compatibility)
- **State**: Zustand (lightweight stores per domain)
- **Components**: shadcn/ui (Tailwind + Radix primitives)
- **Styling**: Tailwind CSS
- **API Client**: Centralized fetch wrapper with bearer auth
- **Testing**: Playwright e2e with API route mocking
