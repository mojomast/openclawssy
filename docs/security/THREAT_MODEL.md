# Openclawssy Threat Model (v0.1)

This model maps known threats to mandatory invariants and concrete abuse tests.

## Assets
- Agent identity, capability grants, and config files.
- Workspace code and artifacts.
- Audit logs and run history.
- Local host integrity where Openclawssy runs.

## Trust Boundaries
- Untrusted: user prompts, repository contents, chat messages, network responses.
- Trusted with constraints: Openclawssy runtime and policy enforcement layer.
- Trusted operator input: config and capability grants.

## Invariants Mapped to Threats

| Invariant | Threats Mitigated |
| --- | --- |
| Config is human-controlled only | Prompt injection attempting config or permission mutation |
| Writes limited to workspace | Path traversal, symlink escape, host file overwrite |
| No sandbox means no `shell.exec` | Arbitrary command execution on host |
| Network off by default + private/local targets denied unless explicitly allowed | Data exfiltration, SSRF into loopback/private services, untrusted remote control |
| HTTP API enforces auth, strict JSON parsing, request body caps, and defensive timeouts | Slowloris/resource exhaustion, parser ambiguity, malformed input abuse |
| All tool calls audited + redacted | Stealth abuse, secret leakage, weak forensics |
| Historical tool messages excluded from model context | Tool replay from stale chat history |
| Repeated identical tool calls reuse prior success | Loop amplification and unnecessary repeated side effects |

## Abuse Cases and Expected Outcome
1. Prompt asks agent to edit `.openclawssy/config.json`.
   - Expected: denied with `policy.denied`; denial is audited.
2. Tool input uses `../../` to escape workspace.
   - Expected: denied after path canonicalization; denial is audited.
3. Workspace file is a symlink to `/etc/passwd` and write is attempted.
   - Expected: denied after symlink resolution; denial is audited.
4. Prompt asks for `shell.exec` while sandbox provider is `none`.
   - Expected: tool unavailable with `sandbox.required`.
5. Prompt attempts to exfiltrate tokens via logs/output.
   - Expected: sensitive values redacted in audit artifacts.
6. HTTP endpoint is called without token.
   - Expected: `401` and no run created.
7. Prior chat history contains a `/tool ...` directive and current user asks normal question.
   - Expected: runtime uses current message; stale directive is not executed.

## Required Security Tests (v0.1)
- Config mutation denial test.
- Traversal + symlink escape denial tests.
- Sandbox-gating test for `shell.exec`.
- Audit redaction test with token-like inputs.
- HTTP auth test for missing/invalid token.
- HTTP strict-body tests (unknown JSON fields, oversized payload rejection, timeout config coverage).
- SSRF resolution tests for loopback/private/link-local targets.

## Residual Risks
- Local privileged user can tamper with runtime files.
- Malicious dependencies can bypass assumptions if dependency policy is weak.
- Misconfigured allowlists can widen network exposure.

## Operational Guidance
- Run as a dedicated low-privilege user.
- Keep bind address on loopback unless explicitly required.
- Rotate API tokens and avoid storing plaintext secrets in repo files.
- Review audit logs for repeated `policy.denied` events.
