# Supplemental concurrency coverage implementation plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task by task, then one fresh whole-branch review. Steps use checkbox syntax for tracking.

**Goal:** Close roadmap Task 04's attachment/mixed concurrency coverage without changing existing reference gates.

**Architecture:** Extend the existing Python scheduler, real extraction phase and fairness CLI with explicit client counts and exploratory measured windows. Reuse private fixture/oracle/environment/resource/reporting paths. Preserve default four-client behavior and retain each new source identity separately.

**Tech Stack:** Python standard library, existing Go optimized proxy, native Tesseract/Poppler.

**Spec:** `docs/superpowers/specs/2026-11-13-capacity-coverage-design.md` and the unchanged daily-use workload contract.

## Global constraints

- Existing churn source remains frozen; no build/test/benchmark competes with active measurements.
- Seed 1102026; normal 50 regex declarations, 1000 dictionary entries, 10000 prepopulated pairs; fail-closed extraction and audit.
- Warm each arm for at least 60 seconds and 100 successful operations; three repetitions; independent state and alternating proxy/bypass arms.
- Exactly 100 distinct measured image operations at eight/sixteen clients; preserve all failure outcomes and no duplicated warm-up indices.
- Mixed exploratory windows are 120 seconds at 1 MiB. These do not qualify normal text sample-count or throughput gates.
- Peak limits remain 512 MiB proxy / 1 GiB combined children; missing observations are incomplete.
- Capture source, fixture, runner and test output locally. Print only safe metadata/status/counts.

## Review focus

1. Non-divisible image totals must not accidentally schedule 104/112 requests or reuse fixtures; Task 1 pins exactly 100 unique measured identities.
2. One-client mixed traffic must contain both classes without concurrent requests; Task 2 pins sequence and maximum active count.
3. Increased image workers must not alter default four-client behavior or hide failed requests; Task 2 pins defaults, exception outcomes and class counts.
4. Warm-up must not leak into measured counts/windows or terminate before both minima; Tasks 1/2 retain boundary tests and Task 3 verifies report evidence.
5. Exploratory results from a new runner must not silently qualify or pool with earlier reference revisions; Task 3 checks provenance and summary labeling.

## Task 1 — Exact-count extraction capacity

**Files:** `scripts/workflow/extraction_performance.py`, `scripts/workflow/test_extraction_performance.py`, `scripts/workflow/performance.py`, `scripts/workflow/test_performance.py`.

**Interfaces:** Extend `run_phase(..., clients=...)` and `--clients` to accept 1/4/8/16, preserving existing defaults. Scheduling must retain exactly `count` measured operations for non-divisible client counts. Add optional `total_count=None` to `performance.closed_loop`, mutually exclusive with `per_client` and `duration`. Assign each worker `total_count // clients + (client < total_count % clients)` operations; extraction derives its unique global index from that worker's prefix count plus local serial. Existing per-client and duration modes remain unchanged.

- [x] Add failing tests `test_capacity_clients_preserve_exact_distinct_count` for clients 8 and 16, count100: exactly100 unique measured indices, disjoint from warm-up, requested concurrency attained with a controlled blocking operation; failures remain counted.
- [x] Add `test_closed_loop_total_count_balances_workers` for count100/clients8 and16, asserting exact100 and per-worker counts differ by at most1; reject conflicting modes and invalid totals. Add invalid-client regression and retain existing one/four-client behavior tests.
- [x] Run focused tests with captured output; establish RED from current client restriction / missing exact-count behavior.
- [x] Implement accepted clients and exact-count scheduling without altering existing reference results or the private fixture oracle.
- [x] Run captured focused tests, expecting all PASS; defer native smoke until the reference host is idle.
- [x] Commit as `feat(workflow): measure exploratory extraction concurrency`.

## Task 2 — Explicit mixed concurrency and windows

**Files:** `scripts/workflow/performance_load.py`, `scripts/workflow/fairness_performance.py`, `scripts/workflow/test_performance_load.py`, `scripts/workflow/test_fairness_cli.py`.

**Interfaces:** Add keyword `clients=4` to `mixed_closed_loop` and `run_fairness_phase`; add CLI `--clients {1,4,8,16}` and mutually exclusive `--duration` / explicit `--count`, resolving omitted count to existing1000 when duration is absent.

- [ ] Add failing tests `test_mixed_single_client_cycles_classes`, `test_mixed_capacity_worker_counts`, `test_mixed_default_four_clients_unchanged`, and `test_mixed_capacity_retains_failed_attempts`. For one client assert repeating text/image/image/image and maximum active1; for eight/sixteen assert one text worker and remaining image workers, unique operation identities and measured peak concurrency.
- [ ] Add `test_duration_comparison_retains_unequal_arm_counts`: preserve every arm record and unmatched identities; strict paired comparison stays incomplete instead of truncating samples or dropping failures. Report per-arm timing quantiles separately where a complete paired comparison is unavailable.
- [ ] Add CLI tests rejecting count+duration and nonfinite/nonpositive durations, forwarding clients/duration, preserving default count1000/clients4, and always retaining `performance_qualified: false`.
- [ ] Run captured focused tests and observe RED, then implement the minimal scheduler/phase/CLI extension.
- [ ] Preserve configured concurrency in warm-up as well as measurement; retain both warm-up minima and per-class counts, drain outcomes, achieved concurrency and environment snapshots in reports.
- [ ] Run captured focused tests; expect PASS. Commit as `feat(workflow): extend mixed workload capacity measurements`.

## Task 3 — Verify, collect and integrate

**Files:** `scripts/workflow/README.md`, Task04 baseline report, original baseline plan and roadmap completion record.

- [ ] With churn finished, run captured full Python discovery, native extraction/mixed smoke, Go race tests and lint as required; resolve failures without weakening checks.
- [ ] Record clean new runner/build provenance. Run six template0 distinct-image phases (8/16 clients ×3 repetitions) and eighteen mixed arms (1/8/16 clients ×3 repetitions ×proxy/bypass), serially with the design's warm-up/windows and no competing tests.
- [ ] Independently verify counts, class partition, concurrency, warm-up/window boundaries, semantic outcomes, stage coverage and resources; preserve failures and incomplete observations.
- [ ] Publish separate safe per-repetition tables including medians/tails, completion counts, timings and memory. Link the original reference evidence without pooling source revisions or declaring qualification.
- [ ] Reconcile every Task04 requirement, including the remaining median/completion summaries, churn evidence and earlier implementation checks. Do not check off unproven requirements.
- [ ] Obtain one fresh whole-branch review, fix material findings with regression evidence, then commit, push, create PR, merge only after required checks, and synchronize clean primary main under standing authorization.

## Self-review

The supplement covers only the missing concurrency categories; the contract's full template matrix remains at one/four clients as specified. Single-client mixed traffic is sequential and explicitly not concurrent fairness. Exact count100 at sixteen clients is a deliberate scheduling test. Scope and provenance are explicit; no acceptance threshold or production behavior changes.
