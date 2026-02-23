# PLAN: PR #36 Copilot Review Follow-up

Last revised: 2026-02-22
PR: https://github.com/mojomast/openclawssy/pull/36
Branch: `cleanup/upstream-replay-1`

## Objective

Process all Copilot review comments in PR #36, apply the valid hardening changes, and explicitly document rationale for any alternatives.

---

## Review Comment Triage

### 1) `internal/tools/network_tools.go` — Proxy bypass risk via `ProxyFromEnvironment`

**Comment summary**
- With env proxies enabled, DNS/IP safety checks can apply only to proxy endpoint, not final target.

**Decision**: **APPLY**

**Change**
- Set `Transport.Proxy = nil` in `createSafeTransport`.
- Behavior: `http.request` no longer honors env proxy settings.

**Rationale**
- `http.request` is a security-constrained tool; destination IP validation must apply to final destination.
- Proxy usage can undermine SSRF protections unless separately modeled.

---

### 2) `internal/tools/network_tools.go` — Over-blocking when any resolved IP is restricted

**Comment summary**
- Existing logic denied host if *any* DNS result was restricted.
- Could fail dual-stack/multi-A hosts even when safe IPs exist.

**Decision**: **APPLY**

**Change**
- Added `filterAllowedIPs(ips, cfg)`.
- Dial now iterates over allowed IPs sequentially and returns success on first connect.
- Returns blocked error only when no allowed IPs remain.

**Rationale**
- Preserves security policy while avoiding avoidable false negatives.
- Better operational behavior on mixed IPv4/IPv6 DNS records.

---

### 3) `internal/tools/network_tools.go` — Missing non-global-unicast edge cases

**Comment summary**
- Ensure broadcast/unspecified/multicast and similar non-unicast addresses are blocked.

**Decision**: **APPLY**

**Change**
- Updated `isRestrictedIP`:
  - loopback controlled by `allow_localhosts`
  - private/link-local-unicast controlled by `allow_private_networks`
  - multicast/unspecified/link-local-multicast always blocked
  - any other non-global-unicast blocked

**Rationale**
- Closes gaps (e.g., limited broadcast) while preserving explicit override semantics only where safe.

---

### 4) `internal/channels/http/server.go` — Length side-channel in `secureTokenEquals`

**Comment summary**
- Early return on length mismatch is not strict constant-time behavior.

**Decision**: **APPLY**

**Change**
- Rewrote `secureTokenEquals` to:
  - compute constant-time length equality with `subtle.ConstantTimeEq`
  - compare padded buffers using `subtle.ConstantTimeCompare`
  - return true only if both length and content checks succeed

**Rationale**
- Better aligns implementation with constant-time intent.

---

### 5) `internal/tools/network_safe_test.go` — Missing edge-case IP tests

**Comment summary**
- Add test coverage for unspecified/multicast/broadcast categories.

**Decision**: **APPLY**

**Change**
- Expanded `TestIsRestrictedIP_DefaultPolicy` coverage for:
  - unspecified v4/v6
  - multicast v4/v6
  - limited broadcast v4
- Added:
  - `TestIsRestrictedIP_AlwaysBlocksNonGlobalUnicast`
  - `TestFilterAllowedIPs`
  - `TestCreateSafeTransportDisablesProxy`

**Rationale**
- Protects against regression in safety-critical IP classification.

---

### 6) `Makefile` — `test-security` regex misses new security tests

**Comment summary**
- `make ci-security` wasn’t exercising some security-relevant tests.

**Decision**: **APPLY**

**Change**
- Expanded regex in `test-security` to include:
  - `Strict|Body|Timeout|RestrictedIP`

**Rationale**
- Ensures security profile executes the newly added hardening tests.

---

## Items Not Applied (and why)

None. All six Copilot comments were addressed with concrete changes.

---

## Validation

Run after changes:

- `go test ./...`
- `go test -race ./...`
- `make ci-security`
- `make ci-quick`
- `make ci-race`

All passed.

---

## Files touched in follow-up

- `internal/tools/network_tools.go`
- `internal/tools/network_safe_test.go`
- `internal/channels/http/server.go`
- `internal/channels/http/server_test.go`
- `Makefile`
- `docs/TOOL_CATALOG.md` (note about proxy behavior)

---

## Next step

- Commit follow-up changes.
- Push branch.
- Reply in PR thread summarizing each addressed comment.
