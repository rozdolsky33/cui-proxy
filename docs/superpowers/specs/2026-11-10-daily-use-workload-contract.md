# Daily-use workload and acceptance contract — version 1

**Date:** 2026-11-10
**Task:** [Roadmap Task 01](../plans/2026-11-10-daily-use-multi-agent-roadmap.md#task-01--define-workloads-and-acceptance-contract)
**Status:** Specification complete; performance and workflow gates are not yet measured.
**Purpose:** Make Task 02 executable and define the four-agent release milestone.

The numerical thresholds below are engineering targets selected for this
milestone, not existing product guarantees. This contract fixes the initial
targets for measurement. A later revision must retain previous results and
explain any changed threshold; passing a revised target must not be presented
as passing version 1.

## 1. Evidence and scope

The actual work observed in the preceding investigation includes repository
inspection, edits, test execution, infrastructure-related commands, screenshots
returned by tools, and repeated conversation history. A recorded refused
request was 741,170 bytes (about 724 KiB). Two tool-result image parts caused
six successive refusals before the image-extraction compatibility fix.

A locally captured configuration snapshot contained 22 rule declarations:
20 regex declarations, one dictionary declaration and one builtin declaration.
The builtin declaration can expand into multiple detectors. Counts below
distinguish declarations from compiled detectors. The snapshot used memory
vault mode and attachment extraction; it does not establish crash durability.
Dictionary size, long-term request-size distribution, and the number of
simultaneously active agents were not established. The fresh dashboard event
probe supplied no response-request samples usable for this contract.

Consequently, the workload is an explicit representative design with headroom,
not a statistically representative production trace. No real prompts, rules,
dictionary entries, commands or image contents are copied into fixtures.

Version 1 targets one trusted workstation and four logical clients. A logical
client is a sequential conversation; four clients may have four requests in
flight. An eight/sixteen-client result is exploratory and cannot silently
expand the advertised support tier.

## 2. Reference environment

| Component | Version 1 reference |
|---|---|
| Hardware | MacBookPro18,3; Apple M1 Pro; 8 logical CPUs; 32 GiB RAM |
| OS / architecture | macOS 26.7.2 / arm64 |
| Go | go1.26.10 darwin/arm64 |
| Client for optional compatibility probes | codex-cli 0.167.0 |
| Real extraction | Tesseract 5.5.4 with English data; Poppler 26.09.0 |
| Backend for deterministic/performance work | Local fake upstream, no external network, fixed timing and response script |
| Build | Record full source commit, clean/dirty status, build flags and executable hash; normal optimized build without race instrumentation for timing |
| Power/load | Plugged into AC, Low Power Mode off, no other benchmark/build/OCR jobs; record power mode and competing load before and after each repetition |

Hardware and tool versions above were read locally on 2026-11-10. The power
configuration probe found no enabled low-power setting; AC and quiet-machine
conditions are requirements to verify at benchmark time, not observations
already established. Other machines may run correctness checks, but their
performance results are labeled separately and do not pass this reference gate.

Use one isolated proxy process per run with temporary config/vault/audit paths
and a separate fake upstream process. Never benchmark through the user's live
proxy or modify its policy. Keep the harness/client process outside measured
proxy resource totals. Measure OCR child resources as specified below.

## 3. Policy, state and data generation

Use deterministic generation seed `1102026`, versioned with the fixture
generator. Commit synthetic generator inputs and semantic expectations; keep
executed fixtures and detailed reports captured locally per `AGENTS.md`.

The normal policy has the builtin rule set enabled, **50 regex declarations**
and **one dictionary declaration with 1,000 entries**. Twenty regex declarations
represent credential/identifier assignments, twenty exact identifiers and ten
boundary/quoting variants. Each has a bounded, valid Go regexp and expected
positive/negative cases. All dictionary entries are unique under the detector's
case folding. Record the final compiled detector count in the manifest.

Create a fixed corpus of 10,000 distinct synthetic original/class pairs and
prepopulate their mappings before timing. Record resulting vault entry count;
do not assume it equals serialized alias count. Timed steady-state traffic
reuses those mappings. A separate churn scenario introduces 100 new pairs per
minute from a deterministic sequence; it is not judged by fixed-state memory
growth criteria. Restoration expectations are independent of synthetic spelling.

Primary correctness/performance runs use `sanitize`, `on_error: block`,
`attachments: extract_text`, encrypted file vault with 30-second save interval,
and enabled audit with `on_error: block`. Keep production durability semantics.
Memory-vault runs are a separately labeled diagnostic comparison. Policy-negative
cases additionally use `strict`, explicit blocking rules, attachment mode
`block`, and permitted passthrough configuration to prove refusal invariants.

Each normal text request contains 40 occurrences of prepopulated detected
values, including repeats, plus 40 independently labeled near-miss occurrences.
The generator rejects accidental extra matches. Padding uses ordinary harmless
prose across messages and tool results, not one giant repeating character.
Vary string leaves and object counts without changing the expected match set.
Keep original occurrences out of opaque protocol fields except in explicit
negative tests. Fix authentication to a fabricated test-only value.

## 4. Workload catalog and independent oracles

All supported cases must reach the expected tool effect or terminal response.
HTTP 200 alone is never success. Use controlled local interpreters of tool
arguments or temporary files, not arbitrary shell execution or real infrastructure.

| ID | Workflow | Required oracle |
|---|---|---|
| W01 | Inspect synthetic repository and answer with identifiers | Upstream contains expected substitutions; client receives correct originals in supported fields |
| W02 | Edit a file through structured function arguments | Restored JSON parses and applies the exact expected edit in a temporary directory |
| W03 | Return test output, then issue a follow-up tool call | Quoting, numeric forms and call association retain the expected semantics |
| W04 | Plan an infrastructure command from fabricated host/resource data | A parser compares restored command and arguments to expected tokens; nothing executes remotely |
| W05 | Tool output containing text plus two inline images | Image bytes/metadata disappear upstream; extracted text is sanitized; ordering/call ID and restored response match expectations |
| W06 | One user attachment followed by a tool-result image | Shared budgets, surrounding text and policy decisions are correct |
| W07 | Parent delegates context to three children and receives results | Shared known substitutions restore consistently; no child receives another case's original through a wrong mapping |
| W08 | Four unrelated conversations interleave responses | Correct request/response association and coherent per-request policy snapshots |
| W09 | Repeated history, resume, and documented compaction representation | Supported fields restore; opaque content stays opaque; no claim to inspect encrypted content |
| W10 | Graceful restart with file vault and continuation | Previously issued substitutions still resolve to the original pair |
| W11 | Blocking matches, unsupported positions and malformed input | Expected diagnostic class; no upstream call; no leaked payload in logs or summaries |
| W12 | Upstream error, malformed successful response, client cancellation | Correct failure attribution; no false success; resources released |

The fake upstream constructs its response from the actual sanitized request.
It must not receive the proxy's vault or an original-to-synthetic mapping from
the harness. The client-side oracle owns originals and expected effects.
Upstream assertions verify absence of all expected protected originals, expected
replacement counts and preserved structure. Add intentional mutation checks:
remove a sanitizer call, swap two restored values, or forward a negative case;
each must fail the relevant harness assertion. These checks demonstrate coverage
of the declared synthetic corpus, not complete detection of arbitrary data.

Unsupported protocol forms receive negative tests and remain outside the
supported-completion denominator. Classify them from a versioned compatibility
manifest before running the implementation, not from the observed outcome.

## 5. Payload, stream and load tiers

| Tier | Bodies on the wire, UTF-8 bytes | Rules / dictionary / prepopulated mappings | Purpose |
|---|---|---|---|
| Small | 64 KiB | Normal policy / 1,000 / 10,000 | Lightweight operations |
| Normal | 256 KiB, 768 KiB, 1 MiB, equally weighted | Normal policy / 1,000 / 10,000 | Four-client release gate |
| Large | 2 MiB, 8 MiB | Normal policy / 1,000 / 10,000 | History/size stress |
| Rule stress | 1 MiB | 200 regex declarations / 10,000 / 10,000 | Detector scaling |
| Vault stress | 1 MiB | Normal policy / 1,000 / 100,000 | Restoration and mapping scaling |

Target body sizes allow +/-1% serialization variation; record actual bytes.
Do not change several stress dimensions together until their individual costs
are measured. Add a combined worst-case run later as exploratory evidence.

Normal SSE: wait 1,000 ms after reading the full request, send 32 output-text
delta events at 20 ms intervals, then send a terminal event. Use about 8 KiB
total decoded text, including substitutions derived from the request. Split
at least two substitutions across events. Structured argument scenarios use a
separate fixed script whose deltas reconstruct valid JSON. Correctness-only
variants split UTF-8 bytes, SSE framing and escapes across writes, inject
malformed frames, or cancel at prescribed event numbers. Flush every write.
Record actual upstream send times to distinguish scheduler jitter from proxy work.

Run text at 1, 4, 8 and 16 clients. Closed-loop throughput mode starts one
request per client, immediately replacing each completed request; it has zero
think time and a fixed measurement window. Fairness mode keeps three clients
busy with images while one text client runs this same closed loop. The proxy
configuration is identical between modes except for explicitly labeled tests.

Open-loop overload discovery schedules 1, 2, 4 and 8 requests/second for
120 seconds per rate with a maximum of 16 client-side outstanding requests.
Count scheduled, sent, completed, rejected, and client-unsent operations
separately. Do not hide client-unsent work as proxy success or exclude failures
from latency summaries. Record achieved concurrency rather than assuming that
an arrival rate guarantees a particular number of agents.

## 6. Attachments and expensive-work fairness

Use deterministic OCR output for fast correctness tests, invoking the real
image validation/extractor subprocess boundary. Real-OCR qualification uses
four generated English text image templates with known printed strings: PNG and JPEG,
1024x768 and 1920x1080, each at most 2 MiB decoded. Fix fonts, rendering inputs
and image hashes in the local fixture manifest. Avoid handwriting/blank images
in the supported set; include them as expected empty/unsupported cases where
appropriate. Plaintext replacement expectations must be specified independently
of whatever OCR happened to return on a run.

Use two generated PDFs: one two-page text-layer document and one two-page
scanned document, each at most 2 MiB decoded. Use paired single-attachment and
mixed user/tool scenarios. Tool-result PDFs remain an unsupported negative case
unless a separate compatibility change expands the contract.

For normal OCR timing, each request carries one image. For each template,
generate 100 distinct rasters by printing a fixed-width six-digit sample index
alongside the same reference text, using the versioned seed/font/rendering
recipe. The independent expected text includes the printed index; record each
image's hash and expected text in the local fixture manifest. Changing only
compression metadata does not count as a distinct raster. Use disjoint indices
for warm-up and measurement. Start a fresh proxy/cache state per repetition,
then use each measured image once for the cold-unique case. The repeated-history
case sends the same template/index image 100 times after warming it once.

Measure all 100 scheduled operations per format/dimension at one and four
clients per repetition; failures stay in counts and fail the zero-failure gate,
rather than being replaced with successful samples. PDF timing is reported
separately from image timing. Existing request budgets still apply to every
logical attachment, even if a later implementation reuses extraction results.

## 7. Measurement protocol and thresholds

Use a monotonic clock. Warm up for 60 seconds and at least 100 completed
requests, then take three measured repetitions. Reset to identical generated
state for each repetition. Alternate bypass/proxy order between repetitions.
For normal text timing, collect at least 1,000 completed requests per size and
concurrency per repetition. A cancelled/failed request remains in outcome counts
and gets its observed duration; do not silently remove it from the workload.

Quantiles use nearest-rank over individual samples. Report p50/p95/p99 and
counts for each size/concurrency/repetition, not just pooled averages. All three
repetitions must meet release thresholds. Report min/max across repetitions;
do not infer statistical significance from one run.

### Latency and throughput

- **Primary added first-byte latency:** for each seeded request case, subtract
  bypass time-to-first-response-byte from proxy time-to-first-response-byte,
  in matched separate runs with identical workload/backend pacing. Retain
  negative differences as jitter; never subtract one percentile from another.
  This measures added client-visible delay, including local scheduling.
- **Stage timings (Task 03):** report proxy ingress/read/parse, metadata/body
  inspection, extraction queue/run, audit wait/write, upstream wait, and
  restoration work. Define spans to avoid double-counting nested extraction
  and sanitization. Existing global overhead metrics are not assumed to cover
  the full request lifecycle. Stage instrumentation explains the primary metric.
- **Text gate:** normal-tier four-client added first-byte p95 <=150 ms and
  p99 <=500 ms; apply the same limits to the text client in fairness mode.
  Separately report end-to-end completion and per-event relay delays.
- **Stream gate:** under normal text load, p99 extra per-event client delay
  relative to the matched bypass event <=100 ms. Preserve event association
  even if a restoration buffer splits or combines wire writes; compare the
  availability of corresponding decoded logical text prefixes.
- **Throughput gate:** normal four-client proxy successful completions/second
  >=85% of bypass over equal 10-minute measured windows after warm-up. Both
  use the same fixed response scripts. Failures are not successful completions.
- **OCR gate:** real image extraction run-time p95 <=5 seconds and queue wait
  p95 <=2 seconds at four clients, with zero unexpected failures. The complete
  extraction phase must remain within its existing 30-second request budget.
  Text fairness must pass simultaneously. PDF extraction p95 <=10 seconds
  for the defined two-page cases, measured as a distinct workload.
- **Queue/cancellation gate:** queued work cancels within 1 second; active
  extraction cancellation releases its slot within 2 seconds. At overload,
  no accepted request waits indefinitely: maximum 45 seconds per harness
  request, with timeout counted as a failure rather than dropped evidence.

No performance gate applies to race-instrumented binaries. Run race checks
separately as correctness evidence. Eight/sixteen-client and stress-tier
latency/throughput are report-only capacity discovery; safety remains mandatory.

### Resources and stability

Sample process RSS, Go live heap, goroutines and child RSS at one-second
intervals, recording measurement availability. Reference-machine normal-load
limits are **512 MiB peak proxy RSS** and **1 GiB peak combined OCR/PDF child
RSS**. Measure the whole live child process tree, not only the parent process.
These are acceptance budgets, not claims of OS-enforced sandbox limits.

For the four-hour soak, use four clients with a deterministic request schedule:
70% text W01–W04/W08, 20% image W05/W06 and 10% scripted lifecycle/negative
operations W07/W09–W12. Record each category's denominator; injected negative
operations do not lower the supported success rate. Run a fixed-state phase
for two hours and a mapping-churn phase for two hours. Mark deliberate restart
intervals and reconnect clients under the recovery script.

For the fixed-state phase, compare the first and last 15-minute windows after
warm-up: median live heap and RSS must not grow by more than 10%; post-drain
goroutines must return to the pre-load idle count plus at most five within
60 seconds. Evaluate post-drain heap after a documented equivalent GC procedure
in both samples; do not compare pre-GC and post-GC snapshots. Any sustained
growth within that bound is still investigated before declaring the milestone.
For the churn phase, report growth per new mapping and enforce peak budgets;
do not label intentional retained mappings a leak or silently evict them.

Soak gates: zero crashes/data races, no stuck requests, no known leaks of corpus
originals, no wrong restorations, and all normal-tier latency/fairness budgets
pass in each 15-minute supported-work window. Fault-injection windows are
reported separately with their own expected outcomes.

### Recovery and correctness

Every deterministic supported case must pass. Every policy/shape negative must
produce the specified diagnostic class with zero upstream calls. At least ten
graceful restarts and ten policy reloads under load must preserve mappings and
per-request policy coherence. Abrupt-stop tests establish the actual loss window
against the 30-second save contract plus measured save duration; no successful
health check may be used as proof that historical values restore. Any silent
wrong-original restoration is a release failure regardless of throughput.

## 8. Failure taxonomy and denominators

Assign each operation exactly one terminal outcome using the manifest and
recorded boundary observations. Keep a separate root-cause field if known.

| Outcome | Meaning | Scoring |
|---|---|---|
| supported_success | Correct terminal response and expected semantic effect | Supported success numerator |
| expected_policy_block | Manifest says configured policy must refuse | Passes negative gate only with zero forwarding |
| expected_representation_block | Manifest marks unsupported/malformed representation | Passes negative gate only with prescribed behavior |
| unexpected_proxy_refusal | Supported input refused by proxy | Supported failure; counts as intervention if recovery needed |
| transformation_error | Leak, changed semantics, wrong/unrestored required value | Safety/semantic failure; never excused by HTTP 200 |
| dependency_failure | Extractor missing/failed outside an injected negative | Environment run invalid for performance qualification; retains failure evidence |
| proxy_overload | Proxy's documented admission/queue bound reached | Expected only in overload scenarios; normal workload failure |
| upstream_failure | Fake/live backend fails before a valid response | Separate backend failure; injected cases must match expected behavior |
| client_cancelled | Scheduled cancellation or external client abandonment | Separate scenario; enforce resource-release criteria |
| client_or_model_failure | Client protocol error or model task failure without established proxy cause | Separately reported; never automatically blamed on proxy |
| harness_invalid | Bad fixture, lost measurements, timing/setup error | Invalidates affected run; retain evidence and rerun |
| unresolved | Failure attribution not established | Unmet acceptance criterion until investigated |

Supported deterministic rate = supported_success / all manifest-supported
operations outside intentionally injected faults. Report negative pass rate
separately. Throughput includes successful completions only; offered, sent,
cancelled, failed and unsent counts must reconcile. A dependency failure cannot
be relabeled an unsupported case to improve the score.

## 9. Optional live backend qualification

Live work is a separate, explicitly budgeted run; Task 01 sends no live test
traffic. Propose 20 synthetic end-to-end workflows, paired bypass/proxy with
the same client/model/version/settings, at most five model requests per arm
(200 total requests). Obtain an explicit token/spend cap before execution.
Stop at either cap; an incomplete sample remains incomplete, not a passing run.

Target at least 19/20 proxied workflows that both complete their expected
semantic task effects and require no proxy-caused intervention (95%). All
twenty predeclared workflows remain in the denominator. Model/backend failures
do not count as completed workflows; attribute them separately without
silently excluding them or replacing them with retries. A run that misses
19/20 cannot qualify, even if no proxy fault is established; repeat a complete
paired run after resolving environmental causes and retain both results.
Report all twenty outcomes and a binomial confidence interval: a 20-case
sample is a practical smoke gate, not proof of a population-wide 95% success
rate. Record input/output tokens, extra turns and task effects; do not assume
masking saves tokens. Raw prompts and detailed reports stay captured locally.

## 10. Task 02 implementation handoff

Build the deterministic correctness harness first; it need not run the full
performance matrix yet. Suggested boundaries:

1. A versioned scenario manifest declares IDs, seeds, payload tiers, policy,
   expected outcomes, tool effects and stream scripts independently of the proxy.
2. A generator produces temporary fixtures and an oracle containing originals;
   only the client-side harness can access that oracle.
3. A fake upstream records controlled boundary observations and emits responses
   based on received values. It supports delay, chunking and explicit fault scripts.
4. A runner starts the actual built binary with isolated paths, drives one or
   four clients, enforces timeouts and shuts down subprocesses cleanly.
5. A report writer stores versioned local structured evidence and prints a
   minimal safe summary. Failures and skips produce distinct non-success states.

Required report fields: contract/generator version, source commit and dirty
state, executable hash, environment, seed, scenario, size, concurrency, policy
revision identifier, vault counts, counts by terminal outcome, latency samples/
quantiles, resource samples, assertion results and evidence locations. Do not
include original values, request bodies, image bytes, OCR text, credentials,
raw workspace/session identities or externally reusable content fingerprints.

Task 02's completion gate is a deterministic W01–W08/W11/W12 suite, both
single-client and four-client, plus mutation checks. W09/W10 and the complete
protocol matrix mature in Task 05/10; the harness must expose the scripts needed
for them. Missing Task 03 timings are marked unavailable, never zero. Task 04
adds the measured performance suite; Task 11 executes the complete soak gate.

## 11. Specification verification

Task 01 closes when the workload inventory, environment, tiers, load/stream
definitions, independent oracles, outcome taxonomy and all metric gates are
specified and reviewed. No performance result is required to close a definition
task, and none is claimed here. Task 02 is the next implementation unit.

Independent review identified and resolved two specification ambiguities before
Task 01 completion: cold-image generation now supplies distinct raster inputs,
and the live numerator requires completed task effects with a fixed denominator.

Revisions must record date, rationale, affected scenario/metric, old/new target
and whether previous results remain comparable. Version 1 is the initial
contract derived from the limited local evidence and explicit engineering
choices described above.

### 2026-11-11 security maintenance revision

Go 1.26.11 replaces Go 1.26.10 for new reference builds because
[GO-2026-6859](https://pkg.go.dev/vuln/GO-2026-6859) reports a reachable
HTTP/1 server crash in the older standard library. The reference table above
retains the originally observed version; record the exact patched version in
each new run. No workload, denominator or numerical gate changes. Historical
Go 1.26.10 diagnostics remain labeled with their original toolchain; do not
pool them with patched-toolchain repetitions or treat them as qualification
of the patched build. Rerun required measurements with the patched binary.
