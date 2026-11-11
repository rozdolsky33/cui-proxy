# Concurrent performance baseline implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task by task.

**Goal:** Measure where the complete proxy spends time and memory under one, four, eight and sixteen concurrent clients, without weakening correctness or policy.

**Architecture:** Extend the existing isolated Python workflow environment with a separate performance runner and deterministic workload generator. Reuse the semantic/boundary oracles and the stage metrics API. Add Go microbenchmarks for component attribution; compare actual proxy and bypass requests in matched separate runs.

**Tech stack:** Go 1.26 testing/pprof; Python standard library; existing local OCR/PDF tooling for separately labelled real-extraction runs.

**Spec:** `docs/superpowers/specs/2026-11-10-daily-use-workload-contract.md`, especially sections 3, 5–8; roadmap Task 04.

## Global constraints

- Preserve fail-closed policies and every correctness oracle. A fast failure is not a successful completion.
- Capture all fixture, runner, tool and profile output locally according to AGENTS.md. Commit only safe metadata summaries; never payloads, raw identifiers, credentials or profiles.
- Use the contract's seeded populations and normal/stress tiers exactly. The compact two-regex correctness corpus is not the normal-tier performance population.
- Primary checkout remains clean on current main; all work belongs to this isolated branch.
- CPU/allocation profiles are diagnostic local artifacts. No production debug endpoint is enabled or exposed.
- Performance qualification requires repeated runs; short smoke measurements must remain explicitly unqualified. Existing numerical gates are not relaxed to fit results.

## Approach decisions

Use a dedicated performance runner alongside the correctness harness. Turning the compact correctness suite into a benchmark would conflate intentionally failing scenarios and incomparable request types; replacing it would lose established semantic checks. Keep Go microbenchmarks complementary: they isolate costs but cannot establish end-to-end latency or agent fairness.

## Review focus

1. Failed or timed-out operations must remain in denominators and outcome counts.
2. Pair first-byte samples by seeded operation identity before subtraction; never subtract aggregate percentiles. Retain negative differences as scheduler jitter.
3. Proxy and bypass runs must use identical backend pacing and offered load, with achieved concurrency measured independently.
4. Cold/warm state, vault growth and extraction tool versions must be recorded; stale fixtures cannot silently change the tier.
5. Reporting and profiling must not retain raw values or claim a gate passed when a required measurement is absent.

## Task 1 — Paired statistics and report contract

**Files:** create `scripts/workflow/performance_stats.py`, `scripts/workflow/test_performance_stats.py`.
**Interface:** `summarize_pairs(proxy, bypass)` takes lists of records with `operation_id` and numeric `first_byte_ms` fields and returns count, p50/p95/p99 paired differences. `summarize_outcomes(records)` reports attempted/completed/supported-success/failure counts without dropping unsuccessful requests. Report records use fixed metadata fields only.

- [x] Add failing tests for pairing independent of input order, negative differences, duplicate/missing sample IDs, non-finite samples, and timeout inclusion.
- [x] Implement strict sample validation and nearest-rank quantiles; incomplete pair sets fail qualification rather than being silently discarded.
- [x] Run captured unittest discovery; expect all oracle/statistics tests to pass.
- [x] Record schema and units in `scripts/workflow/README.md`; commit the tested statistics increment.

## Task 2 — Representative workloads and controlled component benchmarks

**Files:** create `scripts/workflow/performance_workload.py`, `scripts/workflow/test_performance_workload.py`; add benchmark files beside detection, adapter request walking and transformation/restoration implementations.
**Interface:** generator returns seed/operation metadata plus private local request/oracle data; reports receive only metadata. Tier settings come directly from workload-contract sections 3 and 5.

- [ ] Test exact configured rule/dictionary populations, payload byte ranges, supported shapes, vault-state populations, reproducibility and tier rejection.
- [ ] Implement normal and stress fixtures with deterministic size construction and isolated file vaults. Validate transformations using existing oracles.
- [ ] Add Go benchmarks varying rule count, input size and live mappings; include long-history rescans and restorer index rebuilding.
- [ ] Run captured unit/race checks and benchmark smoke; make no performance claim yet.

## Task 3 — Matched load runner and measurements

**Files:** create `scripts/workflow/performance.py`, `scripts/workflow/test_performance.py`; extend reusable environment helpers only where needed.
**Interface:** CLI accepts binary, tier, concurrency, seed, measurement duration, repetitions and private report path. A short smoke mode always reports `performance_qualified: false`.

- [ ] Test pacing, attempt/completion accounting, timeout/cancellation cleanup, pair-key stability and missing metrics behavior with controlled clocks/backends.
- [ ] Run text-only, attachment-only and mixed workloads at 1/4/8/16 clients; reuse independent semantic oracles and record achieved concurrency.
- [ ] Use matched separate proxy/bypass runs with identical backend scripts. Record first byte, end-to-end completion, decoded logical prefix relay timing, stage snapshots, queue wait and outcome counts.
- [ ] Exercise real OCR/PDF separately from the deterministic stand-in; label tool versions and source generation parameters. Missing tools make qualification incomplete, not successful.
- [ ] Run smoke against an isolated binary and test cleanup on deliberate failure before long measurements.

