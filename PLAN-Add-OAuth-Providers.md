# Plan: Add OpenAI Codex OAuth and Anthropic OAuth Providers

## Goal

Add two new model providers to Openclawssy:

- `openai_codex` — ChatGPT/Codex OAuth-backed access to the Codex Responses API
- `anthropic` — Claude OAuth-backed access to the Anthropic Messages API

The design must fit the current codebase safely:

- preserve existing `chat/completions` providers unchanged
- avoid weakening secrets handling or dashboard auth boundaries
- support CLI login first, dashboard login second
- keep provider-specific protocol differences isolated behind small adapters
- include telemetry/logging for login, refresh, and failure flows

---

## Plan Review Summary

This plan is **feasible**, but the original draft had several codebase-alignment gaps that should be fixed before implementation.

### Critical revisions applied

1. **Dashboard auth endpoints belong in `internal/channels/dashboard/handler.go`, not `internal/channels/http/server.go`.**
   The generic HTTP channel server only exposes `/v1/*` run/chat APIs and does not own admin config/secrets flows. Existing admin/config/provider endpoints already live in the dashboard handler.

2. **Provider allowlists must be updated in multiple places, not just runtime config structs.**
   The current repo hardcodes provider names in several validation surfaces:
   - `internal/config/config.go` validation maps
   - `internal/channels/dashboard/handler.go` provider test/model allowlists
   - `cmd/openclawssy/main.go` doctor provider routing

3. **Provider-aware auth must be applied to both generation and model discovery.**
   `runtime.ListProviderModels()` currently always sends `Authorization: Bearer ...`. That will be wrong for Anthropic and incomplete for Codex.

4. **Anthropic message conversion must happen before the current generic role normalization.**
   Today `Generate()` remaps `tool` role messages to `user` globally. That would destroy the information needed to build Anthropic `tool_result` blocks.

5. **Dashboard manual OAuth needs pending-login session state.**
   The browser-less dashboard flow is multi-step, so the server must retain PKCE verifier/state/provider between “start login” and “complete login”.

6. **Telemetry is required.**
   Login start/success/failure, refresh success/failure, logout, and dashboard OAuth endpoint activity must be logged with safe structured fields and without token leakage.

### Approval status

**NEEDS REVISION → revised below**

---

## Current Codebase Alignment

Relevant current behavior:

- `internal/runtime/model.go` only builds OpenAI-compatible `POST /chat/completions` requests.
- `runtime.ListProviderModels()` always applies `Authorization: Bearer ...`.
- `config.Validate()` hardcodes supported providers.
- Dashboard provider tools and validation hardcode supported providers.
- `cmd/openclawssy/main.go` doctor/provider routing hardcodes supported providers.
- The secrets store already supports encrypted arbitrary key/value storage and is suitable for OAuth credential persistence.

So the implementation is not just “add two configs”; it requires:

- protocol routing
- provider-specific request building
- provider-specific auth application
- provider validation updates across the repo
- CLI and dashboard login UX

---

## Provider Facts

### OpenAI Codex

- OAuth authorize URL: `https://auth.openai.com/oauth/authorize`
- OAuth token URL: `https://auth.openai.com/oauth/token`
- API base: `https://chatgpt.com/backend-api`
- inference endpoint: `POST /codex/responses`
- auth style: `Authorization: Bearer <token>` plus `chatgpt-account-id`
- extra headers: `OpenAI-Beta: responses=experimental`, `originator: openclawssy`
- request format: Responses API (`instructions`, `input`, `tools`, etc.)
- transport: SSE

### Anthropic

- OAuth authorize URL: `https://claude.ai/oauth/authorize`
- OAuth token URL: `https://platform.claude.com/v1/oauth/token`
- API base: `https://api.anthropic.com`
- inference endpoint: `POST /v1/messages`
- auth style: `x-api-key: <token>`
- extra headers: `anthropic-version: 2023-06-01`, `anthropic-beta: oauth-2025-04-20`
- request format: Messages API (`system`, `messages`, `tools`, `max_tokens`)
- transport: JSON or SSE depending on `stream`

---

## Cross-Cutting Requirements

### Security

- Store OAuth credentials only in the encrypted secrets store.
- Never log access tokens, refresh tokens, authorization codes, or full callback URLs.
- JWT payload decoding for Codex is for extracting account metadata only, not for authorization decisions.
- Dashboard OAuth endpoints remain behind existing bearer auth.

