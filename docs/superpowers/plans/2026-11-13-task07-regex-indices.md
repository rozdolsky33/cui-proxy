# Task 07 Regexp Prefilter Implementation Plan

> Use superpowers:executing-plans for inline implementation.

Goal: reduce measured detector cost without changing any detection semantics.
Architecture: derive a necessary ASCII literal and precheck non-group matching only.
Tech stack: Go 1.26, existing Go benchmarks and Python HTTP performance harness.
Spec: docs/superpowers/specs/2026-11-13-task07-regex-indices.md

## Global Constraints

One production optimization only. Preserve group overlap, refiners, finders,
policy tags, fail-closed behavior and all quality gates. No request/body cache.
Capture fixtures, reports and test output locally; emit safe metadata only.
Continue review/commit/PR/merge under standing authorization.

## Review Focus

1. Capturing custom regexes must preserve whole-match leftmost semantics.
2. Group-based overlapping secrets must stay on their existing capture path.
3. Unicode, invalid UTF-8, empty matches and anchors must remain equivalent.
4. Blocking tags and overlap resolution must not differ from reference.
5. Benchmarks must compare identical work and not hide regressing workloads.

### Task 1: Differential and allocation contracts

Files: internal/detect/indices_test.go and internal/detect/regex.go.
Interfaces: test-only referenceFind/referenceScan preserve the old API path;
production Detector interface unchanged.
- [x] Add differential tests across builtin/custom rules, profiles and edge inputs.
- [x] Add TestRequiredLiteralConservative and observe RED for unsafe inferred literals.
- [x] Implement bounded syntax-derived required literals; retain the reference regexp matching API.
- [x] Observe GREEN; run differential fuzzing and package tests.
- [x] Add paired Scan reference/candidate benchmarks for normal/stress payloads.

### Task 2: Measure end-to-end behavior

Files: existing scripts/workflow harness (no semantic changes); verification report.
Interfaces: base binary at 11f4c99 versus candidate binary; same workload recipe.
- [x] Inspect existing performance runner controls and record bounded paired plan.
- [x] Run alternating reference/candidate normal/stress workloads and compare noise.
- [x] Run semantic/policy HTTP corpus and full Go race/Python/vet/staticcheck gates.
- [x] Accept only evidence-backed improvement; record any workload regression.

### Task 3: Review and integrate

Files: roadmap and docs/verification-results/2026-11-13-task07-regex-indices.md.
- [ ] Record evidence, invariants, limitations and acceptance decision.
- [ ] Fresh whole-branch review; fix material findings and reverify.
- [ ] Commit/push/PR, wait for required CI, merge and synchronize clean main.
