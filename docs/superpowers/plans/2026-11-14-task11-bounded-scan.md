# Bounded Regex Scan Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline under standing authorization.

**Goal:** Reduce sparse-text detector work without changing matching semantics.
**Architecture:** Conservative immutable syntax metadata enables merged windows for the original regex. All unsupported patterns and inputs retain the original path.
**Tech Stack:** Go 1.26.11 standard regexp and regexp/syntax.
**Spec:** docs/superpowers/specs/2026-11-14-task11-bounded-scan.md

## Global Constraints

Width at most 256 bytes; minimum input 4096 bytes; coverage budget one eighth. Preserve policies, match ordering, captures, group overlap, refiners, numeric acceptance gates and captured-output verification.

## Review Focus

- Nullable leading alternatives may introduce nondigit starts: eligibility and differential tests.
- Unicode case folding and invalid UTF-8: conservative fallback tests.
- Slicing may alter boundary and overlapping matches: raw-index differential tests.
- Dense candidates may regress performance: budget fallback plus sparse/dense benchmarks.
- Capture/refiner semantics must survive offset translation: final-span comparisons.

### Task 1: Metadata and execution

Files: create internal/detect/bounded_scan.go and bounded_scan_test.go; modify regex.go and builtin.go.
Interfaces: boundedDigitWidth(*regexp.Regexp) int returns zero for fallback; boundedMatches(*regexp.Regexp,string,int) [][]int preserves FindAllStringSubmatchIndex.

- [x] Write TestBoundedDigitWidth with hand-derived positive widths and rejected nullable/unbounded/non-ASCII/anchor/native-prefix cases; run RED against fallback stub.
- [x] Implement bounded syntax analysis; run GREEN.
- [x] Write differential TestBoundedMatches plus FuzzBoundedMatches and capture/refiner integration tests. Prove a context/offset mutation fails.
- [x] Implement boundedMatches; wire only non-group detectors; run detector suite and race tests.
- [ ] Run sparse/dense and canonical microbenchmarks with exact-source attestation. Reject regressions or fix with preserved evidence.

### Task 2: Qualification and integration

- [ ] Run short mixed reference probe, then original full pair and remaining matrix if it passes. No concurrent heavy workloads.
- [ ] Run repository checks and one independent final whole-branch review; resolve material findings.
- [ ] Commit, push, PR, merge only after required checks; synchronize main. Preserve soak-harness branch and resume its remaining campaign/evaluator work.
