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

- [ ] Add failing tests for pairing independent of input order, negative differences, duplicate/missing sample IDs, non-finite samples, and timeout inclusion.
- [ ] Implement strict sample validation and nearest-rank quantiles; incomplete pair sets fail qualification rather than being silently discarded.
- [ ] Run captured unittest discovery; expect all oracle/statistics tests to pass.
- [ ] Record schema and units in `scripts/workflow/README.md`; commit the tested statistics increment.

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
- [ ] Finish outcome/timeout accounting and report schema, then commit the complete Task 1 increment.
- [ ] The load runner, representative benchmarks and full performance baseline remain unimplemented/unmeasured.

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
