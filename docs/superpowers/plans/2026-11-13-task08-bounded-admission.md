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
6. Detector workers preserve exact spans, blocking and tie order; worker panics reach caller recovery and release capacity. Unknown and cheap detector sets remain sequential, including policy wrappers.
7. Dynamic assignment cannot exceed the shared extra-worker budget or strand remaining detectors behind a slow scan. Memory and CPU evidence must include the bounded parallel candidate.

### Task 1: Admission primitive and bounded helper queue
Files: internal/admission/limiter.go, limiter_test.go; internal/attachment/extract.go and queue tests.
Interface: New(Limits) *Limiter; Acquire(context.Context,int64) (func(),error); Snapshot() State. Limits carry Active,Queued,Bytes,SmallBytes,ReservedActive,ReservedBytes. State exposes active,queued,reserved input bytes. Package Requests is the shared default.
- [x] Add bound, reservation/FIFO, cancellation/grant-race, impossible-weight and idempotent-release tests; run go test ./internal/admission, expect RED before implementation.
- [x] Implement the mutex-protected two-lane queue with alternating eligible heads and checked resource accounting; same tests GREEN under race detector.
- [x] Add helper queue saturation/cancellation test, observe RED; replace unbounded helper waiting with bounded admission, adapt existing slot tests; attachment package GREEN.

### Task 2: HTTP admission, diagnostics and observability
Files: internal/proxy/admission.go, proxy.go, admission_test.go; internal/observe/trace.go; internal/metrics/metrics.go, stages.go; internal/recovery/recovery.go.
Interface: admission occurs after route/native/encoding guards but before metadata/body processing; uses shared limiter, releases after serve returns. Add overload closed reason and admission_wait stage; metrics snapshot includes admission state.
- [x] Add HTTP tests for refusal before body read, no forwarding under passthrough, known/unknown body accounting, multi-handler limits, cancellation and small-work progress. Run focused tests, expect RED.
- [x] Implement guarded admission, bounded reads, Retry-After and guidance, fixed-cardinality gauges/stage. Tests GREEN, including existing guard/recovery tests.
- [x] Commit coherent implementation after affected race suites pass.

### Task 2b: Bounded detector parallelism from measured CPU evidence
Files: internal/detect/parallel.go, parallel_test.go, detect.go.
- [x] Observe RED for worker budget/fallback and panic containment contracts.
- [x] Implement at most three nonblocking extra workers; retain original detector order and caller panic recovery; race tests GREEN.
- [x] Fuzz exact spans and block decisions against sequential reference.
- [x] Compare HTTP latency and memory under the existing normal/mixed workloads; reject unsupported performance claims.

### Task 3: Measurement, documentation and integration
Files: docs/admission.md; docs/verification-results/2026-11-13-task08-admission.md; roadmap and manual snapshot if needed.
- [x] Run bounded mixed-traffic measurements and report all outcomes, latency and memory; compare with unchanged target, no discarded failures.
- [x] Document exact bounds, unknown-length behavior and overload recovery; update roadmap status accurately.
- [x] Full Go race, Python workflow, HTTP corpus, vet/staticcheck and manual checks all exit zero.
- [ ] Fresh whole-branch review; fix material findings with RED/GREEN proof, archive rulings.
- [ ] Push/PR, require passing CI, merge and sync clean main.

## Measured acceptance follow-up

The implementation can be integrated after correctness, security and CI gates pass, while roadmap Task 08 remains in progress if mixed first-byte p95 exceeds 150 ms. The unchanged target must be retested after subsequent measured optimizations, including conditional Task 09 extraction reuse. An incremental merge is not performance qualification or completion of roadmap Task 08. This avoids expanding admission into unbounded speculative optimization work.
