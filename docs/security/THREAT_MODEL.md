# Openclawssy Threat Model (v0.2)

This model maps known threats to mandatory invariants and concrete abuse tests.

## Assets
- Agent identity, capability grants, and config files.
- Workspace code and artifacts.
- Audit logs and run history.
- Local host integrity where Openclawssy runs.
- Docker container and volume contents (when docker provider is active).

## Trust Boundaries
- Untrusted: user prompts, repository contents, chat messages, network responses.
- Trusted with constraints: Openclawssy runtime and policy enforcement layer.
- Trusted operator input: config and capability grants.
- Trusted infrastructure: Docker daemon socket (host-level trust).

## Invariants Mapped to Threats

| Invariant | Threats Mitigated |
| --- | --- |
| Config is human-controlled only | Prompt injection attempting config or permission mutation |
| Writes limited to workspace | Path traversal, symlink escape, host file overwrite |
| No sandbox means no `shell.exec` | Arbitrary command execution on host |
| Network off by default | Data exfiltration and untrusted remote control |
| All tool calls audited + redacted | Stealth abuse, secret leakage, weak forensics |
| Historical tool messages excluded from model context | Tool replay from stale chat history |
| Repeated identical tool calls reuse prior success | Loop amplification and unnecessary repeated side effects |
| Docker: `/workspace` prefix enforced at two layers | Container path traversal, host fs access via docker provider |
| Docker: secrets never in container env | Secret exfiltration via `docker inspect` or `env` inside container |
| Docker: network=none by default | Container-initiated data exfiltration |
| Docker: agentID sanitized before use in container/volume names | Shell injection via agentID, path traversal in container names |

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
8. (Docker) fs.write path is `/workspace/../../etc/shadow`.
   - Expected: `validateContainerPath()` rejects before reaching container; `dockerResolvePath()` also blocks at engine layer.
9. (Docker) agentID supplied as `../../etc/passwd`.
   - Expected: `sanitizeDockerName()` converts to `______etc_passwd`; no path component reaches Docker CLI.
10. (Docker) Caller inspects container environment for API keys.
    - Expected: `docker exec <container> env` returns no secret-looking values; secrets are only in the model API call headers.

## Required Security Tests
- Config mutation denial test.
- Traversal + symlink escape denial tests.
- Sandbox-gating test for `shell.exec`.
- Audit redaction test with token-like inputs.
- HTTP auth test for missing/invalid token.
- Docker: `validateContainerPath` unit tests covering null bytes, `..`, Windows paths, absolute non-workspace paths.
- Docker: container env inspection shows no secrets.
- Docker: agentID injection test (`sanitizeDockerName`).

## Residual Risks
- Local privileged user can tamper with runtime files.
- Malicious dependencies can bypass assumptions if dependency policy is weak.
- Misconfigured allowlists can widen network exposure.
- Docker socket is trusted at host level; compromising the socket = full host access.
- Container image supply chain: image must be operator-trusted; no image signing enforced.
- Shared kernel: Docker provides process isolation, not VM isolation; no custom seccomp profile is applied beyond Docker defaults.

## Operational Guidance
- Run as a dedicated low-privilege user.
- Keep bind address on loopback unless explicitly required.
- Rotate API tokens and avoid storing plaintext secrets in repo files.
- Review audit logs for repeated `policy.denied` events.
- When using docker provider: mount `/var/run/docker.sock` only to the control-plane process, not agent containers.
- When using docker provider: set `sandbox.docker.network_enabled=false` (default) unless outbound access is explicitly required.
