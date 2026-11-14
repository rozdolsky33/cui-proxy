# Task 12 release evidence implementation plan

> **For agentic workers:** Use superpowers:executing-plans inline under standing autonomous authorization; one final whole-branch review.

**Goal:** Make release evidence and rollback safety explicit without claiming unfinished performance acceptance.
**Architecture:** Reuse existing workflow oracles and Go benchmarks; add bounded numeric comparison reporting and operator documentation.
**Tech Stack:** Go, Python standard library, existing GitHub Actions.
**Spec:** docs/superpowers/specs/2026-11-13-task12-release-evidence.md

## Global Constraints

Preserve fail-closed policies, metadata-only reports, existing CI/security gates and clean primary main. Capture fixture/test-runner output locally. Do not run local builds or benchmarks concurrently with reference measurements. Shared-runner timing is report-only. Task 08/11 remain open until original gates pass.

## Review Focus

1. Unsupported future vault formats must not encourage data loss or unsafe history reuse.
2. Missing, duplicate, malformed or non-finite benchmark samples must not produce a passing comparison.
3. CI must not expose raw fixture output, credentials or untrusted branch code to privileged tokens.
4. Source/build mismatches and missing long-run evidence must remain visibly unqualified.
5. Rollback instructions must preserve mappings and distinguish compatible restart from destructive retirement.

## Task 1: Safe recovery diagnostics

Files: internal/vault/file.go, internal/vault/recovery_guidance_test.go, docs/vault-recovery.md.
- [ ] Add a regression for unsupported format guidance: startup refuses, no vault/key removal advice, preserves file bytes, recommends compatible binary and retained state.
- [ ] Run targeted test, observe actual RED; update wording only; targeted vault tests GREEN.
- [ ] Commit recovery guidance and evidence.

## Task 2: Bounded benchmark comparison reporting

Files: scripts/workflow/benchmark_comparison.py, scripts/workflow/test_benchmark_comparison.py, .github/workflows/ci.yml.
Interfaces: parser consumes captured Go benchmark lines; comparator consumes equally sized finite positive samples per benchmark and source identities; emits only names, counts, median ns/op, bytes/op, allocations/op and relative differences.
- [ ] Test malformed/duplicate/missing samples, unequal repetition counts, zero/non-finite baselines, preserved negative improvements, and allocation regressions.
- [ ] Observe RED, implement strict parsing/comparison, GREEN.
- [ ] Integrate a bounded base/candidate comparison after correctness gates; capture raw logs privately and upload safe aggregate artifact only. Keep timings report-only.
- [ ] Verify command failure and cancellation retain evidence and do not launch later work; unit tests and full workflow suite pass.

## Task 3: Operator release evidence

Files: docs/release-readiness.md, docs/vault-recovery.md, docs/install.md, roadmap completion records.
- [ ] Record release evidence identity, required gates, on-demand/pre-release long qualification, safe retention policy, compatibility matrix and measured limitations.
- [ ] Verify packaging/upgrade/running binary/dependency preflight/rollback against existing isolated correctness harnesses; no user installation modifications.
- [ ] Record all unmet acceptance items and reassess ratings without treating diagnostics as qualification.
- [ ] Run full appropriate regression/docs gates; independent review, one material RED/GREEN fix pass; authorized PR/CI/merge and clean-main sync.

## Sequencing ruling

While Task 11 timing runs, only plan/design and lightweight source inspection proceed here. Local builds/tests wait. The complete Task 12 milestone also depends on final Task 08/11 evidence; this branch must not close those gates by documentation.