## Task 4 — Profiles, repeated baseline and bottleneck report

**Files:** add local profiling support to benchmark/runner tooling and a safe summary under `docs/verification-results/`; update roadmap and harness guide.

- [ ] Collect CPU, allocation, heap and mutex/block profiles locally with fixed private paths. Include audit durability cost and growing-vault effects without exposing request values.
- [ ] Perform contract warm-up and repeated measurement windows, including its ten-minute throughput windows. Record environment/load and per-run variation; avoid arbitrary shorter qualification runs.
- [ ] Evaluate normal-tier thresholds, report stress capacity separately, and identify the dominant measured cost or demonstrate all required normal-tier gates pass.
- [ ] Run final correctness/race/lint checks and independent review; link evidence and limitations in the Task 04 completion record.

## Starting inventory

Task 03 provides fixed eight-stage timing rings, active/queued/extractor gauges, five request-size buckets and optional bounded correlation. Existing Go benchmarks cover only instrumentation; detection, request walking and restoration benchmarks are still missing. The deterministic harness has 125 correctness operations plus two delay probes, uses compact fixtures and has no normal-tier performance qualification. Main CI now runs Python regression tests on Linux/macOS. Decide full workflow/load smoke integration with the runner, keeping expensive qualification outside routine CI.

## Progress

- [x] Inspected existing benchmark inventory and the workload contract.
- [x] Selected isolated runner plus existing oracles and stage metrics.
- [x] Implement paired first-byte statistics with eight new unit tests (RED: module absent; GREEN: all 26 Python tests pass). Pairing precedes nearest-rank quantiles; negative differences remain, incomplete/duplicate/invalid samples fail with fixed diagnostics.
- [x] Finish outcome/timeout accounting and report schema; committed as `f8208c2`.
- [ ] Complete the real-extraction, fairness, overload and qualification measurements. The text runner and component benchmarks are implemented; diagnostic measurements are in progress.

## Security and CI follow-up — 2026-11-10

The worktree was fast-forwarded to `2e16227` after security PRs 21–23. Capture
IDs and resolved paths are constrained; CI runs the Python regressions; the
MkDocs dependency and embedded manual use patched Material 9.7.7. The existing
statistics work remains uncommitted and intact. After the path changes, all
30 Python tests passed.

Two separate CI fuzz smoke runs reported `context deadline exceeded` after
about 21 seconds (argument JSON on Linux, request sanitization on macOS).
Artifacts contained only existing seeds; no assertion failure or new
counterexample was recorded. Failed-job reruns passed with unchanged test
limits. Track this scheduling/runtime stability issue during baseline work;
do not hide failures or weaken fuzz/correctness gates.

## Text measurement increment — 2026-11-11

Implemented deterministic normal/stress text generation, isolated seeded file
vaults, matched proxy/bypass SSE loads, paired logical-prefix timings, fixed
outcome accounting, resource sampling, and component CPU/heap/lock profiles.
The existing metrics endpoint now includes fixed Go runtime counters without
a debug endpoint or forced GC. Short matrix runs are diagnostic only.

Task 04 remains **in progress**. Real OCR/PDF templates and timing, text/image
fairness, open-loop overload, mapping-churn end-to-end runs, full reference
environment verification, 10-minute throughput windows, and the full three-
repetition sample counts still require completion. The text sample-count
matrix alone takes roughly twelve hours at the specified backend pacing.
The owner has authorized autonomous execution without repeated confirmation.
Continue the remaining implementation and use the reference machine for
sustained runs when ready; preserve every qualification gate.

## Integration and CI follow-up

- The text increment passes 61 Python regressions, 20 Go package race tests,
  vet/staticcheck and the 125-operation deterministic workflow suite.
- The 12-cell development matrix completed 1,044 measured operations with no
  failures. A separate clean-revision four-client sample completed 120
  operations; its added first-byte tails exceeded the numerical targets in
  all three short repetitions. This is diagnostic evidence, not qualification.
- [Aggregate measurement record](../../verification-results/2026-11-11-text-performance-increment.md).
- Review fixes preserve measured operations if post-load collection fails and
  report missing resource observations as unavailable rather than zero.
- A macOS CI readiness failure was isolated to HTTP server binding, which
  invoked reverse DNS. The loopback backend now binds without hostname
  resolution; a regression verifies it works when reverse DNS is unavailable.
- CI runs Python regressions first, cancels sibling matrix jobs on failure,
  gates packaging on successful tests, cancels superseded runs, and avoids
  duplicate feature-push/PR test runs. Live failure evidence confirmed sibling
  cancellation and packaging skip. Docs and default CodeQL are separate.

