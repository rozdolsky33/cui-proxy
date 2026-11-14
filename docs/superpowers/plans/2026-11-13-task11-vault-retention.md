# Task 11 vault retention and interrupt plan

Use executing-plans inline, with standing autonomous authorization. Runtime base40e3122; current branch also includes the prior qualification plan/integration bookkeeping.
Spec:docs/superpowers/specs/2026-11-13-task11-vault-retention.md.

## Review Focus
1. Every retained real/reservation owns bounded storage, including duplicate admissions and failure reservations, without altering collisions or exact restoration.
2. String cloning adds no production unsafe code and does not change mapping/version persistence behavior.
3. Interrupt cleanup sets stop before joining workers, propagates BaseException and leaves no owned processes; in-flight timeout bounds remain explicit.
4. Interrupted evidence is not mislabeled as qualification; any reused bypass has unchanged generator/backend/input identity.
5. Allocation/resource gains are measured without weakening any original performance or security gate.

## Task 1: Reproduce and fix
- [x] Write vault storage-ownership regressions; observe actual RED.
- [x] Reproduce mixed-loop interrupt dispatch and startup barrier release with controlled real workers; observe actual RED.
- [x] Minimal fixes; targeted vault/transform/proxy race and300workflow tests GREEN (4opt-in skips).

## Task 2: Verify outcomes
- [x] Compare fixed-cardinality retained input and full-request allocation/latency with captured diagnostics.
- [x] Run full local regression gates and update interrupted-run evidence honestly.
- [ ] Independent whole-branch review; one material RED/GREEN fix pass, archive rulings.
- [ ] Commit/push/PR, pass exact-head CI, merge, synchronize clean main.
- [ ] Resume full-sample acceptance with provenance-checked baseline reuse or fresh arms.