### Telemetry / Logging

Minimum telemetry to add during implementation:

- `oauth.login.started`
- `oauth.login.completed`
- `oauth.login.failed`
- `oauth.refresh.started`
- `oauth.refresh.completed`
- `oauth.refresh.failed`
- `oauth.logout.completed`
- `oauth.dashboard.session.created`
- `oauth.dashboard.session.expired`

Each event should include safe fields such as:

- provider
- source (`cli` or `dashboard`)
- success/failure
- duration_ms
- error class/message (sanitized)

### Backward compatibility

Existing providers (`openai`, `openrouter`, `requesty`, `hatz`, `zai`, `openai_compat`) must continue to use the current `chat/completions` path with no behavior change.

---

## Revised Implementation Plan

### Step 1: Create `internal/oauth/` package

Add a dedicated OAuth package with small, testable primitives.

#### New files

- `internal/oauth/pkce.go`
- `internal/oauth/provider.go`
- `internal/oauth/openai_codex.go`
- `internal/oauth/anthropic.go`
- `internal/oauth/callback.go`
- `internal/oauth/manual.go`
- `internal/oauth/jwt.go`
- `internal/oauth/store.go`
- `internal/oauth/browser.go`
- `internal/oauth/pending.go` — pending dashboard login session storage with TTL

#### Responsibilities

- PKCE verifier/challenge generation
- provider registry and token exchange/refresh contracts
- local CLI callback server support
- manual paste parsing
- Codex account ID extraction from token payload
- secrets-store persistence for credentials
- short-lived pending-login session storage for dashboard manual flow

#### OAuth storage shape

Store durable credentials under names like:

- `oauth/openai_codex/access_token`
- `oauth/openai_codex/refresh_token`
- `oauth/openai_codex/expires_at`
- `oauth/openai_codex/account_id`
- `oauth/anthropic/access_token`
- `oauth/anthropic/refresh_token`
- `oauth/anthropic/expires_at`

Do **not** store pending verifier/state in the durable secrets store for dashboard flows; keep that in a TTL in-memory pending session store.

#### Tests

- PKCE generation
- JWT decode / account ID extraction
- manual input parsing
- callback validation and timeout
- credential save/load/delete
- pending session create/get/delete/expire

---

### Step 2: Extend config and validation for new providers

#### Modify `internal/config/config.go`

Add to `ProvidersConfig`:

- `OpenAICodex ProviderEndpointConfig`
- `Anthropic ProviderEndpointConfig`

Default values:

- `openai_codex.base_url = https://chatgpt.com/backend-api`
- `anthropic.base_url = https://api.anthropic.com`

Keep env names as optional escape hatches, but the intended path is OAuth-backed auth.

#### Important codebase alignment updates

Also update all supported-provider maps in:

- `Config.Validate()`
- agent profile provider validation
- dashboard config validation/messages
- any provider-name allowlists used by settings or tests

#### Tests

- defaults
- `ApplyDefaults()`
- redaction
- validation accepts the two new provider IDs

---

### Step 3: Introduce provider-aware auth application

The original plan mixed auth resolution into `resolveProviderAccess()`, but current call sites need something stronger because auth style differs by provider.

#### Add a small provider auth abstraction

Implement a runtime helper that can answer:

- base URL
- provider headers
- current access token
- optional Codex account ID
- optional refresh callback
- how to apply auth to an outgoing `http.Request`

For example, conceptually:

```go
type providerAccess struct {
    BaseURL      string
    Headers      map[string]string
    AccessToken  string
    AccountID    string
    Refresh      func(context.Context) (providerAccess, error)
    ApplyAuth    func(*http.Request)
}
```

Exact shape can vary, but **the key requirement is that auth application becomes provider-aware**.

#### Why this is required

Current repo behavior is incompatible with Anthropic because:

- generation hardcodes `Authorization: Bearer`
- model discovery hardcodes `Authorization: Bearer`

That must be replaced with provider-specific request auth.

#### Update call sites

Use the provider-aware auth helper in:

- provider model construction
- generation requests
- streaming requests
- model discovery requests
- any future health/test requests that need auth

---

### Step 4: Split generation into protocol-specific adapters

Refactor `ProviderModel.Generate()` so provider routing happens **before** the current OpenAI-compatible message normalization path mutates provider-specific semantics.

