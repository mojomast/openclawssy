# PLAN: Upstream Rebase + Fork Replay Cleanup

Last revised: 2026-02-22
Owner: @shuv

## Objective

Create a clean, reviewable branch on top of `upstream/main` and reapply only the fork deltas we want to propose back upstream, with minimal noise and clear commit boundaries.

---

## Baseline Snapshot (as of plan creation)

- Current fork branch: `main`
- Divergence: `upstream/main...main` = `24 behind / 37 ahead`
- Patch-unique commits to evaluate (`git cherry -v upstream/main main`): `22`
- Known replay friction:
  - `323e811` conflicts because `internal/tools/network_safe.go` no longer exists in upstream.
  - `e3970cc` conflicts because it includes unrelated formatting-only changes in tool files.

---

## Success Criteria

- [ ] New replay branch is based on latest `upstream/main`.
- [ ] Reapplied commit stack is small, coherent, and free from accidental churn.
- [ ] `go test ./...` passes.
- [ ] `go test -race ./...` passes.
- [ ] `make ci-quick`, `make ci-security`, `make ci-race` pass.
- [ ] Docs are aligned with final code behavior.
- [ ] Branch is pushed and ready for a clean upstream PR.

---

## Phase 0 — Safety + Freeze

- [ ] Confirm clean working tree on `main`.
- [x] Tag rollback checkpoint.
- [x] Fetch latest refs.

Commands:

```bash
git switch main
git status --short
git tag backup/fork-main-pre-upstream-replay-2026-02-22
git fetch --all --prune
```

---

## Phase 1 — Commit Triage

Build triage from:

```bash
git cherry -v upstream/main main
git log --oneline --reverse upstream/main..main
```

### Triage Labels

- **KEEP-CHERRY-PICK**: cleanly replayable commit.
- **KEEP-REIMPLEMENT**: logic needed, but must be manually ported.
- **DROP**: already upstream / superseded / not PR target.
- **SPLIT**: mixed concerns; rewrite into multiple commits.

### Initial triage seeds

- [x] `bcd9cc1` — KEEP-CHERRY-PICK (`feat(ui): keyboard sidebar toggle and scheduler delete confirmation`)
- [x] `2c01d22` — KEEP-CHERRY-PICK (`feat(http): harden request handling and add health plus run filters`)
- [x] `323e811` — KEEP-REIMPLEMENT (network/setup hardening; port to upstream architecture)
- [x] `e3970cc` — KEEP-REIMPLEMENT (CI profiles; recreate without formatting-only noise)
- [x] `47a2b56` — KEEP-CHERRY-PICK (docs refresh; verify against final code)
- [ ] Remaining patch-unique commits — classify KEEP/DROP and document rationale.

---

## Phase 2 — Create Replay Branch

- [ ] Create branch from latest upstream.

Commands:

```bash
git switch -c cleanup/upstream-replay-1 upstream/main
```

---

## Phase 3 — Reapply Changes (Clean Stack)

### 3A) Apply clean cherry-picks first

- [ ] Cherry-pick UI commit.
- [ ] Cherry-pick HTTP hardening commit.

Commands:

```bash
git cherry-pick -x bcd9cc1
git cherry-pick -x 2c01d22
```

### 3B) Reimplement security/network commit (`323e811`)

- [ ] Port setup hardening in `cmd/openclawssy/main.go` + tests.
- [ ] Port network policy changes into current upstream network implementation (do not restore deleted files).
- [ ] Ensure config schema and config tool mutability align with upstream structure.
- [ ] Add/adjust tests for private/local egress restrictions.

Implementation notes:

- Upstream currently uses `internal/tools/network_tools.go` for request policy; integrate there.
- Preserve behavior intent:
  - stricter request safety
  - private/local network controls
  - explicit config flags + validation + tests

### 3C) Reimplement CI profile commit (`e3970cc`)

- [ ] Update `Makefile` with:
  - `fmt-check`
  - `ci-quick`
  - `ci-security`
  - `ci-race`
- [ ] Update `.github/workflows/ci.yml` to run those targets.
- [ ] Exclude unrelated formatting-only changes from the commit.

### 3D) Apply docs commit (`47a2b56`) last

- [ ] Cherry-pick docs commit.
- [ ] Manually reconcile any mismatch with final implementation.

Command:

```bash
git cherry-pick -x 47a2b56
```

---

## Phase 4 — Commit Hygiene

Target final commit stack:

- [ ] `feat(ui): keyboard sidebar toggle and scheduler delete confirmation`
- [ ] `feat(http): strict JSON, request caps, health endpoint, run filters/sort`
- [ ] `feat(security): setup validation + network egress hardening`
- [ ] `chore(ci): quick/security/race profiles and fmt-check gate`
- [ ] `docs: align contracts/usage/threat model with runtime behavior`

Rules:

- [ ] No formatting-only hunks unless intentional.
- [ ] No mixed concern commits.
- [ ] Each commit builds/tests in isolation when practical.

---

## Phase 5 — Validation Gates

Run all required checks:

```bash
go test ./...
go test -race ./...
make ci-quick
make ci-security
make ci-race
```

- [ ] Unit tests pass.
- [ ] Race tests pass.
- [ ] CI profile targets pass.

---

## Phase 6 — Push + PR Prep

- [ ] Push replay branch.
- [ ] Open PR against `mojomast/openclawssy:main`.
- [ ] Include conflict-port notes in PR body.
- [ ] Include test evidence in PR body.

Commands:

```bash
git push -u origin cleanup/upstream-replay-1
```

PR checklist:

- [ ] What was cherry-picked vs manually reimplemented.
- [ ] Why manual port was required (`network_safe.go` removal upstream).
- [ ] Test commands and results.
- [ ] Any behavior deltas from original fork commits.

---

## Risk Register

1. **Upstream changes during replay**
   - Mitigation: refetch + rebase replay branch before final push.
2. **Behavior drift in manual ports**
   - Mitigation: port tests first, then implementation.
3. **Noisy diffs**
   - Mitigation: use `git add -p`; avoid broad formatting sweeps.
4. **Docs mismatch**
   - Mitigation: docs commit applied last and cross-checked against endpoints/config.

---

## Execution Log

- 2026-02-22: Plan created.
- 2026-02-22: Created rollback tag `backup/fork-main-pre-upstream-replay-2026-02-22`.
- 2026-02-22: Fetched and pruned remotes (`origin`, `upstream`).
- 2026-02-22: Completed initial triage for target commits (`bcd9cc1`, `2c01d22`, `323e811`, `e3970cc`, `47a2b56`).
- [ ] Add dated notes here as phases complete.