## Real extraction fixture increment

Added a native reference-Mac CoreText generator for four PNG/JPEG templates
and two two-page PDFs (text layer and scanned). Warm-up and measurement
indices are disjoint and printed in the raster. Private manifests record
expected text, font/image hashes, OS, dimensions and byte counts; validation
checks byte integrity and containment before captured Tesseract/Poppler calls.
The word oracle accepts whitespace differences only. This increment does not
measure the proxy, sanitization, extraction queues or fairness. Task 04 remains
in progress; integrate these fixtures into the isolated runner next.

Portable regression discovery passes 68 tests (one opt-in test skipped); the
explicit real-tool regression passes seven tests, including 16 images and
both PDFs. The Go race suite also passes. Full-corpus validation is recorded
separately after completion.

Review found no critical or important issue in this fixture-only scope. Before
qualification, enforce stored-manifest schema/coverage, record extractor and
Swift versions plus English traineddata identity, and reject font substitution
by checking the resolved PostScript name. These are deferred minor findings
for this generation-and-validation CLI; existing byte hashes do not establish
tool provenance or complete corpus coverage. Production OCR uses different
page-segmentation defaults and still needs its own end-to-end verification.

## Real proxy extraction diagnostic increment

Fixture version 2 now records tool/traineddata provenance, enforces the resolved
font at generation and validates complete recipe coverage. The isolated proxy
runner supports cold-unique and repeated images, separate PDFs, one/four
clients, normal seeded policy/vault and measured per-request extraction stages.
Warm-up records are excluded, metadata-only reports retain failed-operation
denominators, and qualification remains false. The 13-phase development smoke
completed all 52 measured operations with complete stage coverage.

Review fixes reject nonfinite warm-up durations, preserve warm-up evidence when
cleanup also fails, preflight report destinations and atomically persist reports.
The recent-stage TTL and separate CLI font metadata check remain documented
limitations before qualification. Full fairness, overload, stream send timing
and sustained reference repetitions still remain.

[Diagnostic verification record](../../verification-results/2026-11-11-real-extraction-increment.md).

## Stream and fairness diagnostic increment

Implemented semantic stream prefix alignment, actual upstream write/flush timing and signed paired relay statistics. Added mixed closed-loop and bounded open-loop schedulers, continuous stage-history collection, and an isolated native mixed text/image phase. The portable discovery run completed 102 tests successfully; the native mixed-load suite completed 12 successfully. See `docs/verification-results/2026-11-11-stream-fairness-increment.md` for measurement semantics and limits.

Task 04 remains in progress. The native overload phase/CLI, full reference measurements, repetitions, churn/stress and consolidated evidence are still required. Short diagnostic tests do not satisfy performance gates.

## Native overload diagnostic increment

Connected bounded open-loop scheduling to isolated proxy/bypass phases and added an atomic, fail-fast CLI. Native and CLI smoke runs retained all four scheduled slots per arm (two completed, two client-unsent). Collection/cleanup failures preserve scheduler denominators and return a failed command status. Existing report paths are refused before backend work.

Full Task 04 qualification remains outstanding; this increment is diagnostic infrastructure only. See `docs/verification-results/2026-11-11-overload-diagnostic-increment.md`.

## Long-run extraction readiness

Extraction phases now use the shared continuous stage collector, retaining early timings across recent-history expiration while keeping strict completeness validation. The extraction CLI checks declared font metadata before measurement, resolving the previously deferred provenance check. Full Task 04 reference qualification remains outstanding.

## Reference campaign orchestration

Add a fairness CLI and a sequential, resumable campaign around the existing isolated runners. Each repetition receives an independent job identity; repetition offsets preserve alternating arms. Use aggregate minimum text counts, preserve failed attempts, verify source/binary/fixture/report hashes on resume, retain private logs and atomic checkpoints, and stop on failure. Reference conditions are checked before/after jobs; completion never implies qualification.

Implementation verification covers locks, interruption cleanup, identity/tamper rejection, retained failure evidence, no repeated completed work, native workload partitioning, real text comparisons and a complete six-job native smoke campaign. Stress/churn, profiles and the final reference acceptance report remain outstanding.

## Measurement boundary evidence

The acceptance evaluator now checks retained reports without qualifying a partial campaign. Review exposed missing scheduler-origin and observed warm-up fields. The follow-up records those existing boundaries, tested with controlled clocks. Historical reports remain unchanged and incomplete for absent observations; new measurements require their own recorded runner identity. Full Task 04 collection, profiling, stress/churn and consolidated evidence remain in progress.

## Durable audit attribution

Add a production-path durable audit microbenchmark with serial/parallel cases, verified persisted record counts and explicit workers. A fixed four-operation smoke runs inside the existing race step without adding CI jobs or relaxing checks. Optimized CPU/allocation/heap/lock profiling remains separate from race validation and will run only when the reference host is idle. See the audit durability profiling guide under verification-results.