#### Required branch

```go
switch m.providerName {
case "openai_codex":
    return m.generateCodexResponses(ctx, req)
case "anthropic":
    return m.generateAnthropicMessages(ctx, req)
default:
    return m.generateChatCompletions(ctx, req)
}
```

#### Important revision

Do **not** run the current generic `normalizeProviderMessageRole()` path before the Anthropic adapter. Anthropic needs access to the original message roles and tool-result context.

---

### Step 5: Add OpenAI Codex Responses adapter

#### New file

- `internal/runtime/codex.go`

#### Responsibilities

- convert internal chat history to Responses API `input`
- map system prompt to top-level `instructions`
- convert tool schema format
- route requests to `/codex/responses`
- add Codex-specific headers
- support streaming SSE consumption using the existing parser where compatible
- add missing handling for:
  - `response.failed`
  - transport-level `error`

#### Notes

The existing SSE parser already covers much of the Responses event model, so request building and header routing are the main gaps.

#### Tests

- request translation
- URL resolution
- header application
- reasoning-effort clamping
- failed-event parsing

---

### Step 6: Add Anthropic Messages adapter

#### New file

- `internal/runtime/anthropic.go`

#### Responsibilities

- build Anthropic `messages` request bodies
- move system prompt to top-level `system`
- convert OpenAI-style tool schemas to Anthropic `input_schema`
- translate prior tool calls/results into Anthropic-compatible content blocks
- merge or normalize adjacent role runs as required by Anthropic request rules
- parse JSON responses into the repo’s internal `agent.ModelResponse`
- parse Anthropic SSE event stream for text deltas, tool use, stop reasons, and usage

#### Important revision

Tool-result handling must be explicit here. The current repo-wide “tool => user” normalization is not sufficient for Anthropic.

#### Tests

- request translation
- tool schema conversion
- tool result conversion
- text response parsing
- tool_use parsing
- SSE event parsing
- header behavior (`x-api-key`, no bearer auth)

---

### Step 7: Update runtime/provider plumbing everywhere it is hardcoded

#### Modify `internal/runtime/model.go`

Update:

- `providerEndpoint()`
- provider-aware auth lookup logic
- `providerModelsPath()`
- `contextWindowForModel()`
- request execution helpers so auth is applied through the new provider-aware mechanism

#### Modify other hardcoded provider surfaces

- `cmd/openclawssy/main.go` → `providerForDoctor()`
- `internal/channels/dashboard/handler.go` provider allowlists/messages
- any tests that assert exact provider lists

#### Optional model discovery fallback

Because Codex model discovery may be less stable than the existing providers, add a fallback static list for dashboard/CLI presentation if authenticated discovery fails or returns empty for oauth providers.

---

### Step 8: Add `openclawssy login` CLI command

#### New file

- `cmd/openclawssy/login.go`

#### Commands

- `openclawssy login openai_codex`
- `openclawssy login anthropic`
- `openclawssy login --list`
- `openclawssy login --logout <provider>`

#### Flow

For CLI:

1. generate PKCE
2. build authorize URL
3. start callback listener when appropriate
4. open browser if possible; otherwise print URL
5. accept callback or manual paste
6. exchange code
7. persist credentials
8. print safe success summary

#### Tests

- provider parsing
- list/logout behavior
- manual paste flow
- mock token exchange flow

---

### Step 9: Add auto-refresh in request paths

OAuth-backed providers should refresh on demand.

#### Behavior

- refresh before request when token is within provider-specific buffer
- on 401, attempt one refresh + retry
- surface actionable “please login again” guidance on unrecoverable refresh failure
- serialize refresh attempts to avoid token stampedes

#### Tests

- refresh before expiry
- 401-triggered refresh/retry
- refresh failure path
- concurrent refresh dedupe

---

### Step 10: Add dashboard OAuth admin endpoints

#### Important revision from original draft

Implement this in:

- `internal/channels/dashboard/handler.go`
- tests in `internal/channels/dashboard/handler_test.go`

**Do not put these in `internal/channels/http/server.go`.**

#### Recommended endpoints

- `POST /api/admin/oauth/{provider}/start`
  - creates pending login session
  - returns `{ session_id, authorize_url, expires_at }`
- `POST /api/admin/oauth/{provider}/complete`
  - body: `{ session_id, input }`
  - parses pasted URL/code, exchanges token, persists credentials
