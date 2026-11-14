# Task 11 Stage Attribution Implementation Plan

> Use superpowers:executing-plans inline with one fresh whole-branch review.

**Goal:** expose checkpoint cost and distinguish text from image processing costs before another optimization.
**Spec:** docs/superpowers/specs/2026-11-13-task11-stage-attribution.md
**Architecture:** one appended closed observe stage; existing trace/metrics retention; value-free mixed-run stage grouping.

## Review Focus
1. Persistence still completes before audit success and upstream forwarding; failure/cancellation/panic behavior is unchanged.
2. Missing callbacks are unobserved, and every invoked callback records exactly one bounded numeric stage on all exits.
3. Text/image stage groups exclude warmups and cannot silently duplicate, drop or misattribute measured requests.
4. Reports retain only numeric stages, preserve missing-versus-zero distinction, and reject malformed coverage.
5. Evidence remains diagnostic and cannot close the original performance or four-hour soak gates.

### Task 1: Checkpoint stage
Files: internal/observe/trace.go, internal/proxy/proxy.go, affected observe/metrics/proxy tests, scripts/workflow/observation.py and tests, docs/dashboard.md.
- [x] Add callback outcome/absence and closed-stage tests; observe RED.
- [x] Append MappingCheckpoint and wrap only the existing callback with deferred measurement.
- [x] Extend closed observer stage metadata without relaxing validation. Affected race and workflow observation tests pass; commit.

### Task 2: Mixed-workload attribution
Files: scripts/workflow/performance_load.py, appropriate stats helper and tests, fairness report.
- [x] Add tests for exact text/image counts, warmup exclusion, missing/duplicate/invalid rows and older binary checkpoint absence; observe RED.
- [x] Produce numeric per-workload stage arrays and explicit coverage from existing correlated records; preserve extraction-only reports.
- [x] Run all workflow tests and captured short four-client diagnostic at the known failing768KiB size. Report text-only/checkpoint percentiles and limitations; commit.

### Task 3: Verification and integration
- [x] Record PR45 integration, diagnostic result and next evidence-based action; retain Task08/11/12 open items.
- [x] Full race, workflow, vet/staticcheck, HTTP corpus and manual verification.
- [ ] One fresh whole-branch review; one material fix pass with RED/GREEN; archive own rulings.
- [ ] Push/PR, pass exact-head remote gates, merge and sync clean main; continue next Task11 step.
