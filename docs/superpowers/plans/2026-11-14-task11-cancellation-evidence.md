# Task 11 Cancellation Evidence Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans inline under standing autonomous authorization; one fresh whole-branch review at the end. No routine approval questions.

**Goal:** Produce reproducible, bounded evidence for queued cancellation and active extractor-slot release against the optimized reference binary.

**Architecture:** Add a private gated OCR launcher, a pure acceptance evaluator, and an isolated diagnostic CLI that reuses the existing soak setup, original warmup and semantic/audit oracles. Preserve the intentionally incomplete collector root and publish only a distinct cancellation result.

**Tech Stack:** Python standard library, native proxy, existing Tesseract/corpus and workflow harness.

**Spec:** docs/superpowers/specs/2026-11-14-task11-cancellation-evidence-design.md

## Global Constraints

- Queued cancellation <=1 second; active extraction slot release <=2 seconds; each operation <=45 seconds.
- Three distinct trials; original normal four-client warmup >=60 seconds and >=100 successes.
- No heavy local execution while reference measurements are live. Pure sub-second tests and source work are permitted.
- Preserve production limits, fail-closed behavior, original evidence and clean primary main.
- Private mode0700 directories/mode0600 artifacts; no raw fixtures, policies, messages or logs in tool output.
- Run one final whole-branch review; fix confirmed defects with regressions and required verification. No re-review loop.

## Review Focus

1. Gauges or transport closure can appear successful while a queued helper later starts; require helper and upstream-absence evidence.
2. Reused PIDs, stale/symlinked gate markers or concurrent dispatch can bind the wrong request; validate identity and admission order.
3. A wrapper that reads stdin can break real OCR or leave children behind; preserve bounded bytes and exec the absolute real helper.
4. Oracle polling can distort measured cancellation time; separate transport/server bounds from later semantic verification.
5. Exceptions during setup, cancellation or restoration must retain evidence and leave no instrumentation, workers or owned services running.

### Task 1: Gate and acceptance primitives

**Files:** Create scripts/workflow/cancellation_gate.py, cancellation_acceptance.py, test_cancellation_gate.py, test_cancellation_acceptance.py.

**Interfaces:** GatedOCR(directory, delegate) context manager owns the launcher, arm(), release(), and snapshot() metadata. evaluate_trial(trial) checks observed timing, queue states and identified helper/slot continuity; returns bounds_passed and always cancellation_qualified=false/performance_qualified=false. Complete evidence composition belongs to Task 2.

- [x] Add failing tests for the exact 1/2/45-second boundaries, missing/non-finite observations, ambiguous helper/request binding, premature headers, lost gate state and a transport exceeding 45 seconds.
- [x] Add failing tests for bounded stdin preservation, absolute delegation, immediate version delegation, stale/symlink markers and restoration on error; defer subprocess-heavy tests until a real gap.
- [x] Implement the smallest primitives satisfying those tests; run focused tests and commit.

### Task 2: Isolated native diagnostic runner

**Files:** Create scripts/workflow/cancellation_probe.py and test_cancellation_probe.py, with cancellation_audit.py and test_cancellation_audit.py for pure audit attribution, plus cancellation_transport.py and focused/native transport tests, and cancellation_evidence.py/test_cancellation_evidence.py for bound trial composition, and cancellation_qualification.py/test_cancellation_qualification.py for complete artifact acceptance; reuse existing soak and request helpers without changing production defaults.

**Interfaces:** run_probe(options) owns setup/identity, thread-local connection tracking, original warmup, three trial sequences, evidence binding and cleanup. evaluate_cancellation(report, expected_identity) combines all three trial bounds with the existing provenance/warmup oracles and receipt/audit, privacy and cleanup evidence; only that fully bound result may set cancellation_qualified=true, while performance_qualified remains false. CLI accepts the existing runtime/source/seed/corpus identity options and a fresh output root; no paid/live service or installation changes.

- [ ] Add failing fake-transport/state-sequence tests for queue cancellation, identified active-helper termination/new slot admission, unrelated connection exclusion, unexpected success, upstream receipt presence, ambiguous audit differences and incomplete cleanup at every failure point.
- [ ] Implement the collector phase hook and measurement sequence; preserve actual observed outcomes and distinguish oracle time from cancellation time.
- [ ] Verify baseline/recovery requests with existing semantic/receipt oracles, request-ID audit matching, privacy and owned-process checks; commit.

### Task 3: Native verification and integration

**Files:** Add a verification record under docs/verification-results/; update this plan/ledger.

- [ ] In a verified quiet gap, run the full workflow suite and the original-warmup three-trial native diagnostic against the frozen optimized binary. Preserve failures; fix reproduced defects without weakening thresholds.
- [ ] Validate all original cancellation criteria, identities, privacy and cleanup. Do not claim full performance/soak qualification.
- [ ] Run required docs/regression checks and one independent final branch review; commit/push/PR, fix CI, merge only after required checks pass, synchronize primary main.

## Self-review

The tasks cover every design requirement and all five review-focus cases. The collector root deliberately remains incomplete, while the separately bound cancellation artifact owns only the cancellation claim. The owner already selected autonomous inline execution; implementation proceeds without another approval exchange.
