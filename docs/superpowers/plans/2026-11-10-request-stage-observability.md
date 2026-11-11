# Stage timing and safe workload correlation implementation plan

**Goal:** Complete roadmap Task 03 with bounded per-stage metrics and optional pseudonymous workload correlation.
**Spec:** docs/superpowers/specs/2026-11-10-daily-use-workload-contract.md and roadmap Task 03.
**Approach:** Add a request-scoped observation recorder shared through context with extraction. Publish fixed stage summaries, bounded size buckets and gauges through the existing metrics API. Correlation is disabled by default, explicitly enabled in configuration, and hashes reported workspace/session labels using an ephemeral process key. Labels never authorize or route requests.

## Timing contract

- ingress_read: body read only; metadata parsing/inspection has its own stage.
- metadata_inspect: adapter headers plus remaining header/query inspection.
- body_inspect: adapter request processing excluding the complete extraction phase.
- extraction_wait: waiting for the shared subprocess slot.
- extraction_run: extraction/decoding and subprocess processing excluding slot wait.
- audit_write: decision recording including lock/durability wait.
- upstream_wait: RoundTrip until response headers; does not include streamed body time.
- restore: response rewriter Event/Flush processing only.

Absent stages are absent samples, not measured zero. Stage times are not summed as full wall time; client/upstream streaming I/O remains outside these work spans. Existing overhead semantics remain compatible.

## Tasks

1. Test and implement the context recorder, fixed stage vocabulary, exclusive extraction accounting and optional HMAC correlation.
2. Test and implement bounded metrics rings, in-flight/extraction gauges, size buckets, and short-lived recent correlation records.
3. Instrument actual proxy/extraction boundaries with cancellation-safe cleanup; add controlled-delay and privacy/bounds integration checks.
4. Extend the deterministic harness to verify stage availability and delay attribution without printing fixture contents.
5. Document configuration/API semantics, measure instrumentation cost with a focused local benchmark, run race/lint/harness checks, and independently review.

## Constraints

No payload logs, dynamic metric labels, persistent correlation key, user-config changes, raw identifier forwarding changes, or policy relaxation. Default operation remains unattributed. Tests verify unknown/missing identifiers remain usable. Scope is metrics API; a new dashboard display is not required for this task. Performance results are instrumentation costs only, not Task 04 workload qualification.

## Fixed bounds and implementation sequence

Use eight fixed stages, at most 4,096 samples per stage, and no client-supplied metric labels. Optional recent correlation records will be capped at 256 and expire from the API after fifteen minutes. Process-local HMAC keys must never be persisted. Missing/invalid identity remains unattributed.

- [x] Request-scoped timing recorder and exclusive-extraction tests.
- [x] Bounded registry integration and resource gauges.
- [x] Proxy/extractor instrumentation and optional correlation configuration.
- [x] Harness delay attribution, privacy/retention checks and instrumentation-cost measurement.
- [x] Documentation, full verification and independent review.

## Completion — 2026-11-10

Task 03 is implemented: request-stage recording, bounded aggregation, extractor
resource gauges, optional correlation, configuration lifecycle, metrics API,
controlled harness probes, operator documentation and instrumentation benchmarks.
The initial recorder tests failed for missing implementation before passing;
the application reload regression failed before its startup wiring was added;
removing the audit timing span made its controlled-delay oracle fail.

## Implementation decisions and evidence

- Correlation uses explicit `session_id` and optional `X-Cui-Workspace-ID`
  headers, with exactly one 1–128-character ASCII token per header. No workspace
  is inferred. Raw values are never retained in metrics. Existing header
  inspection and forwarding still apply; identity does not authorize anything.
- Startup and successful config reload are the only writers of the correlation
  enabled state. Disabling clears history and invalidates generation tokens, so
  completions from an earlier enabled interval cannot repopulate it. UI snapshots
  can suppress correlation output but never re-enable it from stale config.
- Fixed stage reservoirs are copied under the registry lock and sorted after
  releasing it. No request-controlled metric series are created.
- The deterministic harness keeps its 125 workflow operations and separately
  reports two controlled-delay stage probes. Both passed; the three injected
  boundary faults are still rejected. Python oracle/lifecycle tests: 18 passed.
- Full Go race verification: all 18 packages passed; vet and staticcheck passed.
  A wrong exported-type comment found by staticcheck was corrected.
- Independent whole-branch review found no Critical/Important defects. Minor
  requests for explicit partial timing assertions after panic/cancellation and
  snapshot concurrency documentation were addressed in a focused test/docs wave.
  Four handler failure subcases pass the race detector; no production fix was needed.

### Local instrumentation cost

Measured with Go 1.26.10 on darwin/arm64, eight logical CPUs, three runs per
benchmark. Command: `go test ./internal/observe ./internal/metrics -run '^$'
-bench . -benchmem -count=3`, with output captured locally.

| Benchmark | Observed time per operation | Bytes / allocations |
| --- | --- | --- |
| All eight stages and aggregate recording | 1.136–1.154 µs | 768 B / 18 |
| Same, with two HMAC identities and recent record | 3.910–3.915 µs | 2,232 B / 42 |
| Snapshot of eight full 4,096-sample rings | 155.7–160.5 µs | 533,770 B / 32 |
| Minimal two-stage recorder/registry path | 336.8–356.5 ns | 288 B / 6 |

These benchmarks exercise bookkeeping with no real processing delay. They do
not measure end-to-end request cost, concurrent contention, large histories,
real OCR, or the workload contract's performance gates. Task 04 must measure
those on the complete proxy/bypass workloads. The snapshot benchmark uses
uniform samples and no recent correlation records.