- `GET /api/admin/oauth/status`
  - returns provider login state and expiry metadata
- `DELETE /api/admin/oauth/{provider}`
  - deletes stored credentials

#### Why two-step start/complete is needed

Dashboard manual paste must preserve PKCE verifier/state across requests. A pending in-memory session with TTL is the simplest safe fit.

#### Optional UI follow-up

If dashboard users are expected to use this directly, add a settings UI section and Playwright coverage in a follow-up sub-step.

---

## Files to Create

### OAuth package

- `internal/oauth/pkce.go`
- `internal/oauth/pkce_test.go`
- `internal/oauth/provider.go`
- `internal/oauth/openai_codex.go`
- `internal/oauth/anthropic.go`
- `internal/oauth/callback.go`
- `internal/oauth/callback_test.go`
- `internal/oauth/manual.go`
- `internal/oauth/manual_test.go`
- `internal/oauth/jwt.go`
- `internal/oauth/jwt_test.go`
- `internal/oauth/store.go`
- `internal/oauth/store_test.go`
- `internal/oauth/browser.go`
- `internal/oauth/pending.go`
- `internal/oauth/pending_test.go`

### Runtime adapters

- `internal/runtime/codex.go`
- `internal/runtime/codex_test.go`
- `internal/runtime/anthropic.go`
- `internal/runtime/anthropic_test.go`

### CLI

- `cmd/openclawssy/login.go`

---

## Files to Modify

- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/runtime/model.go`
- `internal/runtime/model_test.go`
- `cmd/openclawssy/main.go`
- `internal/channels/dashboard/handler.go`
- `internal/channels/dashboard/handler_test.go`

Optional if UI is included now:

- dashboard UI settings/auth page files
- relevant Playwright tests

---

## Risk Mitigations

### Risk: provider auth behavior leaks into old providers

Mitigation:
- keep `generateChatCompletions()` path intact
- isolate new provider logic in dedicated adapter files
- add focused regression tests for existing providers

### Risk: dashboard pending sessions expire or are lost on restart

Mitigation:
- TTL-backed in-memory store is acceptable for first version
- return explicit “start login again” on missing/expired session

### Risk: refresh races

Mitigation:
- single-flight or mutex around per-provider refresh

### Risk: token/header leakage in logs

Mitigation:
- never log request headers or full callback URLs
- sanitize error payloads before recording telemetry

### Risk: Codex/Anthropic discovery quirks

Mitigation:
- provider-aware model discovery
- fallback static model list for oauth providers if needed

---

## Verification Checklist

### Unit / package tests

1. OAuth PKCE helpers
2. OAuth manual parse helpers
3. OAuth callback validation
4. OAuth durable credential storage
5. OAuth pending dashboard sessions
6. Codex request translation
7. Codex streaming error events
8. Anthropic request translation
9. Anthropic tool-result translation
10. Anthropic SSE parsing
11. provider validation accepts new providers
12. doctor/provider routing accepts new providers
13. dashboard provider allowlists accept new providers
14. model discovery uses provider-appropriate auth
15. existing providers still use chat/completions unchanged

### Focused integration checks

1. `openclawssy login openai_codex`
2. `openclawssy login anthropic`
3. `openclawssy login --list`
4. one Codex completion
5. one Anthropic completion
6. forced token refresh path
7. dashboard start/complete/logout flow
8. dashboard status endpoint
9. `openclawssy doctor` with each new provider configured

### Broader validation before merge

- `make fmt`
- focused `go test` packages first
- `make test` if shared runtime paths changed broadly

---

## Recommended Implementation Order

1. `internal/oauth/` primitives + tests
2. config/provider validation updates
3. provider-aware auth application abstraction
4. Codex adapter
5. Anthropic adapter
6. runtime plumbing + model discovery updates
7. CLI login
8. auto-refresh
9. dashboard admin endpoints
10. optional dashboard UI

---

## Final Readiness Assessment

After the revisions above, the plan is **ready to implement**.

The main thing to preserve during implementation is architectural separation:

- auth concerns in `internal/oauth/`
- protocol translation in provider-specific runtime adapters
- hardcoded provider lists updated consistently across config, doctor, runtime, and dashboard
- dashboard OAuth built on admin endpoints, not the generic `/v1` HTTP server
