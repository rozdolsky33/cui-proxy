# Live OCR Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce fail-closed live OCR recovery evidence on the exact c8 binary without altering real tools or sealed artifacts.

**Architecture:** Reuse the actual workflow environment, image canonical oracle, request checker, deferred text boundary checker, and backend receipt verifier. A private PATH of real extractor launchers supplies controlled removal and restoration; metadata-only evidence binds candidate and producer separately.

**Tech Stack:** Python standard library, existing workflow helpers, native Tesseract and Poppler, exact candidate binary.

**Spec:** `docs/superpowers/specs/2026-11-14-task11-soak-harness.md:544` and `docs/superpowers/specs/2026-11-23-live-ocr-recovery-design.md`.

## Global Constraints

- Do not run any tests, builds, extraction, benchmarks, profiling, or probes while the parent-controlled soak owns `reference-machine.lock`.
- Do not edit the primary checkout, frozen c8 source/artifact, or sealed evidence.
- Use only private launcher removal; preserve the real tools and all shared symlinks.
- Keep candidate binary identity separate from producer identity.
- Retain no fixture values, replacements, request IDs, raw errors, or raw audit records in the report or tool output.
- Initial preparation was static while the prior soak held the lock. The completed checks below ran after that controller stopped; no timed campaign ran concurrently.

## Review Focus

- A held or missing global lock must prevent every child process and extractor call.
- Cleanup must restore the private launcher after a failed assertion or exception.
- Canonical response success must also require actual upstream boundary receipts.
- Different fixture hashes and unchanged extraction cache counters must establish uncached behavior.
- Missing producer, privacy, audit, overlap, or cleanup proof must prevent a passing report.

### Task 1: Prepare private launchers and fail-closed assertions

**Files:**
- Create: `scripts/workflow/ocr_recovery_probe.py`
- Test: `scripts/workflow/test_ocr_recovery_probe.py`

**Interfaces:**
- `reference_lock(path)` opens and locks the existing non-symlink lock inode without truncation.
- `PrivateExtractors(directory, tools)` owns private regular launchers; `without_ocr()` always restores OCR.
- `check_missing_failure(status, raw)`, `check_overlap(rows, start, finish)`, `audit_summary(...)`, and `validate_evidence(report)` reject incomplete evidence.
- `bind_identity(...)` verifies the fixed c8 binary and a separately supplied clean producer commit.

- [x] Write tests for cleanup after exception, private PATH isolation, lock refusal, exact recovery guidance, all-three overlap, missing receipts, audit privacy, captured shutdown-log leaks, mandatory final log proof, and completeness gates.
- [x] After lock release, capture failing regressions before their fixes for private tool coverage, raw HTTP behavior and route, wire schema, and strict audit outcomes. Preserve the initial capture-path failures as well.
- [x] Prepare the helper implementation for static review.
- [x] Run focused tests after review with captured output; all 21 tests pass after fixing meaningful failures.

### Task 2: Execute the real recovery sequence

**Files:**
- Create: `scripts/workflow/ocr_recovery_probe.py`

**Interfaces:**
- `run_probe(arguments, report)` uses real Environment, attachment cases, canonical requests, deferred text boundaries, and backend receipts.
- `main(argv=None)` requires explicit candidate SHA/source, producer commit/source, fixture directory, existing lock file, and a new private report file.

- [x] Prepare the baseline → private OCR removal → same-process missing-tool preflight → uncached image failure overlapping three text requests → launcher restoration → canonical image recovery sequence.
- [x] Prepare final evidence validation for candidate/producer binding, six audit records, five canonical receipts, three overlapping text successes, uncached image behavior, captured log privacy after shutdown, and cleanup.
- [x] Prepare fail-closed preservation of private captured failure logs before Harness removes its temporary environment, with safe metadata-only report labels/counts/digests.
- [x] Complete independent source and retained-evidence review with no outstanding Critical or Important findings.
- [x] Acquire the global lock for sequential focused/native execution and the final full workflow suite; all 851 workflow tests pass with captured output.
- [x] Commit each producer after focused checks, bind the live run to its clean identity, and complete the full suite before PR creation.
- [x] Inspect the accepted report locally and verify cleanup, privacy, and bindings before PR creation. Retain failed attempts separately.

The [verification record](../../verification-results/2026-11-23-live-ocr-recovery.md)
records the accepted native sequence, aggregate results, and remaining scope.
