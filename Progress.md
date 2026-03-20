# Progress

## Status
Completed

## Tasks
- [x] Step 3: Update `providerEndpoint()` — add `openai_codex` and `anthropic` cases
- [x] Step 4: Update `resolveProviderAccess()` — OAuth token fallback + provider-specific headers
- [x] Step 4a: Add `isOAuthProvider()` helper
- [x] Step 4b: Add `applyProviderAuth()` helper
- [x] Step 5: Update `contextWindowForModel()` — 200k for anthropic, 128k for openai_codex
- [x] Step 5b: Update `providerModelsPath()` — `/v1/models` for anthropic, `""` for codex
- [x] Step 6: Update `ListProviderModels()` — codex fallback list + use `applyProviderAuth`
- [x] Step 7: Update `Generate()` — route `openai_codex` and `anthropic` before normalization
- [x] Step 7b: Update `doChatCompletionOnce()` — guard Bearer header for non-anthropic
- [x] Step 7c: Update `doStreamingChatCompletionOnce()` — same guard
- [x] Create `internal/runtime/codex.go`
- [x] Create `internal/runtime/anthropic.go`
- [x] Create `internal/runtime/codex_test.go`
- [x] Create `internal/runtime/anthropic_test.go`
- [x] `go build ./...` — clean
- [x] `go test ./... -count=1` — all 33 packages pass

## Files Changed
- `internal/runtime/model.go` — 7 surgical edits (see below)
- `internal/runtime/codex.go` — new file, Codex Responses API adapter
- `internal/runtime/anthropic.go` — new file, Anthropic Messages API adapter
- `internal/runtime/codex_test.go` — new file, 9 unit tests
- `internal/runtime/anthropic_test.go` — new file, 10 unit tests

## model.go Edit Summary

1. **`providerEndpoint()`** — added `case "openai_codex"` and `case "anthropic"` returning the new config fields.

2. **`resolveProviderAccess()`** — added OAuth token fallback block (reads `oauth/{provider}/access_token` from secret store when no traditional API key exists), added Anthropic header block (`x-api-key`, `anthropic-version`, `anthropic-beta`), added Codex header block (`OpenAI-Beta`, `originator`, optional `chatgpt-account-id`).

3. **`isOAuthProvider()`** — new helper returning true for `openai_codex` and `anthropic`.

4. **`applyProviderAuth()`** — new helper that sets either `x-api-key` (Anthropic) or `Authorization: Bearer` (everyone else), then iterates custom headers.

5. **`contextWindowForModel()`** — added 200k for `anthropic`, 128k for `openai_codex`.

6. **`providerModelsPath()`** — added `/v1/models` for `anthropic`, `""` for `openai_codex`.

7. **`ListProviderModels()`** — added early-return fallback list for `openai_codex` (`codex-mini-latest`, `o4-mini`, `o3`, `gpt-4.1`); replaced manual header-setting with `applyProviderAuth`.

8. **`Generate()`** — added protocol routing switch before message normalization (`openai_codex` → `generateCodexResponses`, `anthropic` → `generateAnthropicMessages`).

9. **`doChatCompletionOnce()`** and **`doStreamingChatCompletionOnce()`** — guarded `Authorization: Bearer` header behind `m.providerName != "anthropic"`.

## Notes
- `anthropic.go` imports `net/http` (required for `http.NewRequestWithContext`) — confirmed by gofmt pass.
- `messagecontent` is imported in `anthropic.go` for `Normalize()` call in `buildAnthropicMessages`.
- The `Thinking` field on `agent.ModelResponse` is `Thinking` (not `ThinkingText`) — corrected during implementation.
- All existing tests still pass (33/33 packages green).
