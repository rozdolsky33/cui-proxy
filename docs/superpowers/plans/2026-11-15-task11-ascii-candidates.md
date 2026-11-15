# Exact ASCII Candidate Scanners Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans inline; perform one independent final branch review before integration.

**Goal:** Reduce built-in candidate scanning cost while preserving exact detection and blocking semantics.

**Architecture:** Dedicated stateless scanners replace two fixed built-in regex candidate languages. Existing validators and custom regex paths remain unchanged.

**Tech Stack:** Go 1.26, testing, regexp differential oracle, fuzzing and race detector.

**Spec:** [ASCII candidate design](../specs/2026-11-15-task11-ascii-candidates.md).

## Constraints and review focus

Preserve greedy runs, one optional card separator, minimum IPv6 candidate length, byte offsets around Unicode/invalid UTF-8, distinct downstream validators and blocking tags. Keep all numerical acceptance thresholds and failed evidence. Never print fixture values or raw test failures.

## Implementation and verification

- [x] Add differential tests in `internal/detect/ascii_candidates_test.go` and demonstrate the missing scanner implementation.
- [x] Implement `ipv6Candidates` and `cardCandidates` in `internal/detect/ascii_candidates.go`; connect only the corresponding built-ins through an optional matcher field in `regexDetector`.
- [x] Verify exhaustive short strings, boundary/Unicode/invalid-byte cases, fuzz parity and integration equivalence with the original regex path, including blocking detectors.
- [x] Measure alternating request benchmarks and a short mixed workload before spending the complete qualification campaign.
- [x] Run race tests, lint and required repository checks; document actual results and limitations.
- [ ] Obtain one final review, commit/push/PR, fix CI, merge after required gates and synchronize main.
- [ ] Freeze the reviewed runtime and rerun unchanged full qualification; do not reuse old-binary samples as new-binary evidence.

Evidence: [scanner verification](../../verification-results/2026-11-15-task11-ascii-candidates.md).
