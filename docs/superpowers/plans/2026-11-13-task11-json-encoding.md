# Task 11 JSON Encoding Implementation Plan

> Use superpowers:executing-plans inline with one fresh whole-branch review.

**Goal:** remove intermediate JSON string buffers while preserving exact output and reducing per-request allocation.
**Spec:** docs/superpowers/specs/2026-11-13-task11-json-encoding.md
**Baseline:** current main c4edb8d; PR46 integrated after all checks. Private profile and corpus evidence is under /private/tmp/cui-task11-request-sequence* and /private/tmp/cui-task11-changing-fixture.

## Review Focus
1. Direct string writes preserve encoding/json output with HTML escaping disabled for all byte sequences.
2. Removing the encoder newline affects only that call, preserving existing buffer prefixes and subsequent syntax.
3. Unchanged-node raw output, canonical mode, request/response restoration and fail-closed behavior remain unchanged.
4. Allocation and mixed-load evidence use comparable inputs, retain failures and show no material regression.
5. This allocation optimization does not waive the original performance or sustained-soak criteria.
6. Fragment counting saturates, metadata stays bounded, final output owns every byte, quoted fragments do not alias encoder pools, and discarded image input does not determine output capacity.

### Task 1: Direct string writer
- [x] Add exact differential prefix/suffix, Unicode/control/invalid-input tests and fuzz target; observe RED.
- [x] Add writeQuote using the existing standard encoder on the destination buffer; use it for keys/values and quote wrapper.
- [x] Affected adapter/transform/proxy race tests and20second differential fuzz pass; commit.

- [x] Reject two character-counting revisions after encoding-control latency regressions; retain evidence.
- [x] Verify bounded per-call standard-encoded fragment planning, ownership and differential output; affected race tests and 20-second differential fuzz pass.

### Task 2: Performance acceptance
- [x] Build identical private benchmark harnesses against reference/candidate; eight alternating equal-work pairs plus existing controls.
- [x] Require at least10% allocation reduction and no material latency regression: rotating allocation -22.783%, latency -0.438%.
- [x] Three short paired four-client768KiB mixed runs pass with exact stage coverage and zero failures; no full qualification claim.

### Task 3: Integration and continuation
- [x] Update PR46 integration and record evidence/limitations/next step.
- [x] Full race/workflow/lint/HTTP/manual checks.
- [ ] Fresh whole-branch review; one material RED/GREEN fix pass; archive own rulings.
- [ ] Push/PR, pass required exact-head gates, merge and sync clean main; continue Task11 acceptance work.
