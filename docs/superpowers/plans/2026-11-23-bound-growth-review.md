# Bound Growth Review Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an explicit, current-run independent investigation close only the positive fixed-state growth review gate.

**Architecture:** A new helper captures an external review and its investigation without writing collection evidence. It binds them to completed-campaign seals, identities, and exact recomputed resource results, then supplies a narrowly scoped gate to the existing evaluator and CLI.

**Tech Stack:** Python standard library and existing strict campaign JSON/canonical encoding helpers.

**Spec:** `docs/superpowers/specs/2026-11-14-task11-soak-harness.md` and `docs/superpowers/specs/2026-11-23-bound-growth-review-design.md`.

## Global Constraints

- Do not execute tests, workflow imports, builds, extraction, profiling, probes, or benchmarks while the ongoing soak holds the global reference lock.
- Do not change frozen c8 source/artifact, primary checkout, OCR producer checkout, or ongoing/sealed collection evidence.
- Preserve the original 10% fixed-resource limits and every existing quality gate.
- Do not create a review or acknowledgement for the ongoing campaign.
- Keep all source changes in `/private/tmp/cui-growth-review-evaluator`.
- Native verification is recorded below; no unit test result qualifies a live campaign.

## Review Focus

- A review for an older campaign must not bind to new seals or identities.
- A review cannot waive a failed numeric gate or any other incomplete/failed gate.
- Missing reviewer independence, investigation, limitations, or producer provenance must fail closed.
- Changed, malformed, symlinked, oversized, or escaped citation files must be rejected without printing contents.
- Legacy nonpositive growth behavior and evaluator-source cleanliness must remain intact.

### Task 1: Capture and validate the independent review

**Files:**
- Create: `scripts/workflow/soak_growth_review.py`
- Test: `scripts/workflow/test_soak_growth_review.py`

**Interfaces:**
- `GrowthReview` carries immutable captured review/investigation bytes.
- `load_growth_review(path, root)` securely captures an external review and its declared investigation.
- `growth_review_context(campaign, expected_identity, resource_result)` returns current-run bindings, observed growth, and window medians without producing any decision.
- `evaluate_growth_review(..., prerequisites, required_prerequisites)` returns a metadata-only gate; defaults and missing inputs fail closed.

- [x] Obtain approval of the written design before implementing the helper.
- [x] Write tests for valid current-run acceptance, absent review, nonpositive legacy behavior, stale bindings, failed limits/prerequisites, missing investigation/provenance/narrative, and secure file capture.
- [x] Preserve a test-only baseline snapshot outside the active source for later red execution.
- [x] Prepare the minimal helper implementation; execution remains pending.
- [x] Parent ran captured baseline red (two missing-feature import errors) and current green (15 tests) under the reference lock.

### Task 2: Wire the existing evaluator and CLI

**Files:**
- Modify: `scripts/workflow/soak_acceptance.py`
- Modify: `scripts/workflow/soak_acceptance_cli.py`
- Test: `scripts/workflow/test_soak_growth_review_cli.py`

**Interfaces:**
- `evaluate_completed_soak(campaign, expected_identity, *, growth_review=None)` preserves existing callers.
- CLI adds optional `--growth-review PATH`; no input preserves the existing incomplete-positive-growth result.

- [x] Write plumbing tests for the optional input, external-file requirement, retained digests, and legacy caller behavior.
- [x] Evaluate review approval after all non-review gate results exist; require the complete unchanged prerequisite gate set to pass.
- [x] Retain only review/investigation hashes, bindings, reviewer digest, and declared manual-producer digests in acceptance output; retain verified machine-context source separately as evaluator provenance.
- [x] Preserve clean-source and new-output checks, and never mutate sealed evidence.
- [x] Independent static review cleared Critical/Important findings; parent ran 15 targeted and 866 full workflow tests (four skips), documentation contract and strict MkDocs checks.
- [ ] Freeze and integrate the verified evaluator; acknowledge positive growth only after independent investigation of the new completed campaign.
