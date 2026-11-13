# Task 10 Vault Recovery Implementation Plan

> Use superpowers:executing-plans inline, with one fresh whole-branch review.

**Goal:** preserve model-visible mappings across process crashes and document recovery/resource limits.
**Architecture:** characterize periodic snapshot loss, then serialize successful encrypted checkpoints before upstream forwarding in file mode. Retain existing file format, shared vault and lifetime lock.
**Tech Stack:** Go 1.26 standard library; existing captured-output workflow harness.
**Spec:** docs/superpowers/specs/2026-11-13-task10-vault-recovery.md

## Global Constraints
No secrets or fixture contents in tool output. No mapping eviction, per-agent vaults, format changes or automatic key rotation. File-mode failure refuses forwarding; memory mode remains ephemeral. Snapshot success includes file and directory sync. Four callers; growth evidence at 1,000 and 10,000 mappings. Main stays clean; work in an isolated branch. Preserve security and quality gates; Task 08 remains open.

## Review Focus
1. Concurrent checkpoint, periodic save and shutdown cannot publish stale state or falsely advance the saved counter.
2. Cancellation or disk failure cannot let an unsaved synthetic reach the upstream, including header-only transformations.
3. A process crash after forwarding cannot make a formerly exposed synthetic restore to a different original.
4. Reload and cooperating agents share mappings while separate processes remain excluded by the lifetime lock.
5. Documentation distinguishes process-crash guarantees, storage failures, pre-upgrade lost history, unbounded retention and measured checkpoint costs.

### Task 1: Characterize and enforce the durability boundary
Files: cmd/cui-proxy/vault_recovery_test.go, app.go; internal/proxy/proxy.go and persistence_test.go; internal/vault/file.go and durability_test.go; docs/adr/0004-mapping-durability.md.
Interfaces: proxy.Deps.PersistMappings func(context.Context) error; App.persistMappings(ctx context.Context) error; App.saveIfChanged() error serialized by saveMu; vault.Save(path string) error must propagate directory-sync failures.
- [x] Add an app-level regression that inspects the encrypted snapshot inside the upstream handler, before shutdown or the periodic timer; require every forwarded synthetic to restore exactly. Observe RED on current periodic-only behavior.
- [x] Add failure/refusal/retry, pre-cancelled forwarding and concurrent checkpoint tests. Include header-only mapping persistence and memory-mode no-op assertions.
- [x] Add directory-sync error propagation coverage with a narrow internal injectable save helper; observe RED while the existing implementation ignores errors.
- [x] Implement the checkpoint callback before the forward/audit success boundary, value-free 503 vault failure, serialized change-counter handling and directory-sync propagation. Preserve existing disk format and shutdown/periodic retry.
- [x] Run captured go test -race ./internal/vault ./internal/proxy ./cmd/cui-proxy; expected exit zero. Record ADR alternatives and observed regression, then commit.

### Task 2: Recovery, collision safety and growth evidence
Files: cmd/cui-proxy/vault_recovery_test.go; internal/vault/recovery_test.go; scripts/workflow recovery diagnostic only if a separate driver is necessary.
Interfaces: consumes Task 1 pre-forward persistence contract and existing Vault Open/Save/Pseudonym/Real plus App lifecycle APIs.
- [ ] Add subprocess abrupt-exit/restart proof with parent-held expected mappings; verify restored tool arguments exactly and release of the lifetime lock after process exit.
- [ ] Add concurrent reload/growth and graceful restart coverage. Force a deterministic candidate collision to verify persisted assignments cannot be reused for a different original after reopening.
- [ ] Run four-caller growth to 10,000 mappings, compare every reopened mapping, and capture time/file-size/resource aggregates at 1,000 and 10,000. Avoid timing assertions in CI; retain reproducible parameters.
- [ ] Run captured affected race tests; expected exit zero and exact mapping counts. Commit tests/evidence helpers.

### Task 3: Operations contract and integration
Files: docs/vault-recovery.md, navigation/manual, verification results and roadmap; update Task 09 integrated status after its merge.
- [ ] Document saved versus ephemeral state, crash contract and failed-save behavior, shared-agent versus unrelated-workspace boundaries, lifetime lock, retention/no-eviction and safe retirement. Record measurement and storage limitations.
- [ ] Rebuild manual; run full Go race, Python workflow, vet/staticcheck, HTTP corpus and manual verification with captured output; expected all zero.
- [ ] Perform one fresh whole-branch review, fix material findings with RED/GREEN and affected/full gates; archive rulings and defer minors.
- [ ] Push/PR, require exact-head CI/security gates, merge and synchronize clean main; continue the roadmap.
