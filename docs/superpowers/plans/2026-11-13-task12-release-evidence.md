# Task 12 release evidence implementation plan

> **For agentic workers:** Use superpowers:executing-plans inline under standing autonomous authorization; one final whole-branch review.

**Goal:** Make release evidence and rollback safety explicit without claiming unfinished performance acceptance.
**Architecture:** Reuse existing workflow oracles and Go benchmarks; add bounded numeric comparison reporting and operator documentation.
**Tech Stack:** Go, Python standard library, existing GitHub Actions.
**Spec:** docs/superpowers/specs/2026-11-13-task12-release-evidence.md

## Global Constraints

Preserve fail-closed policies, metadata-only reports, existing CI/security gates and clean primary main. Capture fixture/test-runner output locally. Do not run local builds or benchmarks concurrently with reference measurements. Shared-runner timing is report-only. Task 08/11 remain open until original gates pass.

## Review Focus

1. Unsupported future vault formats and rollback must preserve mapping state and prevent unsafe history reuse.
2. Missing, duplicate, malformed or non-finite benchmark samples must not produce a passing comparison.
3. CI must not expose raw fixture output, credentials or untrusted branch code to privileged tokens.
4. Source/build mismatches and missing long-run evidence must remain visibly unqualified.
5. Zombie-only signal groups must not replace interruption with permission errors; live or unverifiable groups must still fail cleanup.

## Task 1: Safe recovery diagnostics

Files: internal/vault/file.go, existing TestOpenOldVersionClearError in internal/vault/vault_test.go, docs/vault-recovery.md.
- [x] Update unsupported-format recovery wording to preserve vault/key and require a compatible binary; startup behavior remains unchanged.
- [x] Verify existing format-refusal and vault tests in CI. No new exact-copy test for this wording-only change; such a test would mirror implementation rather than verify new behavior.
- [x] Commit recovery guidance and evidence.

## Task 2: Bounded benchmark comparison reporting

Files: scripts/workflow/benchmark_comparison.py, scripts/workflow/test_benchmark_comparison.py, .github/workflows/ci.yml.
Interfaces: parser consumes captured Go benchmark lines; comparator consumes five finite samples per benchmark per arm (positive ns/op; nonnegative bytes/op and allocations/op) and source identities; emits only names, counts, median ns/op, bytes/op, allocations/op and relative differences.
- [x] Test malformed/duplicate/missing samples, unequal repetition counts, zero/non-finite baselines, preserved negative improvements, and allocation regressions.
- [x] Observe RED, implement strict parsing/comparison, GREEN.
- [x] Integrate a bounded base/candidate comparison after correctness gates, using `BenchmarkRequestWalk` with 10 iterations per invocation and five alternating base/candidate pairs; capture raw logs privately and upload safe aggregate artifact only. Keep timings report-only.
- [x] Verify command failure and cancellation retain evidence and do not launch later work; unit tests and full workflow suite pass.

## Task 3: Operator release evidence

Files: docs/release-readiness.md, docs/vault-recovery.md, docs/install.md, roadmap completion records.
- [x] Record release evidence identity, required gates, on-demand/pre-release long qualification, safe retention policy, compatibility matrix and measured limitations.
- [ ] Verify packaging/upgrade/running binary/dependency preflight/rollback against existing isolated correctness harnesses; no user installation modifications.
- [ ] Record all unmet acceptance items and reassess ratings without treating diagnostics as qualification.
- [ ] Run full appropriate regression/docs gates; independent review, one material RED/GREEN fix pass; authorized PR/CI/merge and clean-main sync.

## Sequencing ruling

While Task 11 timing runs, only plan/design and lightweight source inspection proceed here. Local builds/tests wait. The complete Task 12 milestone also depends on final Task 08/11 evidence; this branch must not close those gates by documentation.

## Infrastructure integration boundary

This branch delivers report-only CI comparison, safer recovery wording and release-evidence documentation. Final measured operating-envelope publication and the roadmap Task12 completion claim remain dependent on Task08/11 acceptance. They are explicitly deferred to the final evidence update, not waived by this infrastructure PR.

## Confirmed cleanup follow-up

Files: scripts/workflow/performance_campaign.py, test_campaign_signal_groups.py, test_performance_campaign.py. A real Darwin zombie-only process group reproduces the old PermissionError; the candidate passes. Successful nonempty, well-formed process-table evidence is required before treating a denied group as stopped. Live or unknown states retain failure. Tests cover signal/exit races, malformed/unavailable observations, exact interruption propagation, and the real kernel condition. Ten repeated real interruption trials passed; the temporary repeated loop was removed to preserve routine CI cost.
