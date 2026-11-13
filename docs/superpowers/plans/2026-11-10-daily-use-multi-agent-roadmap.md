# Daily-use and multi-agent reliability roadmap

**Created:** 2026-11-10
**Status:** Tasks 01–03 complete; Task 04 in progress. Workload performance targets remain unmeasured.
**First milestone:** Reliable daily use with four concurrent agents on one trusted workstation.
**Owner:** Project maintainer; record an implementer and reviewer when starting each task.

This is the follow-up to the repository assessment and the tool-result image
fix and ADR 0003 in [PR #16](https://github.com/rozdolsky33/cui-proxy/pull/16). It is an
ordered improvement backlog, not a claim that the proposed capabilities or
performance targets already exist. Each task becomes a small implementation
plan once its prerequisites supply the measurements and design decisions.

## Intent

Make the proxy an effective everyday protection layer: supported work finishes
correctly, avoidable refusals are rare and recoverable, and concurrent agents
do not spend unnecessary time waiting for the proxy. Preserve masking,
restoration, explicit policy decisions, and fail-closed behavior throughout.

The proxy is not an agent scheduler or a model-context manager. It should
cooperate with those systems without taking over their responsibilities.
This milestone does not promise universal tool formats, complete detection of
sensitive information, visual understanding from OCR, or lower model token
usage. Measure effects on task success and token use instead of assuming them.

## Starting assessment

These scores are qualitative engineering judgments, not completion percentages.

| Dimension | Assessment on 2026-11-10 | Evidence or gap |
|---|---|---|
| Narrow masking/restoration promise | 8/10 | Structured JSON handling, streamed restoration, policy guards, audit and encrypted vault support |
| Everyday Codex use | 6/10 | Useful text workflows; protocol surprises and rejected history still require intervention |
| Demonstrated multi-agent efficiency | 4/10 | Concurrent correctness tests exist; no Go benchmarks or dedicated sustained-load suite found in the assessment |

The assessed tree at commit `826302b` (PR #16, open when this roadmap was
written) contains 473 test functions. The latest local race run passed
17 packages; vet, staticcheck, build, and request-sanitizer fuzz smoke passed.
Real local OCR through a fake upstream passed for both function/custom tool
output types. These are point-in-time observations, not a substitute for the
workflow and load evidence below. Record commit IDs in all future results.

Potential costs to measure include repeated history scanning, synchronized
audit writes, shared vault locks, restoration snapshot rebuilding, and OCR's
two global subprocess slots. None is designated a proven bottleneck yet.

## Working rules

1. Work on one task at a time. Follow dependencies; finish a reviewable change
   and its evidence before starting the next task.
2. Start with a short task-specific plan naming the actual interfaces and
   tests. Use test-first development for behavior changes. Do not implement
   every item in this roadmap in one PR.
3. Preserve policy blocks, default attachment refusal, mapping correctness,
   and value-free logging. Never obtain a performance gain by skipping scans,
   weakening rules, forwarding originals, or acknowledging an audit write
   before the selected durability contract permits it.
4. Use synthetic workloads and isolated configuration. Follow repository
   `AGENTS.md`: capture fixtures, runner output, configuration and detailed
   reports locally. Session output contains only statuses, rule names, counts
   and other approved metadata. Do not publish captured real traffic.
5. Separate local deterministic verification from optional live backend
   checks. Live checks need a bounded request/token budget and synthetic data.
6. Mark a task `done` only when its acceptance criteria pass and evidence is
   linked. Record an unmet criterion explicitly; do not silently lower targets.
7. Avoid performance tuning without a measured cause. A conditional task may
   be marked `not needed`, with evidence, rather than implemented for its own sake.
8. Update this document after each task. Changes to supported formats,
   persistence, policy or diagnostics also update the relevant operator docs.

## Definition of the first milestone

Task 01 defines the [version 1 workload and acceptance contract](../specs/2026-11-10-daily-use-workload-contract.md).
That contract is authoritative for workload definitions, denominators and gates;
the table below is a summary. These are **initial engineering targets**, not measured guarantees.
If the baseline shows they are unrealistic, document the data and the tradeoff
and have the maintainer accept a revised target before using it as a gate.

| Measure | Proposed acceptance target |
|---|---|
| Deterministic supported workflows | 100% expected completions and correct tool effects; no unexpected refusals |
| Safety regression corpus | Zero known original-value leaks, wrong restorations, or forwarded requests that should be refused |
| Representative live workflows | At least 95% finish without proxy-caused intervention; report sample size and each failure separately from model/backend failures |
| Text latency at four concurrent clients | Proxy-added p95 <=150 ms and p99 <=500 ms in the normal workload tier; report each processing stage |
| Sustained throughput | At least 85% of bypass throughput against the same paced fake backend and workload; never compare different backend delays |
| Fairness | With three attachment-heavy clients, one text client retains the normal text latency target |
| Stability | Four-hour mixed-workload soak at four clients; no crashes, data races or stuck requests; resources return to their expected steady state after load |
| Resource growth | With vault cardinality and workload held fixed after warm-up, retained heap and goroutine counts show no continuing upward trend; record bounded variance and before/after values |
| Restart/reload | Graceful restart preserves persistent mappings; reload uses one coherent policy snapshot per request; crash-loss behavior is explicitly tested and documented |
| Attribution | Reports distinguish proxy, extraction, upstream and client/model failures without logging content |

Proposed normal tier: request bodies up to 1 MiB, 50 regex rules, 1,000
dictionary entries and 10,000 prepopulated mappings. Stress tiers use 2/8 MiB
bodies, larger rule sets and growing vaults. Task 01 verifies whether these
tiers represent actual usage. OCR is measured separately; image requests do
not inherit a text-only latency target. Eight and sixteen clients are capacity
discovery tiers, not initially advertised service guarantees.

## Task tracker

Use `ready`, `blocked`, `in progress`, `done`, or `not needed`. Dependencies
here describe technical ordering, not an instruction to stop and request
permission between already authorized tasks.

| ID | Task | Priority | Depends on | Status |
|---|---|---|---|---|
| 01 | Define workloads and acceptance contract | P0 | — | done |
| 02 | Build deterministic workflow harness | P0 | 01 | done |
| 03 | Add stage timings and safe workload correlation | P0 | 01, 02 | done |
| 04 | Establish concurrent performance baseline | P0 | 02, 03 | in progress |
| 05 | Verify task semantics and long-session compatibility | P0 | 02 | ready |
| 06 | Improve refusal recovery and dependency preflight | P0 | 03, 05 | blocked |
| 07 | Fix the largest measured processing cost | P1 | 04, 05 | blocked |
| 08 | Bound contention and protect text-workload fairness | P1 | 04, 07 | blocked |
| 09 | Make repeated attachment processing efficient | P1 | 04, 05, 08 | blocked |
| 10 | Verify mapping lifetime and recovery | P1 | 02, 04, 05 | blocked |
| 11 | Run sustained four-agent acceptance and tune | P0 release gate | 06–10 | blocked |
| 12 | Establish release and regression gates | P0 release gate | 11 | blocked |

Priority is importance, not permission to bypass dependencies. Recommended
execution order is 01 through 12. Tasks 07 and 09 may close as `not needed`
if measured targets already pass without the proposed optimizations.

## Task 01 — Define workloads and acceptance contract

**Outcome:** A reproducible definition of useful, efficient daily operation.

- [x] Inventory intended workflows using metadata only: coding, tests,
      infrastructure commands, screenshots, delegated work and long sessions.
- [x] Define normal/stress tiers, concurrency, request arrival rates, stream
      chunking, backend latency, rule sizes and vault cardinality.
- [x] Specify the reference hardware, OS, Go/client versions, dependencies,
      power settings, warm-up, sample counts and repetition methodology.
- [x] Define failure taxonomy and how proxy-added latency is separated from
      network/model time. Separate policy-expected blocks from unwanted blocks.
- [x] Finalize the milestone targets above, including a fixed memory budget
      for the reference workstation and explicit OCR latency/queue targets.

**Acceptance:** Every metric has a workload, denominator, measurement method
and threshold. No claim of an unwanted block relies on whether a request
happened to pass the current implementation.

**Artifact:** [Daily-use workload and acceptance contract — version 1](../specs/2026-11-10-daily-use-workload-contract.md).

## Task 02 — Build deterministic workflow harness

**Likely areas:** `scripts/`, `internal/proxy/`, adapter tests and local test utilities.

- [x] Exercise the real proxy against a controlled fake upstream with captured
      request inspection and deterministic SSE output.
- [x] Cover text requests, structured tool arguments/results, metadata,
      mixed text/images, policy refusals, malformed responses and cancellation.
- [x] Simulate multiple independent conversations and an explicitly shared
      parent/child conversation. Preserve tool call IDs and output ordering.
- [x] Include bypass/control mode and fixed seeds; keep environment and backend
      behavior identical between comparisons.
- [x] Emit a machine-readable local report plus a safe summary; include commit,
      versions, sample counts and expected/actual outcomes.

**Acceptance:** An intentional missing sanitization, incorrect restoration,
or unexpected forward makes the harness fail. Successful supported workflows
verify tool effects, not merely HTTP 200. No external service is required.

## Task 03 — Add stage timings and safe workload correlation

**Likely areas:** `internal/proxy/`, `internal/metrics/`, `internal/audit/`, dashboard.

- [x] Measure request parsing, metadata/body inspection, extraction wait/run,
      audit persistence, upstream wait and response restoration separately.
- [x] Add bounded queue/in-flight counts, request-size buckets and relevant
      resource counters. Avoid retaining one metric label per request.
- [x] Design privacy-preserving correlation for sessions/workspaces where
      reliable identifiers are available; use bounded retention and keyed
      identifiers. Never expose raw identifiers or infer authorization from them.
- [x] Keep unknown/missing identity usable and clearly unattributed.

**Acceptance:** A controlled delay in each stage appears in the correct
measurement. Correlation is bounded, leak-free and optional. Instrumentation
overhead is measured and included in the baseline. Dashboard changes receive
visual verification and screenshots.

## Task 04 — Establish concurrent performance baseline

- [x] Add Go benchmarks for detection, request walking and restoration with
      controlled rule counts, content sizes and vault sizes.
- [x] Run the workflow harness at 1/4/8/16 concurrent clients for text-only,
      attachment-only and mixed traffic; report offered load and completions.
- [x] Collect CPU, allocation, heap and lock-contention profiles locally.
- [x] Measure long-history rescanning, audit-write latency, mapping churn and
      restoration-index rebuilding rather than inferring their cost.
- [x] Compare proxy and bypass under identical backend pacing; report p50/p95/
      p99, throughput, memory, queue delay and failure counts.

**Acceptance:** Repeated runs show variation and identify the dominant cost
with evidence, or show that all normal-tier performance targets already pass.
Publish a safe baseline summary; keep detailed profiles local.

Baseline collection and local verification are complete; numerical qualification remains false. See the [measured baseline and limitations](../../verification-results/2026-11-11-task04-baseline-results.md). Independent review and its regression-verified correction are complete. Required CI and merge status are tracked by the publishing PR; primary-main synchronization is verified during integration.

## Task 05 — Verify task semantics and long-session compatibility

**Likely areas:** `internal/adapter/codex/`, `internal/transform/`, workflow corpus.

- [x] Maintain a client/protocol support matrix covering item types, response
      events, transport modes and attachment positions. Unsupported entries
      have explicit diagnostics and tests.
- [x] Test repeated history, compaction representations, delegated context,
      reconnect/resume and supported stream variants with synthetic inputs.
- [x] Verify commands, arguments, files and configuration edits retain their
      intended effects after a round trip. Include quoting and numeric forms.
- [x] Separate representational correctness from model reasoning quality:
      synthetics may change relationships or tokenization. Measure those
      effects with a small paired live corpus when authorized.
- [x] Add regressions for each observed compatibility failure; expand the
      allowlist only when the protocol form and safe transformation are known.

**Acceptance:** Every advertised workflow passes the deterministic contract;
live outcomes report model variance and proxy failures separately. No blanket
acceptance of unknown fields, images or transports is used to make tests pass.

Implementation evidence: [Task 05 results](../../verification-results/2026-11-13-task05-semantics.md)
and [protocol support matrix](../../protocol-support.md). Deterministic scope is
qualified; paired live model quality remains explicitly unmeasured (conditional
on an authorized provider run). No protocol allowlist was expanded.

## Task 06 — Improve refusal recovery and dependency preflight

**Likely areas:** diagnostics, CLI, dashboard, client-facing documentation.

- [x] Distinguish policy decisions, unsupported representations, dependency
      failures, exhausted budgets and transient overload with stable safe codes.
- [x] Provide an actionable remedy and retry classification for each code.
      Preserve existing HTTP/error compatibility unless a versioned change is justified.
- [x] Detect recurring deterministic failures using bounded, value-free state
      where possible. Do not persist raw bodies or reusable content fingerprints.
- [x] Where the client supports it, verify retry guidance actually stops futile
      retries. Otherwise document the client limitation and exact recovery action.
- [x] Add preflight checks for the running binary, effective policy, extractor
      discovery and language data, using the service's actual environment.

**Acceptance:** A repeated unsupported-image scenario produces useful recovery
instructions without upstream calls. A missing OCR executable is diagnosed
before a user encounters an opaque failure. No automatic context deletion,
policy weakening, or unsupported promise to control client retries.

Evidence: [Task 06 recovery results](../../verification-results/2026-11-13-task06-recovery.md).
Client retry guidance remains advisory; preflight checks the running service.

## Task 07 — Fix the largest measured processing cost

**Conditional work:** Select one optimization from Task 04 evidence, not a list
of simultaneous rewrites. Possible areas include detector scanning, restoration
index updates, redundant parsing, or audit persistence.

- [x] Record the bottleneck, expected improvement and correctness invariants.
- [x] Implement one change with differential checks against the reference path.
- [x] Re-run normal/stress benchmarks and the semantic/policy corpus.
- [x] If audit batching is justified, preserve ordering and ensure requests
      wait for their required durable record before forwarding.

Evidence: [paired benchmark and verification report](../../verification-results/2026-11-13-task07-regex-indices.md). Audit batching was not selected; audit behavior is unchanged.

**Acceptance:** Improvement exceeds run-to-run noise, no supported workload
regresses materially, and safety/semantic gates pass. If caching is proposed,
its policy versioning, mapping lifetime, memory bound and invalidation must be
designed explicitly. No complete-request bypass cache is assumed safe.

## Task 08 — Bound contention and protect text-workload fairness

**Likely areas:** proxy admission, attachment extraction scheduling, metrics.

- [ ] Use baseline data to choose bounds for in-flight work, queued work and
      estimated memory. Define behavior at each bound.
- [ ] Ensure cancellation removes queued work and releases resources promptly.
- [ ] Separate expensive extraction work from lightweight text requests where
      measurements show interference; prevent starvation.
- [ ] Add per-workload fairness only where identity can be used reliably; do
      not treat client-supplied labels as a security boundary.

**Acceptance:** Saturation produces a documented bounded response instead of
unbounded memory/queues. Mixed traffic meets the text fairness target; cancelled
clients do not consume persistent queue slots. Overload cannot forward originals.

## Task 09 — Make repeated attachment processing efficient

**Conditional work:** Implement only if repeated extraction is material in Task 04.

- [ ] Measure image reuse across history/retries and extraction queue costs.
- [ ] Compare no cache with a bounded, process-local extraction cache. Document
      sensitive-text retention and scope before implementing a cache.
- [ ] Key any extraction reuse by attachment content plus extractor version/
      options. Keep fingerprints private; do not turn them into public metrics.
- [ ] Reapply current sanitization and blocking rules to reused extracted text.
      Cache hits must still count toward logical request count/byte/text limits.
- [ ] Verify eviction, concurrent identical requests, cancellation and policy
      reload; do not cache transient failures as permanent refusals.

**Acceptance:** Repeated-image latency or resource use improves measurably,
current rules cannot be bypassed by a cache hit, memory stays bounded, and
failed/cancelled extraction does not poison other requests. If retention costs
outweigh the benefit, close with evidence and retain uncached extraction.

## Task 10 — Verify mapping lifetime and recovery

**Likely areas:** `internal/vault/`, application lifecycle, restoration, operations docs.

- [ ] Test long-running vault growth, graceful restart, abrupt termination,
      periodic-save failures and coherent policy reload under concurrent work.
- [ ] Quantify the file-vault crash-loss window and test that recovery does not
      silently produce incorrect restored tool arguments.
- [ ] Decide whether stronger persistence is needed from evidence and workload
      expectations; document its throughput and durability tradeoff.
- [ ] Document shared mappings for cooperating agents and boundaries for
      unrelated workspaces. Do not introduce per-agent vaults that break delegation.
- [ ] Specify lifecycle/retention options without evicting mappings still
      needed by active or resumable histories. Scope changes require an ADR.

**Acceptance:** Graceful restart/resume succeeds with original mappings;
crash behavior matches the documented contract; multi-process locking holds;
no mapping silently resolves to another original. Resource growth and recovery
limits are recorded, not hidden behind a successful health endpoint.

## Task 11 — Run sustained four-agent acceptance and tune

- [ ] Run four hours of four-client mixed work with fixed-cardinality and
      growing-vault phases, realistic history growth and regular tool calls.
- [ ] Inject cancellations, reloads, missing extraction tools, backend delays
      and controlled restart scenarios; distinguish injected failures from bugs.
- [ ] Confirm all milestone targets and collect safe evidence for each.
- [ ] Run 8/16-client stress tests to document the capacity envelope and graceful
      overload behavior; do not claim support beyond the passing tier.
- [ ] Fix the highest-impact failing criterion in a focused PR, then rerun
      affected checks and the acceptance soak when required.

**Acceptance:** All first-milestone gates pass with repeatable evidence. A
partial pass remains partial; successful unit tests alone do not close this task.

## Task 12 — Establish release and regression gates

- [ ] Put a short deterministic workflow suite and stable benchmark comparison
      in CI. Use a controlled performance runner or report-only thresholds
      until runner variance is understood; avoid flaky shared-runner gates.
- [ ] Define when the longer soak runs: scheduled/on demand and before relevant
      releases. Record versions, environment and artifact-retention rules.
- [ ] Verify packaging, upgrade, running-binary identification, effective
      configuration, dependency preflight and rollback with active histories.
- [ ] Publish the compatibility matrix and measured four-client operating
      envelope. Document how to recognize and recover from saturation.
- [ ] Reassess the original ratings using the collected evidence and prioritize
      the next milestone from remaining user-visible failures.

**Acceptance:** A released version can be tied to its workflow, safety,
performance and recovery evidence. Operators can verify that their running
installation matches the tested configuration and restore a working release.

## Per-task completion record

Copy this template below for each completed task. Keep detailed sensitive
artifacts local; link only safe summaries or appropriately protected locations.

```text
Task ID / title:
Implementer / reviewer:
Status:
Commit / PR:
Reference workload and environment:
Behavior changed:
Before / after measurements (sample count and variation):
Safety and semantic verification:
Acceptance criteria passed / unmet:
Evidence location:
Remaining limitations / rollback:
Next ready task:
```

## Task 01 completion record — 2026-11-10

- **Implementer / reviewer:** Codex primary agent / independent specification reviewer.
- **Status:** Done; specification acceptance criteria met, runtime gates unmeasured.
- **Commit / PR:** `d4d6dfb`, [PR #18](https://github.com/rozdolsky33/cui-proxy/pull/18), merged as `373264c`.
- **Deliverable:** [Version 1 workload and acceptance contract](../specs/2026-11-10-daily-use-workload-contract.md).
- **Environment:** Observed 8-core M1 Pro, 32 GiB RAM, macOS 26.7.2,
  Go 1.26.10, Codex CLI 0.167.0, Tesseract 5.5.4 and Poppler 26.09.0.
- **Evidence:** Metadata-only local probes and prior request-size evidence;
  sources and representativeness limitations recorded in contract sections 1–2.
- **Behavior changed:** None. Defined 12 workflow cases, independent semantic
  oracles, normal/stress tiers, reproducible load/streams, failure taxonomy,
  measurement methods, initial numerical gates and Task 02 boundaries.
- **Validation:** Reviewed task coverage and metric definitions; checked document
  links/structure and whitespace. Independent review's live-completion denominator
  and cold-image generation findings were addressed.
- **Before / after performance:** Not measured; this task defines the experiment.
- **Limitations:** Reference workload is designed from limited observations,
  not a representative production trace. Proposed targets may expose failures;
  revisions must retain prior evidence and explain changed criteria.
- **Rollback:** Documentation-only change; no running service or policy changes.
- **Next ready task:** 02, deterministic workflow harness.

## Task 02 completion record — 2026-11-10

- **Implementer / reviewer:** Codex primary agent / independent harness reviewer.
- **Status:** Done for the deterministic correctness scope in contract section 10.
- **Commit / PR:** `1ccd539`, [PR #19](https://github.com/rozdolsky33/cui-proxy/pull/19), merged as `78a1dcb`.
- **Deliverable:** [Harness guide](../../../scripts/workflow/README.md), versioned scenario manifest, runner and oracle/lifecycle unit tests.
- **Behavior changed:** Developer verification only. Real built proxy and separate fake upstream exercise W01–W08/W11/W12 at one/four clients, including parent/child context flow, semantic file effects, strict/disabled modes, mixed-image budget refusal, error and cancellation paths.
- **Validation:** 125 local operations passed; three actual-boundary injected faults rejected; 13 oracle/lifecycle unit tests and all 17 Go race-test packages passed.
- **Review:** Addressed skipped boundary checks on error/cancellation paths, escaped-original detection and cleanup of descendants after their parent exits; added regression tests.
- **Evidence:** Captured local report `/private/tmp/cui-workflow-result.json`; terminal output limited to statuses and counts. Raw temporary fixtures are removed at exit.
- **Before / after performance:** Not qualified. Diagnostic durations only; stage/resource data explicitly unavailable.
- **Limitations:** Compact correctness fixtures use two regex rules and a small growing vault. Real OCR quality, normal-tier benchmarks, compaction/resume, restart qualification and the soak remain later tasks. Bypass controls cover supported text/tool cases only.
- **Rollback:** Remove developer harness changes; production proxy behavior is unchanged.
- **Next ready task:** 03, stage timings and safe workload correlation. Task 05's dependencies are also satisfied; recommended sequence remains Task 03 first.

## Task 03 completion record — 2026-11-10

- **Implementer / reviewer:** Codex controller and runtime implementer / independent whole-branch reviewer.
- **Status:** Done; [implementation and measurement record](2026-11-10-request-stage-observability.md).
- **Commit / PR:** `65815a4`, [PR #20](https://github.com/rozdolsky33/cui-proxy/pull/20), merged as `8fef6e4`.
- **Behavior changed:** Eight exclusive stage summaries, bounded request-size buckets and active/queued extractor gauges are exposed through `/api/metrics`. Optional process-keyed correlation is disabled by default, capped at 256 records and fifteen minutes, and cleared on disable. Existing forwarding and fail-closed policies remain intact.
- **Validation:** All 18 Go packages pass race tests; vet and staticcheck pass. Eighteen Python tests, 125 workflow operations and two controlled-delay probes pass; three boundary fault injections are detected. Controlled delay, reload/privacy/retention, panic and cancellation tests cover the runtime boundaries.
- **Review:** No Critical/Important findings. Added partial timing/gauge assertions for four handler panic/cancellation cases and documented approximately concurrent metrics snapshots.
- **Instrumentation cost:** Local eight-stage aggregate bookkeeping measured 1.136–1.154 µs/op; with correlation 3.910–3.915 µs/op. Full stage snapshots measured 155.7–160.5 µs/op. Three runs; allocation counts and limitations are in the implementation record.
- **Limitations:** These are instrumentation microbenchmarks, not normal-tier qualification. Explicit header identities are client claims and grant no authority. Unknown identity remains unattributed. Stage data is in the API; no dashboard card changes.
- **Rollback:** Revert the change and remove the optional `observability` YAML block before starting an older binary.
- **Next ready task:** 04, concurrent performance baseline.

## Next action

Continue **Task 04** using its [implementation plan](2026-11-10-concurrent-performance-baseline.md). Start with paired first-byte statistics, then representative workloads and the matched-load runner. The full baseline remains unmeasured.
