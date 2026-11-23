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
- The test-first execution steps below are intentionally pending the explicit no-execution instruction; static preparation does not constitute red/green verification.

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
- [ ] After the parent releases the reference lock, run the focused tests with stdout/stderr captured locally and confirm the intended red failures using a reviewed preimplementation snapshot if available. Never claim that the current static preparation demonstrated red/green.
- [x] Prepare the helper implementation for static review.
- [ ] Run focused tests after review, capture all output, report only status/counts/test names, and fix meaningful failures.

### Task 2: Execute the real recovery sequence

**Files:**
- Create: `scripts/workflow/ocr_recovery_probe.py`

**Interfaces:**
- `run_probe(arguments, report)` uses real Environment, attachment cases, canonical requests, deferred text boundaries, and backend receipts.
- `main(argv=None)` requires explicit candidate SHA/source, producer commit/source, fixture directory, existing lock file, and a new private report file.

- [x] Prepare the baseline → private OCR removal → same-process missing-tool preflight → uncached image failure overlapping three text requests → launcher restoration → canonical image recovery sequence.
- [x] Prepare final evidence validation for candidate/producer binding, six audit records, five canonical receipts, three overlapping text successes, uncached image behavior, captured log privacy after shutdown, and cleanup.
- [x] Prepare fail-closed preservation of private captured failure logs before Harness removes its temporary environment, with safe metadata-only report labels/counts/digests.
- [ ] Parent independently reviews the static changes.
- [ ] After the soak ends, acquire the global lock and run focused tests followed by the full required native workflow suite sequentially with captured output.
- [ ] Commit the producer only after tests/review, then run the live probe with its clean producer identity under the same global lock discipline.
- [ ] Inspect the report locally, verify cleanup and all bindings, and retain only metadata evidence. Do not create a PR before tests and review.
