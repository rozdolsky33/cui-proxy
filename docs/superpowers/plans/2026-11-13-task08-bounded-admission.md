# Task 08 Bounded Admission Implementation Plan

> Use superpowers:executing-plans inline, with one fresh final branch review.

**Goal:** bound proxy work and helper queues while preserving text progress.
**Architecture:** process-wide weighted admission with two FIFO lanes and reserved small-request capacity; separate bounded helper admission.
**Tech Stack:** Go 1.26, net/http, existing metrics and Python workflow harness.
**Spec:** docs/superpowers/specs/2026-11-13-task08-bounded-admission.md

## Global Constraints
Eight active/eight queued requests; 64 MiB input reservations; small <=2 MiB; reserve two slots/8 MiB from bulk; five-second queue timeout. Helpers two active/two queued; existing 30-second extraction deadline. No original forwarding on overload; preserve policy and audit gates. Capture test/fixture/report output privately. Primary checkout remains clean main.

## Review Focus
1. Cancellation racing a grant cannot leak or double-release capacity.
2. Unknown or false Content-Length cannot under-account allocated body bytes.
3. Full bulk capacity cannot block a small request; continuous small traffic cannot starve an eligible bulk head.
4. All handlers share process limits, and slow response streams remain charged.
5. Queue refusal remains fail-closed under passthrough and failed audit persistence.

### Task 1: Admission primitive and bounded helper queue
Files: internal/admission/limiter.go, limiter_test.go; internal/attachment/extract.go and queue tests.
Interface: New(Limits) *Limiter; Acquire(context.Context,int64) (func(),error); Snapshot() State. Limits carry Active,Queued,Bytes,SmallBytes,ReservedActive,ReservedBytes. State exposes active,queued,reserved input bytes. Package Requests is the shared default.
- [ ] Add bound, reservation/FIFO, cancellation/grant-race, impossible-weight and idempotent-release tests; run go test ./internal/admission, expect RED before implementation.
- [ ] Implement the mutex-protected two-lane queue with alternating eligible heads and checked resource accounting; same tests GREEN under race detector.
- [ ] Add helper queue saturation/cancellation test, observe RED; replace unbounded helper waiting with bounded admission, adapt existing slot tests; attachment package GREEN.

### Task 2: HTTP admission, diagnostics and observability
Files: internal/proxy/admission.go, proxy.go, admission_test.go; internal/observe/trace.go; internal/metrics/metrics.go, stages.go; internal/recovery/recovery.go.
Interface: admission occurs after route/native/encoding guards but before metadata/body processing; uses shared limiter, releases after serve returns. Add overload closed reason and admission_wait stage; metrics snapshot includes admission state.
- [ ] Add HTTP tests for refusal before body read, no forwarding under passthrough, known/unknown body accounting, multi-handler limits, cancellation and small-work progress. Run focused tests, expect RED.
- [ ] Implement guarded admission, bounded reads, Retry-After and guidance, fixed-cardinality gauges/stage. Tests GREEN, including existing guard/recovery tests.
- [ ] Commit coherent implementation after affected race suites pass.

### Task 3: Measurement, documentation and integration
Files: docs/admission.md; docs/verification-results/2026-11-13-task08-admission.md; roadmap and manual snapshot if needed.
- [ ] Run bounded mixed-traffic measurements and report all outcomes, latency and memory; compare with unchanged target, no discarded failures.
- [ ] Document exact bounds, unknown-length behavior and overload recovery; update roadmap status accurately.
- [ ] Full Go race, Python workflow, HTTP corpus, vet/staticcheck and manual checks all exit zero.
- [ ] Fresh whole-branch review; fix material findings with RED/GREEN proof, archive rulings.
- [ ] Push/PR, require passing CI, merge and sync clean main.
