# Task 04 — Concurrent performance baseline

**Performance qualification: false.** The collected baseline identifies regular-expression matching as the dominant measured CPU cost: it accounts for 93.51% of cumulative CPU samples in the detection component profile and 82.30% in the request-walking profile. These component shares are diagnostic, not percentages of end-to-end request time.

The four-client normal-tier first-byte p95 target fails at all three tested text sizes. Ten-minute throughput ratios fail at 768 KiB and 1 MiB, and the 1 MiB text and growing-mapping runs exceed the proxy RSS budget. Correctness, successful collection, and passing relay/extraction checks do not erase those failures. The following sections preserve each repetition, source revision, failure, missing observation and collection interruption so subsequent work can address measured costs without weakening fail-closed behavior.

## Current collection status

Measurement collection is complete. Performance qualification remains **false**: several unchanged latency, throughput and memory gates fail, and the reference evidence spans separately identified source revisions. Local verification and independent review are complete. Required CI and merge status are recorded by the pull request publishing this report.

| Workload | Retained collection | Result and limits |
| :--- | :--- | :--- |
| Normal text | 36 cells, 72 proxy/bypass arms | Complete sample-count matrix; latency and memory failures retained |
| Throughput | 9 pairs, 18 arms | Ten-minute windows; 3 ratio passes and 6 failures |
| Real extraction | 60 phases | All 30 four-client extraction evaluations pass; single-client capacity is reported separately |
| Mixed fairness | 9 pairs, 18 arms | All nine added first-byte p95 evaluations fail; correctness and resource observations pass |
| Open-loop overload | 72 phases | Accounting and observed resources pass; 1,878 client-unsent slots retained |
| Stress | 5 cases, 15 pairs, 30 arms | Exploratory sample counts; all three 8 MiB proxy arms exceed the RSS budget |
| Mapping churn | 6 arms | All three proxy arms exceed the RSS budget; interruption/continuation and unsent work retained |
| Supplemental concurrency | 15 jobs, 24 phases | 8,488 successful records; all observed resource budgets pass; exploratory and unqualified |
| Component profiling | 12 invocations, 24 raw profiles | CPU, heap/allocation and contention evidence retained locally |

The supplemental campaign has 30 retained environment snapshots matching all eight fixed fields, 15 verified report hashes, complete proxy extraction-stage coverage, recorded zero exit status for each measurement command, and verified owned-process cleanup. Its supervisor required one documented continuation after a progress-output pipe closed. That interruption does not erase completed results or turn the campaign into an uninterrupted run.

## Completed normal four-client observations

Values below are the range across completed repetitions, in milliseconds. Each repetition pairs proxy and bypass samples by operation identity before calculating nearest-rank percentiles. These are ranges of per-run percentiles, not pooled or subtracted aggregate percentiles. Warm-up operations are excluded.

| Request size | Completed repetitions | Supported successes, proxy / bypass | Added first-byte p95 | Added first-byte p99 | Paired relay p99 |
|---|---:|---:|---:|---:|---:|
| 256 KiB | 3/3 | 3,000 / 3,000 | 303.1–319.0 | 337.5–348.3 | 59.3–63.1 |
| 768 KiB | 3/3 | 3,000 / 3,000 | 680.1–685.0 | 695.8–706.5 | 62.7–63.5 |
| 1024 KiB | 3/3 | 3,000 / 3,000 | 898.8–902.0 | 918.1–926.6 | 60.3–62.2 |

The normal limits are added first-byte p95 ≤150 ms and p99 ≤500 ms, and relay p99 ≤100 ms. Completed four-client repetitions exceed the first-byte p95 limit. The completed 768 KiB and 1 MiB repetitions also exceed the first-byte p99 limit. Their completed operations preserve correctness and their relay timings meet the relay limit. Further measurements cannot erase these observed failures.

## Observed peak memory

The resource evaluator has complete peak samples for the nine completed four-client repetitions. The proxy limit is 512 MiB; the extraction-child limit is 1 GiB. Peaks are resource observations, not request-latency percentiles. For these text runs, the resource sampler starts after warm-up; the reported RSS peaks come from those post-warm-up samples.

| Request size | Proxy peak RSS range (MiB) | Resource gate |
|---|---:|---|
| 256 KiB | 151.7–155.2 | Pass |
| 768 KiB | 391.6–395.1 | Pass |
| 1 MiB | 519.8–524.6 | Fail |

All three completed 1 MiB repetitions exceed the proxy memory limit. No extraction-child RSS was recorded for these text-only workloads. CPU and allocation profiles are still required to attribute the observed latency and memory costs.

## Measured cost diagnostics

Final stage-timing snapshots include pre-measurement traffic and warm-up, and differ from the measured-only acceptance pairs above. Stage metrics retain the most recent 4,096 observations. The nine completed four-client runs contain 1,113–1,197 body/audit observations each, so their stage buffers have not wrapped; longer future runs may report a recent window instead. Stage percentiles describe elapsed stage time; they cannot be added together or treated as CPU attribution. The body timer spans adapter sanitization and request-event bookkeeping, subtracting nested extraction spans; it is broader than detection alone (see `handler.sanitize` and `Trace.Body`). Runtime heap samples, like the RSS samples, are collected after warm-up. They include dashboard work. The live-heap metric describes the most recent garbage collection, not a forced collection at sampling time.

| Request size | Body inspection p95 range (ms) | Audit write p95 range (ms) | Peak sampled live heap range (MiB) | Peak sampled heap objects range (MiB) |
|---|---:|---:|---:|---:|
| 256 KiB | 235.2–254.6 | 11.7–13.7 | 67.6–68.8 | 125.0–126.8 |
| 768 KiB | 601.4–607.9 | 12.4–13.6 | 188.0–192.9 | 350.4–362.9 |
| 1 MiB | 804.9–821.1 | 12.6–14.9 | 251.2–252.9 | 472.6–488.2 |

Body-inspection elapsed time and heap size increase with request size in the completed four-client observations. This makes body processing a profiling target, but does not establish a particular CPU hotspot, a memory leak, or the share of end-to-end latency caused by any stage. Sampled peaks may miss transient maxima. Optimized profiles and the remaining concurrency levels are still required.

## Other concurrency observations

These results are capacity comparisons, separate from the four-client acceptance gates. Each completed repetition still requires 1,000 operations per arm. Client counts are not pooled.

| Clients | Request size | Completed repetitions | Supported successes, proxy / bypass | Added first-byte p95 (ms) | Added first-byte p99 (ms) | Paired relay p99 (ms) | Proxy peak RSS (MiB) |
|---:|---|---:|---:|---:|---:|---:|---:|
| 1 | 256 KiB | 3/3 | 3,000 / 3,000 | 294.8–295.8 | 299.6–302.5 | 60.3–60.4 | 143.2–145.7 |
| 1 | 768 KiB | 3/3 | 3,000 / 3,000 | 658.3–662.8 | 668.1–676.0 | 60.4–62.9 | 374.0–379.0 |
| 1 | 1 MiB | 3/3 | 3,000 / 3,000 | 860.1–863.2 | 872.5–877.2 | 60.2–60.8 | 460.6–482.1 |
| 8 | 256 KiB | 3/3 | 3,000 / 3,000 | 382.0–392.4 | 412.7–424.6 | 65.1–65.2 | 161.3–162.2 |
| 8 | 768 KiB | 3/3 | 3,000 / 3,000 | 954.8–971.7 | 994.7–1,029.9 | 53.2–65.0 | 409.0–418.2 |
| 8 | 1 MiB | 3/3 | 3,000 / 3,000 | 1,219.7–1,249.8 | 1,273.0–1,315.2 | 63.7–65.5 | 530.7–531.6 |
| 16 | 256 KiB | 3/3 | 3,024 / 3,024 | 431.2–455.5 | 614.3–638.8 | 63.0–66.9 | 169.6–173.5 |
| 16 | 768 KiB | 3/3 | 3,024 / 3,024 | 1,347.3–1,389.1 | 1,653.5–1,854.0 | 60.4–72.7 | 432.7–440.9 |
| 16 | 1 MiB | 3/3 | 3,024 / 3,024 | 2,002.4–2,024.3 | 2,300.9–2,416.6 | 84.2–87.7 | 561.1–566.0 |

The single-client 1 MiB row contains all three required repetitions. Both arms completed all 1,000 operations successfully in each repetition. Complete post-warm-up resource samples stayed within the proxy and child-process budgets in all three repetitions. These are capacity observations; the four-client acceptance gates are evaluated separately.

The eight-client 256 KiB row contains all three required repetitions. Both arms completed all 1,000 operations successfully in each repetition. Complete post-warm-up resource samples stayed within the proxy and child-process budgets. These capacity observations do not establish full-matrix qualification.

The eight-client 768 KiB row contains all three required repetitions. Both arms completed all 1,000 operations successfully in each repetition. Complete post-warm-up resource samples stayed within the proxy and child-process budgets. These results complete this capacity case, not the full text matrix.

The eight-client 1 MiB row contains all three required repetitions. Both arms completed all 1,000 operations successfully in each repetition. Complete post-warm-up resource samples recorded proxy RSS of 556,466,176, 556,662,784 and 557,449,216 bytes (530.7–531.6 MiB), exceeding the 512 MiB budget in every repetition; all three resource results are **fail**. Child-process RSS was zero. The observed budget failures remain part of the evidence.

The sixteen-client 256 KiB row contains all three required repetitions. Both arms completed all 1,008 operations successfully in each repetition. The runner rounds the 1,000-operation minimum up to 63 operations per client for sixteen clients; all retained operations are included in the statistics. Complete post-warm-up resource samples stayed within the proxy and child-process budgets.

The sixteen-client 768 KiB row contains all three required repetitions. Both arms completed all 3,024 operations successfully across these repetitions. Complete post-warm-up resource samples stayed within the proxy and child-process budgets. These capacity observations are separate from the four-client acceptance gates.

The sixteen-client 1 MiB row contains all three required repetitions. Both arms completed all 3,024 operations successfully across these repetitions. Complete post-warm-up resource evidence records proxy RSS of 593,477,632, 588,333,056, and 590,692,352 bytes, all exceeding the 512 MiB budget; child-process RSS was zero. These are capacity results, separate from the four-client latency acceptance gates.

All three repetitions are complete for the one-client 256 KiB workload. all three repetitions are complete for the one-client 768 KiB workload. The table shows ranges of per-repetition statistics where multiple repetitions are available; single-repetition values remain preliminary. These results do not qualify the full workload matrix.

## Provenance and interpretation

The refreshed nontext acceptance evaluator processed all 87 collected jobs. Its full-campaign coverage state remains `invalid`: it requires 123 planned jobs, while this separately recorded campaign plans 87. The earlier 36 text cases are retained under their own source revision; these two campaigns are not pooled into a qualification claim. Collection status and numerical acceptance are separate.

Within the nontext observations, throughput passed in 3 cases and failed in 6; all 9 mixed-workload first-byte checks failed, while their relay checks passed. All 60 extraction correctness/resource observations passed. The 30 four-client extraction observations passed their sampling, run, queue and combined-budget checks; the 30 single-client capacity observations remain incomplete for those qualification checks by design. All 72 overload phase accounting checks passed, with client-unsent operations still included in their reported accounting.

The text campaign runs the frozen, reviewed `f0c3f37` source with optimized Go 1.26.11 binaries on the reference MacBook Pro (M1 Pro, eight logical CPUs, 32 GiB). Environment conditions are checked before and after jobs. The workload uses the contract's normal policy and 10,000 prepopulated pairs, with matched backend pacing and at least 60 seconds/100 operations of warm-up.

The thirty-six completed runs have 144 before/after environment snapshots, all reporting `MacBookPro18,3`, `arm64`, Darwin `25.6.0`, eight logical CPUs, 32 GiB RAM and Go `go1.26.11`, with AC power present and low-power mode disabled. The campaign controller uses Python `3.14.5`. These checks do not prove continuous host conditions between snapshots.

| Text campaign identity | Value |
|---|---|
| Clean source revision | `f0c3f37d858c84c6b86ab0dc361c531d18c45a5c` |
| Proxy binary SHA-256 | `bbdb327d4ee3ab5335aa92059ab1f08441f5483b5428dd620f2b71789c656e90` |
| Seed helper SHA-256 | `953912b7456c71d8ed849626389fa483cec99deb20b93cd03e963d7bd9e56ba8` |

Private campaign manifests bind source, binary and report identities. Raw reports, fixture values, mapping tables and profiles are not included here. The acceptance evaluator verifies report hashes and retains unsuccessful outcomes. [The evaluator guide](2026-11-11-baseline-acceptance-evaluator.md) describes incomplete-evidence handling.

The remaining non-text campaign will use a separate reviewed source identity containing the actual throughput origin and observed extraction/fairness warm-up fields. Historical reports will not be backfilled, and operation pairs or repetitions from incompatible identities will not be silently combined. [Measurement-boundary observations](2026-11-11-measurement-boundary-observations.md) explain the distinction.

## Verification and integration status

Collection, acceptance analysis, final local checks, and the independent whole-branch review are complete. The publishing pull request records required CI and merge status; final primary-checkout synchronization is verified during integration. Performance qualification remains false regardless of those development checks.

## Supporting measurements

### Completed text matrix

All 36 text comparisons are retained: three sizes, four concurrency levels, and three repetitions per combination. Each arm completed 36,072 successful operations in total (1,000 per repetition at one, four, and eight clients; 1,008 at sixteen clients). Percentiles remain separated by size, concurrency, and repetition. This completes collection of the normal text matrix, not qualification of Task 04.

After the final text report was saved, the planned source transition stopped the original controller. The first throughput attempt was interrupted and retained as failed; it contributes no qualifying throughput evidence. No reference workers remained before preparing the reviewed candidate for the remaining workload groups.


### Optimized component profiling

All six components completed separate unprofiled timing and CPU/heap/mutex/block profile runs on Go 1.26.11, using reviewed source `25a96b16d23caf7658370ea92b63721aab5b4815`. The command used `-benchtime=3s -cpu=1,4 -count=1` without the race detector. All 12 invocations produced benchmark rows with positive iteration counts; all 24 profile files parsed successfully, and heap analysis examined both allocation volume and retained heap. Normal fixtures used a 1 MiB request, 50 regex rules, 1,000 dictionary entries, and a seeded 10,000-pair vault. Audit durability uses its own verified decision-log fixture.

| Component | GOMAXPROCS=1 ms/op | GOMAXPROCS=4 ms/op | Interpretation |
| --- | ---: | ---: | --- |
| Detection | 765.67 | 763.07 | Dominant measured request-processing cost |
| Request walk including detection | 760.21 | 761.75 | Includes detection; do not add these two rows |
| Restoration, warm snapshot | 3.84 | 4.11 | Component corpus, separate from streamed response acceptance |
| Restoration, cold snapshot | 4.79 | 4.77 | Includes rebuilding the snapshot |
| Vault save | 15.82 | 15.42 | Seeded-vault serialization and persistence |
| Mapping churn | 1.09 | 1.09 | Component operation, separate from paced end-to-end churn |
| Audit durability, serial | 4.33 | 4.39 | Record persistence verified |
| Audit durability, parallel | 4.23 | 4.43 | Aggregate ns/op, not individual latency under contention |

The detection CPU profile attributes 93.51% cumulative CPU samples to `regexp.(*machine).match`; the request-walk profile attributes 82.30% to the same routine. These component timings and profiles support regex matching as the dominant measured text-processing CPU cost for this fixture, consistent with the first-response failures. They are separate runs and cannot be subtracted to infer adapter overhead. Each detection/request-walk timing row contains only four benchmark iterations; these are diagnostic measurements, not a substitute for the completed 1,000-plus-operation paired distributions. GOMAXPROCS settings do not represent concurrent client counts.

Allocation profiles identify buffer growth (26.93% flat allocation volume) and JSON unquoting (9.70%) in request walking. In mapping churn, restoration snapshot construction accounts for 87.71% flat and 99.54% cumulative sampled allocation volume. These findings identify allocation work worth investigating; they do not prove the cause of the end-to-end RSS excess or establish a leak. Retained-heap snapshots from standalone benchmarks do not represent peak proxy RSS. Profiles include setup and, where applicable, multiple subcases.

The audit benchmark verified persisted record counts against iterations in each serial/parallel timing row. Its CPU profile is dominated by system calls; mutex/block profiles are retained for contention analysis. No audit durability check was relaxed.

Candidate build provenance for the remaining workload groups:

| Binary | SHA-256 |
| --- | --- |
| proxy | `de1e9de59780089ba3bbc05b0c717aa2e35b1e53f8ef4d74e9547e40074a7d61` |
| seed | `4535fee7a901d8999a233f119dc2e398c59aa1139f03d21a48920a84501dd7e2` |

These results remain separate from the earlier `f0c3f37` text campaign. The reviewed candidate also passed the native macOS churn smoke test before the remaining 87-job reference campaign began. Full throughput, extraction, fairness, overload, stress, and paced churn evidence remains pending.


### Rescanning and restoration benchmark scope

The retained profiling fixtures contain 40 input items totaling 1 MiB. `BenchmarkRequestWalk` sanitizes the complete request on each timed iteration, so the reported request-walking cost includes repeated full-history rescanning. It does not measure incremental history processing or establish the benefit of caching. `BenchmarkRestoration` runs both boolean cold-state cases: the cold case constructs a restorer inside every timed iteration before restoring text, while the warm case reuses its restorer. This measures reconstruction versus reuse with the retained fixture and vault population; it is not a general forecast for changing production histories. Component allocation/heap profiles remain local and do not establish a leak or an end-to-end RSS cause.

### Throughput reference results

All nine throughput pairs are complete: three repetitions at each of 256 KiB, 768 KiB, and 1 MiB. Extraction collection has started; fairness and overload collection remain pending.

Across these nine pairs, all 36 before/after host snapshots agree on hardware, OS, architecture, Go version, logical CPUs, memory, AC power, and low-power mode. Load averages are not treated as identity fields. These snapshots do not prove continuous power or load stability throughout the windows.

Each ratio uses successful completions within the actual 600-second window. Total phase counts below also include requests completing outside that window; they are not the throughput numerator. The gate is at least 85% of bypass throughput.

| Size | Repetition | Proxy window successes | Bypass window successes | Ratio | Gate | Peak proxy RSS |
| --- | --- | ---: | ---: | ---: | --- | ---: |
| 256 KiB | 1/3 | 1,096 | 1,226 | 89.40% | Pass | 157.2 MiB |
| 256 KiB | 2/3 | 1,096 | 1,227 | 89.32% | Pass | 161.8 MiB |
| 256 KiB | 3/3 | 1,096 | 1,228 | 89.25% | Pass | 157.3 MiB |
| 768 KiB | 1/3 | 956 | 1,218 | 78.49% | Fail | 384.1 MiB |
| 768 KiB | 2/3 | 976 | 1,276 | 76.49% | Fail | 383.5 MiB |
| 768 KiB | 3/3 | 924 | 1,228 | 75.24% | Fail | 385.1 MiB |
| 1 MiB | 1/3 | 848 | 1,215 | 69.79% | Fail | 490.2 MiB |
| 1 MiB | 2/3 | 848 | 1,216 | 69.74% | Fail | 483.1 MiB |
| 1 MiB | 3/3 | 876 | 1,220 | 71.80% | Fail | 491.5 MiB |

Every attempt in these nine completed pairs succeeded. Sampled resource evidence is complete and passes the 512 MiB proxy and 1 GiB child budgets for each pair; observed child RSS is zero. This does not erase the separate fixed-count text memory failures.

| Size | Proxy total successes across completed repetitions | Bypass total successes across completed repetitions |
| --- | ---: | ---: |
| 256 KiB | 3,300 | 3,693 |
| 768 KiB | 2,868 | 3,735 |
| 1 MiB | 2,584 | 3,663 |

All three 256 KiB repetitions pass the throughput gate. All three 768 KiB repetitions and all three 1 MiB repetitions fail it. Ratios are evaluated separately, without pooling repetitions.

| Size | Body inspection p95 range (ms) | Audit write p95 range (ms) |
| --- | ---: | ---: |
| 256 KiB | 233.6–248.4 | 12.5–13.1 |
| 768 KiB | 604.9–635.6 | 12.8–13.4 |
| 1 MiB (three repetitions) | 807.9–826.4 | 12.8–13.0 |

Resource observations begin after warm-up. Final stage snapshots include warm-up observations; stage percentiles are diagnostic elapsed times, cannot be summed, and do not establish CPU attribution. Body inspection covers broader request sanitization and bookkeeping. The separate optimized CPU profiles identify regex processing as a major measured cost.

The final pair retained 880/880 successful proxy operations and 1,224/1,224 successful bypass operations. Its actual windows contain 876 and 1,220 successes respectively, giving 71.8033%; peak sampled proxy RSS is 515,325,952 bytes. The three 1 MiB ratios span 69.74–71.80%, all below the 85% gate. Full performance qualification remains unproven.

### Extraction results

All 60 extraction cases are complete, with 6,000/6,000 measured requests successful. 30 of 30 four-client cases passed every measured extraction gate. The 30 single-client cases are capacity observations. Mixed-workload fairness and overload measurements remain pending, so this collection does not establish overall qualification. The image latency outlier documented below remains part of the baseline.

The full image matrix is complete: 48 cases across four format/dimension templates, one and four clients, distinct and repeated inputs, and three repetitions per combination. All 4,800 measured requests succeeded. All 24 four-client cases passed their measured extraction, queue, combined-budget, sampling, correctness, and resource gates. The 24 single-client cases are capacity observations. The earlier JPEG latency tail remains recorded below. PDF extraction and the remaining campaign sections are still required before the full baseline is complete.

All 60 completed extraction reports record host metadata consistent with the throughput reference for hardware, architecture, OS, Go version, CPU count, memory, AC power, and low-power mode. Extraction reports provide one host snapshot each; these are not continuous observations or before/after pairs. Load average is excluded from identity comparisons.

60 of 60 extraction jobs are complete. The first three use 1024 × 768 PNG images (template 0), one client, and distinct inputs. Each measured 100 successful requests after at least 100 warm-up completions and 60 seconds. Each retained 100 extraction-run and 100 queue-wait samples with zero polling errors.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Resource result |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1/3 | 100/100 | 189.214 | 0.005666 | 195.781 | 67,403,776 | 47,579,136 | Pass |
| 2/3 | 100/100 | 185.415 | 0.005458 | 190.641 | 66,977,792 | 46,563,328 | Pass |
| 3/3 | 100/100 | 186.379 | 0.005209 | 192.948 | 67,223,552 | 47,022,080 | Pass |

These are single-client capacity results. The acceptance evaluator intentionally leaves sampling and timing qualification incomplete because the OCR acceptance gate requires four clients and achieved concurrency of four. These timings do not prove the four-client gate or simultaneous text fairness. The first four-client result is recorded below; remaining extraction, fairness, overload, stress, and churn evidence is still required.

#### Four-client extraction observations

The first distinct-input 1024 × 768 PNG repetition achieved concurrency four and completed all 100 measured requests successfully. Warm-up recorded 200 completions over 111.56 seconds. Both timing stages retained 100 samples with zero polling errors.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Measured gates |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1/3 | 100/100 | 192.712 | 65.544375 | 357.612 | 96,059,392 | 6,062,080 | Pass |
| 2/3 | 100/100 | 189.940 | 62.931625 | 347.662 | 94,093,312 | 91,963,392 | Pass |
| 3/3 | 100/100 | 191.760 | 69.729000 | 365.746 | 94,289,920 | 77,365,248 | Pass |

Correctness, extraction-run latency, queue wait, combined extraction budget, sampling, and sampled resource checks pass for all three completed repetitions. This does not establish overall performance qualification: repeated-image cases, other image and PDF cases, simultaneous text fairness, overload, stress, and churn evidence remain outstanding. Single-client repeated-image results follow.

#### Repeated-image single-client observations

All three repeated-input 1024 × 768 PNG cases each completed all 100 requests successfully, retaining 100 samples per extraction stage and zero polling errors after the required warm-up. This is capacity evidence; the four-client timing gate remains unproven for repeated images.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Resource result |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1/3 | 100/100 | 188.047 | 0.004958 | 230.603 | 66,060,288 | 47,710,208 | Pass |
| 2/3 | 100/100 | 186.502 | 0.004583 | 192.201 | 66,076,672 | 46,399,488 | Pass |
| 3/3 | 100/100 | 186.713 | 0.004500 | 198.901 | 65,748,992 | 46,989,312 | Pass |

Four-client repeated-image results follow.

#### Repeated-image four-client observations

All three completed repeated-input 1024 × 768 PNG runs achieved concurrency four, completed all 100 requests successfully per repetition, retained 100 samples per extraction stage with zero polling errors, and met the warm-up requirements.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Measured gates |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1/3 | 100/100 | 190.780 | 73.379209 | 391.997 | 95,272,960 | 85,180,416 | Pass |
| 2/3 | 100/100 | 189.667 | 96.940917 | 372.917 | 74,776,576 | 58,163,200 | Pass |
| 3/3 | 100/100 | 192.422 | 90.429792 | 358.497 | 94,388,224 | 93,716,480 | Pass |

Correctness, run latency, queue wait, combined extraction budget, sampling, and sampled resources pass for all three repetitions. The 1024 × 768 PNG matrix is complete: twelve runs, 1,200/1,200 successful measured requests, covering distinct and repeated images at one and four clients with three repetitions each. The next case uses 1024 × 768 JPEG images. Full qualification still requires the remaining repetitions, other cases, and simultaneous text fairness, and the previously observed text and throughput gate failures remain.

#### JPEG single-client distinct-input observations

All three 1024 × 768 JPEG cases each completed all 100 requests successfully after the required warm-up. Both extraction stages retained 100 samples per repetition with zero polling errors. These are single-client capacity observations; four-client timing qualification is pending.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Resource result |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1/3 | 100/100 | 181.425 | 0.004583 | 190.077 | 65,929,216 | 40,484,864 | Pass |
| 2/3 | 100/100 | 182.689 | 0.004167 | 187.432 | 68,894,720 | 30,736,384 | Pass |
| 3/3 | 100/100 | 179.776 | 0.005416 | 191.914 | 66,322,432 | 41,304,064 | Pass |

Four-client distinct-input JPEG results follow.

#### JPEG four-client distinct-input observations

All three completed distinct-input 1024 × 768 JPEG runs achieved concurrency four, completed all 100 requests successfully per repetition, retained 100 samples per extraction stage with zero polling errors, and met the warm-up requirements.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Measured gates |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1/3 | 100/100 | 183.322 | 73.307583 | 345.416 | 94,552,064 | 82,231,296 | Pass |
| 2/3 | 100/100 | 182.874 | 73.350167 | 316.127 | 95,322,112 | 83,181,568 | Pass |
| 3/3 | 100/100 | 184.951 | 85.818625 | 349.075 | 95,141,888 | 74,711,040 | Pass |

Correctness, extraction-run latency, queue wait, combined extraction budget, sampling, and sampled resource checks pass for all three repetitions. All six distinct-input JPEG cases are complete, totaling 600/600 successful measured requests across one and four clients. Single-client repeated-input JPEG results follow. This does not establish full qualification; remaining workloads and the already recorded text and throughput failures must remain in the final assessment.

#### JPEG single-client repeated-input observations

All three repeated-input 1024 × 768 JPEG cases each completed all 100 requests successfully after the required warm-up. Both extraction stages retained 100 samples per repetition with zero polling errors. This is capacity evidence; four-client repeated-input JPEG qualification remains pending.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Resource result |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1/3 | 100/100 | 181.619 | 0.004334 | 185.591 | 67,518,464 | 41,041,920 | Pass |
| 2/3 | 100/100 | 181.391 | 0.004042 | 185.724 | 67,469,312 | 41,189,376 | Pass |
| 3/3 | 100/100 | 182.914 | 0.006000 | 205.942 | 67,338,240 | 38,191,104 | Pass |

Four-client repeated-input JPEG results follow.

#### JPEG four-client repeated-input observations

All three completed repeated-input 1024 × 768 JPEG runs achieved concurrency four, completed all 100 requests successfully per repetition, retained 100 samples per extraction stage with zero polling errors, and met the warm-up requirements.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Measured gates |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1/3 | 100/100 | 184.238 | 88.577834 | 329.289 | 94,093,312 | 65,552,384 | Pass |
| 2/3 | 100/100 | 186.219 | 96.172792 | 4297.069 | 95,272,960 | 81,133,568 | Pass |
| 3/3 | 100/100 | 189.518 | 84.536709 | 356.714 | 96,403,456 | 75,038,720 | Pass |

Correctness, extraction-run latency, queue wait, combined extraction budget, sampling, and sampled resources pass for all three repetitions. The 1024 × 768 JPEG matrix is complete: twelve runs and 1,200/1,200 successful measured requests across distinct/repeated inputs and one/four clients. The next case uses 1920 × 1080 PNG images. Full qualification remains incomplete; previously recorded text and throughput failures remain part of the assessment.

Repetition 2 contains a latency tail: its largest paired extraction observation is 4,297.069 ms (4,289.862 ms run plus 7.207 ms queue). Two run observations and two queue observations exceed one second; four paired observations exceed one second. Maximum full request duration is 6,480.116 ms, including backend and other request work. These observations are retained, despite the p95 and 30-second budget gates passing. The recorded evidence does not establish the cause of this tail.

#### Larger PNG single-client distinct-input observations

All three repetitions are complete for distinct 1920 × 1080 PNG inputs with one client. These are capacity observations; the timing acceptance gates require four clients.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Resource result |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1/3 | 100/100 | 235.923 | 0.004417 | 248.698 | 66,306,048 | 80,199,680 | Pass |
| 2/3 | 100/100 | 235.600 | 0.005166 | 245.287 | 67,239,936 | 77,922,304 | Pass |
| 3/3 | 100/100 | 236.273 | 0.004792 | 239.407 | 65,732,608 | 77,676,544 | Pass |

All three repetitions completed 100/100 successful requests, retained 100 run and queue samples with zero polling errors, and passed sampled resource limits. The four-client distinct-input observations follow below. Overall qualification remains incomplete.

#### Larger PNG four-client distinct-input observations

All three repetitions are complete for distinct 1920 × 1080 PNG inputs with four clients. All passed correctness, extraction time, queue time, combined budget, sampling, and resource gates.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Measured gates |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1/3 | 100/100 | 238.051 | 76.250750 | 432.964 | 95,338,496 | 155,516,928 | Pass |
| 2/3 | 100/100 | 238.222 | 73.755125 | 430.588 | 95,059,968 | 157,384,704 | Pass |
| 3/3 | 100/100 | 235.444 | 83.744875 | 454.489 | 74,235,904 | 148,488,192 | Pass |

Each run achieved four concurrent clients and retained 100 run and queue samples with zero polling errors. The first two runs warmed up with 200 successful requests over at least 120 seconds; the third used 100 successful requests over 60.08 seconds. All satisfy the required warm-up minimums. The single-client repeated-image observations follow below. Overall qualification remains incomplete.

#### Larger PNG single-client repeated-input observations

All three repetitions are complete for a repeated 1920 × 1080 PNG input with one client. These are capacity observations; the timing acceptance gates require four clients.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Resource result |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1/3 | 100/100 | 237.149 | 0.005917 | 241.190 | 67,026,944 | 80,134,144 | Pass |
| 2/3 | 100/100 | 237.553 | 0.005000 | 242.585 | 66,306,048 | 79,069,184 | Pass |
| 3/3 | 100/100 | 239.538 | 0.005709 | 267.499 | 65,798,144 | 78,561,280 | Pass |

All three runs retained 100 run and queue samples with zero polling errors after 100 successful warm-up requests over at least 238 seconds. The four-client repeated-image observations follow below. Overall qualification remains incomplete.

#### Larger PNG four-client repeated-input observations

All three repetitions are complete for a repeated 1920 × 1080 PNG input with four clients. All passed correctness, extraction time, queue time, combined budget, sampling, and resource gates.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Measured gates |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1/3 | 100/100 | 236.880 | 71.077542 | 410.282 | 95,813,632 | 158,400,512 | Pass |
| 2/3 | 100/100 | 235.716 | 73.948792 | 464.517 | 94,699,520 | 153,944,064 | Pass |
| 3/3 | 100/100 | 242.341 | 68.238959 | 437.318 | 94,830,592 | 156,401,664 | Pass |

All three runs achieved four concurrent clients and retained 100 run and queue samples with zero polling errors after 200 successful warm-up requests. Warm-up durations were 120.03, 119.80, and 120.16 seconds, respectively. This completes all 12 larger-PNG cases, with 1,200/1,200 successful measured requests. The larger-JPEG observations follow below. Overall qualification remains incomplete.

#### Larger JPEG single-client distinct-input observations

All three repetitions are complete for distinct 1920 × 1080 JPEG inputs with one client. These are capacity observations; the timing acceptance gates require four clients.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Resource result |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1/3 | 100/100 | 219.353 | 0.005042 | 223.167 | 66,502,656 | 52,183,040 | Pass |
| 2/3 | 100/100 | 217.560 | 0.005125 | 225.303 | 67,059,712 | 62,160,896 | Pass |
| 3/3 | 100/100 | 217.915 | 0.005083 | 223.397 | 66,945,024 | 61,194,240 | Pass |

All three runs retained 100 run and queue samples with zero polling errors after 100 successful warm-up requests over at least 236 seconds. The four-client distinct-input observations follow below. Overall qualification remains incomplete.

#### Larger JPEG four-client distinct-input observations

All three repetitions are complete for distinct 1920 × 1080 JPEG inputs with four clients. All passed correctness, extraction time, queue time, combined budget, sampling, and resource gates.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Measured gates |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1/3 | 100/100 | 213.686 | 66.527167 | 417.397 | 93,470,720 | 118,030,336 | Pass |
| 2/3 | 100/100 | 215.417 | 69.630417 | 401.674 | 93,782,016 | 119,226,368 | Pass |
| 3/3 | 100/100 | 218.980 | 68.583875 | 369.433 | 94,339,072 | 95,715,328 | Pass |

All three runs achieved four concurrent clients and retained 100 run and queue samples with zero polling errors after 200 successful warm-up requests. Warm-up durations were 119.29, 119.37, and 119.09 seconds, respectively. The single-client repeated-input observations follow below. Overall qualification remains incomplete.

#### Larger JPEG single-client repeated-input observations

All three repetitions are complete for a repeated 1920 × 1080 JPEG input with one client. These are capacity observations; the timing acceptance gates require four clients.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Resource result |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1/3 | 100/100 | 216.825 | 0.005417 | 222.259 | 67,059,712 | 63,094,784 | Pass |
| 2/3 | 100/100 | 218.595 | 0.005042 | 228.528 | 67,420,160 | 63,389,696 | Pass |
| 3/3 | 100/100 | 219.204 | 0.005958 | 253.298 | 66,797,568 | 57,098,240 | Pass |

All three runs retained 100 run and queue samples with zero polling errors after 100 successful warm-up requests over at least 236 seconds. The four-client repeated-input observations follow below. Overall qualification remains incomplete.

#### Larger JPEG four-client repeated-input observations

All three repetitions are complete for a repeated 1920 × 1080 JPEG input with four clients. All passed correctness, extraction time, queue time, combined budget, sampling, and resource gates.

| Repetition | Successful requests | Extraction-run p95 (ms) | Queue-wait p95 (ms) | Maximum run plus wait (ms) | Peak proxy RSS (bytes) | Peak child RSS (bytes) | Measured gates |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1/3 | 100/100 | 215.366 | 65.273125 | 425.253 | 94,273,536 | 122,781,696 | Pass |
| 2/3 | 100/100 | 214.123 | 68.567875 | 406.481 | 95,354,880 | 120,864,768 | Pass |
| 3/3 | 100/100 | 215.750 | 70.855875 | 419.583 | 95,567,872 | 116,293,632 | Pass |

All three runs achieved four concurrent clients and retained 100 run and queue samples with zero polling errors after 200 successful warm-up requests. Warm-up durations were 119.25, 119.29, and 119.00 seconds, respectively. This completes all 12 larger-JPEG cases. The text-PDF observations follow below. Overall qualification remains incomplete.

#### Text-PDF single-client observations

3 of three repetitions are complete for the two-page pdf-text fixture. These are capacity observations; timing acceptance requires four clients. Overall Task 04 evidence remains incomplete.

| Repetition | Successful requests | Extraction-run p95 ms | Queue-wait p95 ms | Maximum run plus wait ms | Proxy RSS bytes | Child RSS bytes | Resource result |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1/3 | 100/100 | 178.223 | 0.029582 | 203.666 | 68,861,952 | 32,768 | PASS |
| 2/3 | 100/100 | 177.748 | 0.032791 | 195.954 | 68,091,904 | 1,425,408 | PASS |
| 3/3 | 100/100 | 176.999 | 0.051751 | 189.137 | 67,551,232 | 1,703,936 | PASS |

These runs retained 100 extraction-run and 100 queue samples each, with 0 stage polling errors. Each completed at least 100 warm-up requests over at least 232.470 seconds. Child memory is a sampled peak and may miss short-lived processes.

#### Text-PDF four-client observations

3 of three repetitions are complete for the two-page pdf-text fixture. The table records each measured gate without treating this subgroup as full qualification. Overall Task 04 evidence remains incomplete.

| Repetition | Successful requests | Extraction-run p95 ms | Queue-wait p95 ms | Maximum run plus wait ms | Proxy RSS bytes | Child RSS bytes | Measured gates |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1/3 | 100/100 | 182.027 | 137.932540 | 317.979 | 75,104,256 | 1,671,168 | PASS |
| 2/3 | 100/100 | 196.143 | 138.819834 | 411.300 | 74,727,424 | 9,076,736 | PASS |
| 3/3 | 100/100 | 185.862 | 104.330666 | 292.074 | 95,813,632 | 6,094,848 | PASS |

These runs retained 100 extraction-run and 100 queue samples each, with 0 stage polling errors. Each completed at least 100 warm-up requests over at least 60.277 seconds. Child memory is a sampled peak and may miss short-lived processes.

#### Scanned-PDF single-client observations

3 of three repetitions are complete for the two-page pdf-scan fixture. These are capacity observations; timing acceptance requires four clients. Overall Task 04 evidence remains incomplete.

| Repetition | Successful requests | Extraction-run p95 ms | Queue-wait p95 ms | Maximum run plus wait ms | Proxy RSS bytes | Child RSS bytes | Resource result |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1/3 | 100/100 | 2455.235 | 0.101416 | 2500.186 | 66,551,808 | 221,757,440 | PASS |
| 2/3 | 100/100 | 2465.499 | 0.129459 | 2604.130 | 68,124,672 | 221,151,232 | PASS |
| 3/3 | 100/100 | 2449.183 | 0.074417 | 2468.309 | 67,125,248 | 220,086,272 | PASS |

These runs retained 100 extraction-run and 100 queue samples each, with 0 stage polling errors. Each completed at least 100 warm-up requests over at least 458.232 seconds. Child memory is a sampled peak and may miss short-lived processes.

#### Scanned-PDF four-client observations

The scanned-PDF queue p95 ranged from 1855.623 to 1875.104 ms across the three four-client repetitions. The smallest observed margin to the 2,000 ms queue limit was 124.896 ms. These runs pass, but provide limited observed queue-time headroom; they do not establish capacity above four clients or predict mixed-workload behavior.

3 of three repetitions are complete for the two-page pdf-scan fixture. The table records each measured gate without treating this subgroup as full qualification. Overall Task 04 evidence remains incomplete.

| Repetition | Successful requests | Extraction-run p95 ms | Queue-wait p95 ms | Maximum run plus wait ms | Proxy RSS bytes | Child RSS bytes | Measured gates |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1/3 | 100/100 | 2491.763 | 1855.622876 | 4911.414 | 74,874,880 | 438,042,624 | PASS |
| 2/3 | 100/100 | 2506.808 | 1875.103751 | 4938.892 | 75,726,848 | 442,843,136 | PASS |
| 3/3 | 100/100 | 2487.587 | 1863.826125 | 4927.664 | 74,153,984 | 434,733,056 | PASS |

These runs retained 100 extraction-run and 100 queue samples each, with 0 stage polling errors. Each completed at least 100 warm-up requests over at least 127.924 seconds. Child memory is a sampled peak and may miss short-lived processes.

### Mixed-workload fairness results

Across all three completed 256 KiB fairness repetitions, added first-byte p95 was 290.924–294.215 ms, failing the 150 ms gate each time. First-byte p99, relay, correctness, and resource checks passed for those repetitions. These results do not qualify larger workloads.

Across all three completed 768 KiB fairness repetitions, added first-byte p95 was 670.115–672.356 ms and p99 was 707.843–735.166 ms. Every repetition failed both the 150 ms p95 and 500 ms p99 gates. Correctness, relay latency, extraction timing, and resource checks passed.

All three 1 MiB fairness repetitions fail the added first-byte limits: p95 is 903.929–938.002 ms (limit 150 ms) and p99 is 993.323–1031.314 ms (limit 500 ms). Correctness, relay delay, extraction timing, and sampled resources pass in all three repetitions. The sampled memory passes in these mixed-workload cases do not supersede the 1 MiB text-only resource failures; these are different workload groups and are evaluated separately.

All 36 before/after environment snapshots from the nine completed fairness cases match the extraction reference across eight recorded fields. These snapshots are not continuous environment monitoring.

9 of nine paired fairness cases are complete. Each case retains its own arms and request counts; image populations and latency distributions are not pooled. Numerical acceptance failures remain part of the baseline. Overall qualification remains false.

| Text size | Repetition | Added first-byte p95 ms | Added first-byte p99 ms | Added relay p99 ms | Proxy RSS bytes | Child RSS bytes |
| --- | --- | --- | --- | --- | --- | --- |
| 256 KiB | 1/3 | 292.497 | 302.379 | 60.573 | 373,243,904 | 157,859,840 |
| 256 KiB | 2/3 | 290.924 | 299.200 | 60.587 | 372,801,536 | 154,861,568 |
| 256 KiB | 3/3 | 294.215 | 313.846 | 61.142 | 371,474,432 | 155,516,928 |
| 768 KiB | 1/3 | 670.115 | 735.166 | 55.338 | 448,004,096 | 160,579,584 |
| 768 KiB | 2/3 | 671.252 | 728.844 | 57.829 | 442,990,592 | 159,006,720 |
| 768 KiB | 3/3 | 672.356 | 707.843 | 60.042 | 456,179,712 | 159,973,376 |
| 1024 KiB | 1/3 | 938.002 | 1031.314 | 54.219 | 496,386,048 | 160,595,968 |
| 1024 KiB | 2/3 | 903.929 | 993.323 | 57.531 | 497,598,464 | 158,285,824 |
| 1024 KiB | 3/3 | 913.019 | 1012.313 | 61.626 | 495,452,160 | 160,071,680 |

The unchanged latency limits are added first-byte p95 150 ms, first-byte p99 500 ms, and relay p99 100 ms. The table values retain each repetition independently.

For 256 KiB repetition 1/3, text successes were 1,000/1,000 through the proxy and 1,000/1,000 through the direct backend. Image successes were 2,367/2,367 through the proxy and 2,912/2,912 through the direct backend. Text sampling: pass; correctness: pass; combined first-byte gate: fail; relay: pass; resources: pass. First-byte and relay sample counts are 1,000 and 32,000.

Concurrent image run p95 is 231.096 ms (pass), queue p95 103.722 ms (pass), and maximum paired run plus wait 379.531 ms (pass); image sampling: pass.

The bypass arm retained a 1930.918 second measured window after 122 warm-up operations over 63.804 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

The proxy arm retained a 2151.013 second measured window after 101 warm-up operations over 65.605 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

For 256 KiB repetition 2/3, text successes were 1,000/1,000 through the proxy and 1,000/1,000 through the direct backend. Image successes were 2,367/2,367 through the proxy and 2,911/2,911 through the direct backend. Text sampling: pass; correctness: pass; combined first-byte gate: fail; relay: pass; resources: pass. First-byte and relay sample counts are 1,000 and 32,000.

Concurrent image run p95 is 230.306 ms (pass), queue p95 101.441 ms (pass), and maximum paired run plus wait 392.888 ms (pass); image sampling: pass.

The proxy arm retained a 2151.620 second measured window after 101 warm-up operations over 65.358 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

The bypass arm retained a 1932.691 second measured window after 123 warm-up operations over 61.738 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

For 256 KiB repetition 3/3, text successes were 1,000/1,000 through the proxy and 1,000/1,000 through the direct backend. Image successes were 2,357/2,357 through the proxy and 2,920/2,920 through the direct backend. Text sampling: pass; correctness: pass; combined first-byte gate: fail; relay: pass; resources: pass. First-byte and relay sample counts are 1,000 and 32,000.

Concurrent image run p95 is 234.126 ms (pass), queue p95 99.509 ms (pass), and maximum paired run plus wait 705.411 ms (pass); image sampling: pass.

The bypass arm retained a 1901.646 second measured window after 124 warm-up operations over 61.800 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

The proxy arm retained a 2129.495 second measured window after 100 warm-up operations over 63.808 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

For 768 KiB repetition 1/3, text successes were 1,000/1,000 through the proxy and 1,000/1,000 through the direct backend. Image successes were 2,790/2,790 through the proxy and 2,949/2,949 through the direct backend. Text sampling: pass; correctness: pass; combined first-byte gate: fail; relay: pass; resources: pass. First-byte and relay sample counts are 1,000 and 32,000.

Concurrent image run p95 is 238.551 ms (pass), queue p95 86.336 ms (pass), and maximum paired run plus wait 438.724 ms (pass); image sampling: pass.

The bypass arm retained a 1901.049 second measured window after 126 warm-up operations over 61.445 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

The proxy arm retained a 2435.787 second measured window after 104 warm-up operations over 67.122 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

For 768 KiB repetition 2/3, text successes were 1,000/1,000 through the proxy and 1,000/1,000 through the direct backend. Image successes were 2,803/2,803 through the proxy and 2,956/2,956 through the direct backend. Text sampling: pass; correctness: pass; combined first-byte gate: fail; relay: pass; resources: pass. First-byte and relay sample counts are 1,000 and 32,000.

Concurrent image run p95 is 236.261 ms (pass), queue p95 85.384 ms (pass), and maximum paired run plus wait 4275.093 ms (pass); image sampling: pass.

The proxy arm retained a 2452.937 second measured window after 104 warm-up operations over 67.477 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

The bypass arm retained a 1871.242 second measured window after 119 warm-up operations over 62.166 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

For 768 KiB repetition 3/3, text successes were 1,000/1,000 through the proxy and 1,000/1,000 through the direct backend. Image successes were 2,799/2,799 through the proxy and 2,942/2,942 through the direct backend. Text sampling: pass; correctness: pass; combined first-byte gate: fail; relay: pass; resources: pass. First-byte and relay sample counts are 1,000 and 32,000.

Concurrent image run p95 is 235.797 ms (pass), queue p95 86.498 ms (pass), and maximum paired run plus wait 554.139 ms (pass); image sampling: pass.

The bypass arm retained a 1858.933 second measured window after 129 warm-up operations over 63.070 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

The proxy arm retained a 2470.810 second measured window after 104 warm-up operations over 67.853 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

For 1024 KiB repetition 1/3, text successes were 1,000/1,000 through the proxy and 1,000/1,000 through the direct backend. Image successes were 3,012/3,012 through the proxy and 2,972/2,972 through the direct backend. Text sampling: pass; correctness: pass; combined first-byte gate: fail; relay: pass; resources: pass. First-byte and relay sample counts are 1,000 and 32,000.

Concurrent image run p95 is 253.715 ms (pass), queue p95 95.384 ms (pass), and maximum paired run plus wait 499.631 ms (pass); image sampling: pass.

The bypass arm retained a 1856.625 second measured window after 120 warm-up operations over 60.154 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

The proxy arm retained a 2633.181 second measured window after 103 warm-up operations over 67.521 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

For 1024 KiB repetition 2/3, text successes were 1,000/1,000 through the proxy and 1,000/1,000 through the direct backend. Image successes were 3,003/3,003 through the proxy and 2,962/2,962 through the direct backend. Text sampling: pass; correctness: pass; combined first-byte gate: fail; relay: pass; resources: pass. First-byte and relay sample counts are 1,000 and 32,000.

Concurrent image run p95 is 238.977 ms (pass), queue p95 100.025 ms (pass), and maximum paired run plus wait 711.934 ms (pass); image sampling: pass.

The proxy arm retained a 2646.408 second measured window after 103 warm-up operations over 70.584 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

The bypass arm retained a 1885.378 second measured window after 126 warm-up operations over 60.652 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

For 1024 KiB repetition 3/3, text successes were 1,000/1,000 through the proxy and 1,000/1,000 through the direct backend. Image successes were 3,003/3,003 through the proxy and 2,954/2,954 through the direct backend. Text sampling: pass; correctness: pass; combined first-byte gate: fail; relay: pass; resources: pass. First-byte and relay sample counts are 1,000 and 32,000.

Concurrent image run p95 is 240.941 ms (pass), queue p95 104.752 ms (pass), and maximum paired run plus wait 447.986 ms (pass); image sampling: pass.

The bypass arm retained a 1859.761 second measured window after 122 warm-up operations over 60.067 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

The proxy arm retained a 2699.260 second measured window after 103 warm-up operations over 69.273 seconds; achieved client concurrency was 4, stage polling errors 0, and aborted status false.

0 fairness cases remain. Overload, stress, and churn evidence and final review/integration are still required.

Fairness stage diagnostic scope: for 256 KiB repetition 1/3, the final proxy snapshot reports body-inspection p95 782.619 ms, audit-write p95 6.645 ms, metadata-inspection p95 0.418 ms, and ingress-read p95 11.894 ms, each with 3,469 retained samples. These stage distributions combine text and image requests and include warm-up; they are not text-only paired measurements or CPU profiles. Their percentiles must not be added or used to attribute the text latency failure to a specific stage. The four before/after environment snapshots agree on the eight recorded identity/power fields; they are not continuous monitoring.

For the first completed 768 KiB fairness repetition, the final mixed-workload stage snapshots contained 3,895 samples. Body-inspection p95 was 792.223 ms, audit-write p95 6.520 ms, metadata-inspection p95 0.271 ms, and ingress-read p95 9.151 ms. These snapshots include warmup and both text and image operations; they are not the paired text-only latency distribution. Body inspection covers adapter and bookkeeping work beyond detection. These percentiles cannot be added or subtracted to attribute the first-byte regression, and do not replace CPU profiles.

The second completed 768 KiB fairness repetition retained an extraction tail of 4275.093 ms: 4275.024 ms extraction run plus 0.068 ms queue wait at that maximum. Among 2,803 image samples, two run times and one queue wait exceeded one second; three combined samples exceeded one second. The extraction p95 and combined-budget gates still passed. The cause of these tail samples is not established.

### Overload results

All nine overload cases are complete: three repetitions each at 256 KiB, 768 KiB, and 1 MiB. Each case retains both arms at 1, 2, 4, and 8 offered requests per second, with a 120-second measurement window, at least 100 successful warmup requests, and a maximum of 16 outstanding requests. All planned overload phases are retained. These results do not establish overall performance qualification.

| Size KiB | Repetition | Mode | Offered req/s | Scheduled | Successful | Client unsent | Rejected | Maximum request ms | Proxy RSS bytes |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 256 | 0 | bypass | 1 | 120 | 120 | 0 | 0 | 1841.176 | 31965184 |
| 256 | 0 | proxy | 1 | 120 | 120 | 0 | 0 | 2215.588 | 86327296 |
| 256 | 0 | bypass | 2 | 240 | 240 | 0 | 0 | 1851.142 | 34488320 |
| 256 | 0 | proxy | 2 | 240 | 240 | 0 | 0 | 2165.991 | 105185280 |
| 256 | 0 | bypass | 4 | 480 | 480 | 0 | 0 | 1844.897 | 34635776 |
| 256 | 0 | proxy | 4 | 480 | 480 | 0 | 0 | 2145.959 | 136331264 |
| 256 | 0 | bypass | 8 | 960 | 954 | 6 | 0 | 2003.749 | 35061760 |
| 256 | 0 | proxy | 8 | 960 | 897 | 63 | 0 | 2159.730 | 170475520 |
| 256 | 1 | proxy | 1 | 120 | 120 | 0 | 0 | 2218.573 | 84819968 |
| 256 | 1 | bypass | 1 | 120 | 120 | 0 | 0 | 1873.448 | 34701312 |
| 256 | 1 | proxy | 2 | 240 | 240 | 0 | 0 | 2073.116 | 106545152 |
| 256 | 1 | bypass | 2 | 240 | 240 | 0 | 0 | 2007.631 | 31506432 |
| 256 | 1 | proxy | 4 | 480 | 480 | 0 | 0 | 2180.095 | 137068544 |
| 256 | 1 | bypass | 4 | 480 | 480 | 0 | 0 | 1886.394 | 34422784 |
| 256 | 1 | proxy | 8 | 960 | 895 | 65 | 0 | 2144.162 | 171704320 |
| 256 | 1 | bypass | 8 | 960 | 960 | 0 | 0 | 1914.625 | 35110912 |
| 256 | 2 | bypass | 1 | 120 | 120 | 0 | 0 | 1856.362 | 34406400 |
| 256 | 2 | proxy | 1 | 120 | 120 | 0 | 0 | 2223.839 | 84262912 |
| 256 | 2 | bypass | 2 | 240 | 240 | 0 | 0 | 1861.344 | 34701312 |
| 256 | 2 | proxy | 2 | 240 | 240 | 0 | 0 | 2303.219 | 105627648 |
| 256 | 2 | bypass | 4 | 480 | 480 | 0 | 0 | 2086.086 | 34553856 |
| 256 | 2 | proxy | 4 | 480 | 480 | 0 | 0 | 2187.435 | 137854976 |
| 256 | 2 | bypass | 8 | 960 | 917 | 43 | 0 | 2087.301 | 35356672 |
| 256 | 2 | proxy | 8 | 960 | 891 | 69 | 0 | 2170.971 | 169787392 |
| 768 | 0 | bypass | 1 | 120 | 120 | 0 | 0 | 2042.866 | 36241408 |
| 768 | 0 | proxy | 1 | 120 | 120 | 0 | 0 | 2599.249 | 180551680 |
| 768 | 0 | bypass | 2 | 240 | 240 | 0 | 0 | 1827.159 | 36126720 |
| 768 | 0 | proxy | 2 | 240 | 240 | 0 | 0 | 2552.625 | 249085952 |
| 768 | 0 | bypass | 4 | 480 | 480 | 0 | 0 | 2007.310 | 35897344 |
| 768 | 0 | proxy | 4 | 480 | 480 | 0 | 0 | 2533.368 | 349306880 |
| 768 | 0 | bypass | 8 | 960 | 950 | 10 | 0 | 2158.597 | 35962880 |
| 768 | 0 | proxy | 8 | 960 | 740 | 220 | 0 | 3629.115 | 422723584 |
| 768 | 1 | proxy | 1 | 120 | 120 | 0 | 0 | 2572.907 | 186056704 |
| 768 | 1 | bypass | 1 | 120 | 120 | 0 | 0 | 1829.909 | 35946496 |
| 768 | 1 | proxy | 2 | 240 | 240 | 0 | 0 | 2525.871 | 245596160 |
| 768 | 1 | bypass | 2 | 240 | 240 | 0 | 0 | 1859.094 | 36716544 |
| 768 | 1 | proxy | 4 | 480 | 480 | 0 | 0 | 2707.023 | 341327872 |
| 768 | 1 | bypass | 4 | 480 | 480 | 0 | 0 | 1887.656 | 35667968 |
| 768 | 1 | proxy | 8 | 960 | 774 | 186 | 0 | 2571.367 | 426819584 |
| 768 | 1 | bypass | 8 | 960 | 950 | 10 | 0 | 2012.074 | 36257792 |
| 768 | 2 | bypass | 1 | 120 | 120 | 0 | 0 | 1966.661 | 35880960 |
| 768 | 2 | proxy | 1 | 120 | 120 | 0 | 0 | 2532.933 | 184958976 |
| 768 | 2 | bypass | 2 | 240 | 240 | 0 | 0 | 1993.736 | 36372480 |
| 768 | 2 | proxy | 2 | 240 | 240 | 0 | 0 | 2600.465 | 245284864 |
| 768 | 2 | bypass | 4 | 480 | 480 | 0 | 0 | 2050.970 | 36438016 |
| 768 | 2 | proxy | 4 | 480 | 480 | 0 | 0 | 2592.845 | 351387648 |
| 768 | 2 | bypass | 8 | 960 | 918 | 42 | 0 | 2063.596 | 35815424 |
| 768 | 2 | proxy | 8 | 960 | 742 | 218 | 0 | 2623.509 | 418185216 |
| 1024 | 0 | bypass | 1 | 120 | 120 | 0 | 0 | 2007.840 | 35602432 |
| 1024 | 0 | proxy | 1 | 120 | 120 | 0 | 0 | 2628.009 | 230260736 |
| 1024 | 0 | bypass | 2 | 240 | 240 | 0 | 0 | 2044.086 | 36388864 |
| 1024 | 0 | proxy | 2 | 240 | 240 | 0 | 0 | 2775.739 | 327122944 |
| 1024 | 0 | bypass | 4 | 480 | 480 | 0 | 0 | 2039.042 | 35979264 |
| 1024 | 0 | proxy | 4 | 480 | 480 | 0 | 0 | 2795.299 | 451051520 |
| 1024 | 0 | bypass | 8 | 960 | 943 | 17 | 0 | 2016.114 | 35831808 |
| 1024 | 0 | proxy | 8 | 960 | 666 | 294 | 0 | 3514.488 | 530333696 |
| 1024 | 1 | proxy | 1 | 120 | 120 | 0 | 0 | 3009.773 | 234749952 |
| 1024 | 1 | bypass | 1 | 120 | 120 | 0 | 0 | 2017.092 | 35635200 |
| 1024 | 1 | proxy | 2 | 240 | 240 | 0 | 0 | 2770.500 | 322682880 |
| 1024 | 1 | bypass | 2 | 240 | 240 | 0 | 0 | 2012.345 | 36454400 |
| 1024 | 1 | proxy | 4 | 480 | 480 | 0 | 0 | 2874.672 | 455671808 |
| 1024 | 1 | bypass | 4 | 480 | 480 | 0 | 0 | 2035.751 | 37060608 |
| 1024 | 1 | proxy | 8 | 960 | 668 | 292 | 0 | 3177.289 | 529498112 |
| 1024 | 1 | bypass | 8 | 960 | 937 | 23 | 0 | 2066.547 | 37027840 |
| 1024 | 2 | bypass | 1 | 120 | 120 | 0 | 0 | 2019.456 | 36929536 |
| 1024 | 2 | proxy | 1 | 120 | 120 | 0 | 0 | 2768.349 | 234782720 |
| 1024 | 2 | bypass | 2 | 240 | 240 | 0 | 0 | 1863.328 | 35389440 |
| 1024 | 2 | proxy | 2 | 240 | 240 | 0 | 0 | 2727.808 | 318898176 |
| 1024 | 2 | bypass | 4 | 480 | 480 | 0 | 0 | 2011.969 | 36847616 |
| 1024 | 2 | proxy | 4 | 480 | 480 | 0 | 0 | 2845.838 | 455016448 |
| 1024 | 2 | bypass | 8 | 960 | 933 | 27 | 0 | 2060.733 | 37093376 |
| 1024 | 2 | proxy | 8 | 960 | 667 | 293 | 0 | 3248.957 | 530923520 |

Accounting checks pass for all 72 phases, but that does not mean every offered request was served. Unsent requests remain in the scheduled denominator and are not backend rejections. All sent requests in these cases completed successfully; none were rejected.

At 8 offered requests per second, the three 768 KiB proxy repetitions completed 740, 774, and 742 requests, leaving 220, 186, and 218 client-unsent requests. Their paired backend arms completed 950, 950, and 918 requests, leaving 10, 10, and 42 unsent. These offered-load observations do not replace the separate sustained-throughput gate.

At 8 offered requests per second, the first 1 MiB proxy repetition completed 666 requests and left 294 unsent, versus 943 completed and 17 unsent for its paired backend. The proxy peak RSS was 530,333,696 bytes, below the 536,870,912-byte limit by 6,537,216 bytes; this narrow margin does not erase the separate text-only memory failures.

The second 1 MiB repetition at 8 offered requests per second completed 668 proxy requests and left 292 unsent, versus 937 completed and 23 unsent for its paired backend. Proxy peak RSS was 529,498,112 bytes, below the 536,870,912-byte limit by 7,372,800 bytes.

The third 1 MiB repetition at 8 offered requests per second completed 667 proxy requests and left 293 unsent, versus 933 completed and 27 unsent for its paired backend. Proxy peak RSS was 530,923,520 bytes, below the 536,870,912-byte limit by 5,947,392 bytes.

All 72 phases have complete resource samples and pass their resource checks. Observed child RSS is zero. Bypass arms start an unused proxy; its reported RSS is not backend memory usage. All 144 before/after environment snapshots match the eight checked reference fields. These snapshots do not establish continuous environmental stability.

Overload collection is complete. Stress capacity, full mapping churn, final report review, and integration remain required.

### Stress capacity observations

Recorded generator populations for the launched stress cases are below. Regex declarations are generator counts; they are not a count of every runtime rule installed by the harness. The selected request size comes from the run settings, since a tier can list multiple supported sizes. Each workload declares 40 occurrences and 40 near misses per request.

| Case | Selected request bytes | Regex declarations | Dictionary entries | Seeded mapping pairs |
| --- | --- | --- | --- | --- |
| 000 | 65536 | 50 | 1000 | 10000 |
| 001 | 2097152 | 50 | 1000 | 10000 |
| 002 | 8388608 | 50 | 1000 | 10000 |
| 003 | 1048576 | 200 | 10000 | 10000 |
| 004 | 1048576 | 50 | 1000 | 100000 |

All five stress workload populations above are taken from retained reports; this table describes inputs, not collection completion or qualification.

These exploratory cases use four clients, 100 measured requests per arm per repetition, three paired repetitions, and at least 60 seconds plus 100 successful warmup requests per arm. They do not replace the 1,000-request normal latency qualification or the separate 600-second sustained-throughput measurements. The fixed-count throughput ratios below are descriptive only.

The 64 KiB case is collected: all 600 requests succeeded across six phases, each achieved four concurrent clients, and all resource samples are complete with passing resource checks. Twelve before/after environment snapshots match the eight reference fields; this does not establish continuous stability.

| Request size | Repetition | Added first-byte p95 ms | Added first-byte p99 ms | Added relay-flush p99 ms | Fixed-count throughput ratio | Proxy peak RSS bytes |
| --- | --- | --- | --- | --- | --- | --- |
| 64 KiB | 0 | 157.499 | 166.896 | 63.785 | 0.964880 | 51953664 |
| 64 KiB | 1 | 160.296 | 177.278 | 64.261 | 0.959131 | 53706752 |
| 64 KiB | 2 | 153.866 | 164.541 | 64.120 | 0.962909 | 54558720 |

Added first-byte p95 was 153.866–160.296 ms in this small-request exploratory case. Each repetition has 100 paired first-byte samples and 3,200 paired relay samples. These observations do not qualify the normal workload or erase its latency failures. The remaining stress cases and full mapping-churn run are still required.


#### 2 MiB stress case

All six phases retained 100 successful requests each (600 total), four-client concurrency, and the required warm-up. All six resource evaluations passed. Twelve before/after environment snapshots matched the eight reference host/toolchain/power fields; this is snapshot evidence, not continuous monitoring.

| Request size | Repetition | Added first-byte p95 ms | Added first-byte p99 ms | Added relay-flush p99 ms | Fixed-count throughput ratio | Proxy peak RSS bytes |
| --- | --- | --- | --- | --- | --- | --- |
| 2 MiB | 0 | 1768.893 | 1834.897 | 62.628 | 0.543768 | 319963136 |
| 2 MiB | 1 | 1787.864 | 1902.720 | 53.928 | 0.528326 | 313720832 |
| 2 MiB | 2 | 1751.324 | 1827.774 | 63.593 | 0.542993 | 318078976 |

Added first-byte p95 was 1751.324–1787.864 ms. These 100-request exploratory repetitions do not establish normal-workload qualification or replace the 600-second throughput measurements. The fixed-count throughput ratios are descriptive only. Full churn analysis and final review/integration remain outstanding.


#### 8 MiB stress case

All six phases retained 100 successful measured requests each (600 total), four-client concurrency and the required warm-up. The runner exited successfully, but all three proxy phases failed the unchanged 512 MiB RSS threshold. Twelve before/after environment snapshots matched the eight reference host/toolchain/power fields; this is snapshot evidence, not continuous monitoring.

| Request size | Repetition | Added first-byte p95 ms | Added first-byte p99 ms | Added relay-flush p99 ms | Fixed-count throughput ratio | Proxy peak RSS bytes |
| --- | --- | --- | --- | --- | --- | --- |
| 8 MiB | 0 | 6768.548 | 6857.845 | 64.479 | 0.241477 | 1066696704 |
| 8 MiB | 1 | 6991.930 | 7095.321 | 60.272 | 0.224316 | 1067696128 |
| 8 MiB | 2 | 7023.408 | 7121.430 | 68.897 | 0.231558 | 1062862848 |

These 100-request exploratory repetitions do not establish normal-workload qualification or replace the 600-second throughput measurements. Fixed-count throughput ratios are descriptive only.

First-repetition runtime diagnostics follow.


The first proxy repetition retained 100 successful measured requests, but peak process RSS was 1,066,696,704 bytes, above the unchanged 536,870,912-byte proxy threshold. This is an exploratory large-request stress result, separate from the normal workload matrix.

Across 200 post-warm-up resource samples, maximum heap-object bytes were 980,001,296 and maximum latest-GC live-heap bytes were 494,879,792. These maxima need not occur in the same sample. RSS, heap objects and latest-GC live heap measure different things; their differences do not establish a leak or a specific allocation cause.

Final stage summaries retained 229 samples, including warm-up: body-inspection p95 was 6764.729 ms, audit-write p95 10.612 ms, ingress-read p95 62.793 ms and metadata-inspection p95 0.106 ms. Body inspection is the broad adapter/bookkeeping stage, not a CPU profile or detection-only measurement. Stage percentiles are not additive and do not replace paired first-byte latency. The table above reports all three paired repetitions.

The second 8 MiB proxy repetition also exceeded the unchanged proxy-memory threshold: peak RSS was 1,067,696,128 bytes, with 100 successful measured requests. The third proxy repetition also failed this resource check, as shown above.


#### Rule-count stress case

The 1 MiB rules workload declares 200 regexes, 10,000 dictionary entries and 10,000 seeded mapping pairs. All six phases retained 100 successful measured requests each (600 total), four-client concurrency and the required warm-up. The runner exited successfully and all six resource evaluations passed. Twelve before/after environment snapshots matched the eight reference host/toolchain/power fields; this is snapshot evidence, not continuous monitoring.

| Request size | Repetition | Added first-byte p95 ms | Added first-byte p99 ms | Added relay-flush p99 ms | Fixed-count throughput ratio | Proxy peak RSS bytes |
| --- | --- | --- | --- | --- | --- | --- |
| 1 MiB | 0 | 2477.324 | 2515.428 | 61.098 | 0.449821 | 205094912 |
| 1 MiB | 1 | 2452.309 | 2456.668 | 59.621 | 0.431273 | 209715200 |
| 1 MiB | 2 | 2537.698 | 2802.668 | 61.213 | 0.427305 | 213991424 |

These 100-request exploratory repetitions do not establish normal-workload qualification or replace the 600-second throughput measurements. Fixed-count throughput ratios are descriptive only. Full churn analysis and final acceptance/review/integration remain outstanding.


#### Vault-population stress case

The 1 MiB vault workload declares 50 regexes, 1,000 dictionary entries and 100,000 seeded mapping pairs. All six phases retained 100 successful measured requests each (600 total), four-client concurrency and the required warm-up. The runner exited successfully and all six resource evaluations passed. Twelve before/after environment snapshots matched the eight reference host/toolchain/power fields; this is snapshot evidence, not continuous monitoring.

| Request size | Repetition | Added first-byte p95 ms | Added first-byte p99 ms | Added relay-flush p99 ms | Fixed-count throughput ratio | Proxy peak RSS bytes |
| --- | --- | --- | --- | --- | --- | --- |
| 1 MiB | 0 | 946.996 | 1010.101 | 59.903 | 0.662641 | 263798784 |
| 1 MiB | 1 | 879.716 | 895.612 | 50.824 | 0.684445 | 266158080 |
| 1 MiB | 2 | 907.119 | 916.163 | 60.630 | 0.696378 | 262488064 |

These 100-request exploratory repetitions do not establish normal-workload qualification or replace the 600-second throughput measurements. Fixed-count throughput ratios are descriptive only. All five stress cases are now collected, with 3,000 successful measured requests across 30 phases. All three 8 MiB proxy repetitions exceeded the unchanged 512 MiB RSS threshold; the other stress resource evaluations passed. Full churn analysis and final acceptance/review/integration remain outstanding.


### Text medians and completion latency

These additional per-repetition statistics complete the earlier text matrix. All times are milliseconds of **paired proxy-minus-bypass differences**, including negative scheduler jitter. Completion means the terminal response duration, not time to first byte. Relay uses corresponding decoded logical prefixes relative to upstream flush timing. First-byte p95/p99 and relay p99 are in the earlier text table. The source remains `f0c3f37`; these rows are not pooled with the later campaign and do not change any failed gate.

| Size KiB | Clients | Rep | Pairs | First-byte p50 | Completion p50 | Completion p95 | Completion p99 | Relay-flush p50 | Relay-flush p95 |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 256 | 4 | 0 | 1000 | 272.110 | 318.222 | 386.076 | 416.753 | 0.465 | 50.687 |
| 256 | 4 | 1 | 1000 | 265.864 | 134.679 | 286.023 | 336.032 | 0.463 | 47.598 |
| 256 | 4 | 2 | 1000 | 271.733 | 223.200 | 285.210 | 315.708 | 0.505 | 53.884 |
| 768 | 4 | 0 | 1000 | 652.218 | 605.865 | 665.846 | 694.757 | 0.473 | 54.133 |
| 768 | 4 | 1 | 1000 | 650.367 | 600.623 | 673.352 | 706.223 | 0.484 | 54.223 |
| 768 | 4 | 2 | 1000 | 647.943 | 596.919 | 665.570 | 694.649 | 0.404 | 53.482 |
| 1024 | 4 | 0 | 1000 | 863.559 | 815.404 | 886.384 | 921.625 | 0.519 | 53.442 |
| 1024 | 4 | 1 | 1000 | 862.069 | 823.106 | 888.046 | 920.783 | 0.521 | 53.277 |
| 1024 | 4 | 2 | 1000 | 862.130 | 821.454 | 888.389 | 922.121 | 0.546 | 53.189 |
| 256 | 1 | 0 | 1000 | 282.814 | 226.299 | 267.446 | 277.813 | 0.433 | 54.050 |
| 256 | 1 | 1 | 1000 | 282.181 | 224.600 | 265.697 | 281.852 | 0.423 | 53.843 |
| 256 | 1 | 2 | 1000 | 282.339 | 226.106 | 265.551 | 293.096 | 0.432 | 53.736 |
| 768 | 1 | 0 | 1000 | 646.911 | 590.190 | 631.182 | 645.738 | 0.455 | 53.895 |
| 768 | 1 | 1 | 1000 | 636.899 | 537.834 | 609.596 | 636.841 | 0.418 | 49.011 |
| 768 | 1 | 2 | 1000 | 643.507 | 598.276 | 726.989 | 770.845 | 0.443 | 54.237 |
| 1024 | 1 | 0 | 1000 | 840.521 | 756.563 | 840.356 | 865.300 | 0.430 | 49.944 |
| 1024 | 1 | 1 | 1000 | 841.529 | 796.265 | 940.007 | 969.310 | 0.436 | 49.165 |
| 1024 | 1 | 2 | 1000 | 841.235 | 762.377 | 820.254 | 858.255 | 0.434 | 50.755 |
| 256 | 8 | 0 | 1000 | 317.101 | 265.020 | 344.951 | 380.828 | 0.667 | 54.678 |
| 256 | 8 | 1 | 1000 | 315.868 | 262.321 | 357.636 | 401.879 | 0.686 | 54.227 |
| 256 | 8 | 2 | 1000 | 325.368 | 274.573 | 357.931 | 398.779 | 0.692 | 53.942 |
| 768 | 8 | 0 | 1000 | 874.315 | 829.471 | 1011.065 | 1063.849 | 0.642 | 48.862 |
| 768 | 8 | 1 | 1000 | 849.045 | 689.757 | 818.979 | 893.128 | 0.579 | 46.355 |
| 768 | 8 | 2 | 1000 | 847.402 | 819.477 | 935.492 | 984.884 | 0.700 | 54.609 |
| 1024 | 8 | 0 | 1000 | 1092.782 | 1128.171 | 1331.482 | 1394.647 | 0.511 | 52.227 |
| 1024 | 8 | 1 | 1000 | 1079.425 | 1017.994 | 1169.590 | 1228.869 | 0.582 | 53.940 |
| 1024 | 8 | 2 | 1000 | 1109.029 | 1042.091 | 1194.829 | 1264.146 | 0.654 | 54.354 |
| 256 | 16 | 0 | 1008 | 287.984 | 230.882 | 396.206 | 535.879 | 0.612 | 53.770 |
| 256 | 16 | 1 | 1008 | 300.598 | 343.889 | 493.929 | 686.807 | 0.693 | 49.240 |
| 256 | 16 | 2 | 1008 | 305.354 | 257.273 | 396.747 | 632.245 | 0.671 | 52.504 |
| 768 | 16 | 0 | 1008 | 784.600 | 630.989 | 1316.815 | 1750.849 | 0.325 | 49.286 |
| 768 | 16 | 1 | 1008 | 744.307 | 605.731 | 1207.631 | 1493.496 | 0.318 | 48.723 |
| 768 | 16 | 2 | 1008 | 684.124 | 524.631 | 1253.607 | 1769.630 | 0.229 | 47.930 |
| 1024 | 16 | 0 | 1008 | 1124.606 | 1166.845 | 2048.066 | 2419.310 | 0.453 | 58.190 |
| 1024 | 16 | 1 | 1008 | 1069.892 | 1007.389 | 1912.268 | 2250.944 | 0.482 | 57.773 |
| 1024 | 16 | 2 | 1008 | 1090.972 | 1033.304 | 1925.963 | 2202.659 | 0.462 | 58.202 |

### Complete latency distributions

The [companion aggregate CSV](2026-11-13-task04-latency-distributions.csv) reports per-arm p50, p95, and p99 first-byte and completion times for the throughput, extraction, mixed, overload, churn, stress, and supplemental measurements. Text-reference medians and completion times are tabulated above. Each CSV row identifies its source revision, report, phase, workload population, and sample count. Repetitions and revisions remain separate.

Quantiles use the nearest-rank definition: sort the finite observed millisecond values and select rank `ceil(p × n)`, with ranks starting at one. `duration_ms` is client-observed completion time. These are absolute per-arm distributions, not added proxy latency. Subtracting two percentile values would not produce the percentile of paired differences. The earlier paired comparisons retain their own validity and numerical gates; unequal duration-arm record sets are not truncated to manufacture a paired result.

Request distributions include all finite observations, including failed attempts when they supply a time. The CSV reports total records, successful records, missing metric samples, and the number of usable samples separately. Missing first-byte observations remain missing; they are not treated as zero latency. These request distributions include drained requests that finished after a measured window, while window-throughput counts elsewhere use only completions inside that window. Neither statistic silently replaces the other.

For mixed traffic, text and image request distributions are separate. Extraction run and wait distributions use the retained correlated stage samples; they are not percentile sums or substitutes for end-to-end completion time. An absent stage distribution is not proof of zero extraction overhead. Stage rows leave request-success and missing-request counts blank because a stage sample is a different population from a request record.

`client_unsent`, when available, is phase-level accounting repeated on metric rows, not a quantity to sum across CSV rows. For mixed phases it is the recorded unsent text count. Empty fields mean unavailable or inapplicable, not zero. All per-arm summaries remain descriptive: no new qualification decision or relaxed budget is introduced by this appendix.

RSS columns repeat phase-level observations. The budget state compares observed peaks with the unchanged 512 MiB proxy and 1 GiB combined-child limits; absent sampling stays incomplete. Sampling can miss short-lived child peaks. Bypass-arm proxy RSS measures the unused proxy process, not the backend. These resource observations are not allocation counts or proof of a memory leak.

The appendix contains 696 distinct metric rows from 113 retained reports. A separate calculation verified all available p50/p95/p99 values against the original observations; report hashes and metric sample counts were also checked.

### Mapping-churn evidence

All six arms were collected in the original alternating order: proxy0, bypass0, bypass1, proxy1, proxy2, bypass2. Each arm used the frozen `25a96b1` measurement implementation, 1 MiB requests, four clients, 100 scheduled new pairs/minute, a 600-second measured window, seed 1102026, at least 60 seconds/100 successful warm-up operations, and a fresh 10,000-pair file vault. All six attained four-client concurrency. Warm-up reused seeded mappings; only measured proxy requests introduced new pairs.

**The churn resource gate failed in all three proxy repetitions.** Each scheduled 1,000 operations but sent 800, all of which succeeded and had their new mappings independently verified and persisted. The remaining 200 were client-unsent, not proxy refusals and not successes. Each proxy reached 10,800 live and persisted mappings with zero unresolved indices. Each bypass sent and completed all 1,000 operations and left the seeded disk vault unchanged; its unused proxy's lazy in-memory vault remained empty. All child-RSS observations were zero. Peak budgets remain 536,870,912 bytes for the proxy and 1,073,741,824 bytes for children.

| Rep | Arm | Scheduled | Sent | Successful | Client-unsent | Persisted pairs | Peak proxy RSS bytes | Resource gate |
|---:|---|---:|---:|---:|---:|---:|---:|---|
| 0 | proxy | 1000 | 800 | 800 | 200 | 10800 | 573210624 | fail |
| 0 | bypass | 1000 | 1000 | 1000 | 0 | 10000 | 31326208 | pass |
| 1 | bypass | 1000 | 1000 | 1000 | 0 | 10000 | 31064064 | pass |
| 1 | proxy | 1000 | 800 | 800 | 200 | 10800 | 566083584 | fail |
| 2 | proxy | 1000 | 800 | 800 | 200 | 10800 | 569835520 | fail |
| 2 | bypass | 1000 | 1000 | 1000 | 0 | 10000 | 30883840 | pass |

The bypass RSS is the **unused proxy process**, not backend RSS. The successful-sent fraction is 100% for each arm, but proxy successful/scheduled is 80%; no unsent work is removed from offered-load accounting. The final vault inspection occurs after owned-proxy shutdown and is excluded from request latency. The complete measured start-time windows, count reconciliation, warm-up minima, concurrency, mapping populations, launcher/report hashes and retained environment fields were checked independently.

#### Churn timing and window accounting

These are per-arm client-observed milliseconds, not differences of percentiles and not a complete paired-latency claim: the proxy and bypass sent different operation sets. Successful completions inside the 600-second window are separated from successful drain completions after its end. This churn scenario does not substitute for the normal fixed-state ten-minute throughput gate.

| Rep | Arm | Sent samples | First-byte p50 | First-byte p95 | First-byte p99 | Completion p50 | Completion p95 | Completion p99 | In-window success | Drain success |
|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 0 | proxy | 800 | 1850.801 | 1869.678 | 1880.016 | 2699.901 | 2736.961 | 2752.532 | 796 | 4 |
| 0 | bypass | 1000 | 1018.266 | 1025.521 | 1030.644 | 1905.348 | 1978.630 | 1998.480 | 997 | 3 |
| 1 | bypass | 1000 | 1020.484 | 1025.184 | 1029.149 | 1929.220 | 1967.270 | 1980.721 | 997 | 3 |
| 1 | proxy | 800 | 1849.733 | 1868.549 | 1880.486 | 2710.635 | 2740.699 | 2757.725 | 797 | 3 |
| 2 | proxy | 800 | 1849.872 | 1868.523 | 1887.398 | 2709.587 | 2741.882 | 2757.209 | 797 | 3 |
| 2 | bypass | 1000 | 1020.875 | 1026.917 | 1030.344 | 1955.387 | 1986.046 | 1998.452 | 997 | 3 |

#### Observed churn memory change

RSS change is last minus first measured resource sample; heap-object and live-heap changes use final minus initial runtime snapshots. Dividing these observed deltas by verified new mappings is descriptive, **not an allocation cost or leak estimate**. RSS sample endpoints do not exactly coincide with admission/persistence boundaries, live heap is the latest GC observation, and no equivalent forced-GC comparison was performed. Negative changes remain visible. Peak whole-phase RSS includes warm-up and independently determines the failed resource gate.

| Rep | New persisted pairs | RSS delta bytes | RSS delta/pair | Heap-object delta bytes | Heap-object delta/pair | Live-heap delta bytes | Live-heap delta/pair |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 0 | 800 | 424738816 | 530923.5 | 354732216 | 443415.3 | 209244152 | 261555.2 |
| 1 | 800 | 421543936 | 526929.9 | 352815768 | 441019.7 | 208966816 | 261208.5 |
| 2 | 800 | 431128576 | 538910.7 | 332342008 | 415427.5 | 210401224 | 263001.5 |

#### Collection provenance and limitations

The original full CLI stopped at proxy repetition0 with exit1 after preserving its failed resource result. It was not restarted or relabeled successful. The five remaining arms were collected serially by independent invocations of the unchanged frozen `performance_churn.run_phase`, preserving the original order and settings. Proxy continuations also exited1 on the resource gate; bypass continuations exited0. Every owned process was checked absent before the next arm began. This continuation provides six-arm evidence but is **not a successful uninterrupted full-CLI execution**, and qualification remains false.

The executed [continuation launcher](2026-11-13-churn-continuation-launcher.py.txt) is archived byte-for-byte (SHA-256 `388405cc07523bc7978a7a978776fd8c64ec0b1045e5af9cf6eae77bf09919b7`). It is an execution record tied to the retained original report, source tree and private paths, not a new supported portable CLI. Original report and launcher hashes are retained in every continuation. Raw records, fixture values, logs and profiles remain private.

The original failed arm retained one environment snapshot, not an after-phase snapshot; that missing observation remains a qualification limitation. All five continuations retained before/after snapshots matching the eight fixed reference fields. These snapshots do not prove continuous machine quiescence. Lightweight local report/schema analysis occurred while some continuations ran; no competing benchmark, build or OCR job was launched. Reference-source separation, the failed gates and the original orchestration failure remain explicit.


## Supplemental capacity collection

The clean frozen source `9dd4fcdde6c867c9cd1a3abd5143b0052a19f74c` adds exploratory attachment and mixed-traffic coverage at the remaining client counts. These results supplement the reference matrix; they do not replace its sample, duration, or numerical acceptance requirements. The binaries were built with Go 1.26.11 without race instrumentation. The serial campaign checks source and executable hashes, fixed environment fields, phase accounting, and owned-process cleanup.

All six attachment-only invocations completed with exit status 0: three repetitions each at 8 and 16 clients, using template 0 and exactly 100 distinct measured images per invocation. Warmup requires both 60 seconds and 100 successful operations. The queue verified achieved concurrency and complete extraction run/wait timing coverage.

| Clients | Repetition | Measured records | Resource result |
| ---: | ---: | ---: | :--- |
| 8 | 0 | 100 | pass |
| 8 | 1 | 100 | pass |
| 8 | 2 | 100 | pass |
| 16 | 0 | 100 | pass |
| 16 | 1 | 100 | pass |
| 16 | 2 | 100 | pass |

Mixed-traffic collection is complete. It used 1 MiB text, the existing image selection, 120-second measurement windows, and three paired repetitions at 1, 8, and 16 clients. The one-client case cycles sequentially through one text and three image operations; it cannot establish concurrent fairness. These shortened windows are exploratory, and the report remains unqualified. The companion CSV retains separate per-repetition text/image first-byte and completion distributions, extraction-stage timings, sample counts and resource observations. All 18 mixed arms completed, retaining unequal proxy/bypass record counts rather than truncating them into a paired comparison.




### Supplemental mixed completions and memory

Each arm uses a 120-second measured window after both warmup minima. Counts below retain completions drained after that window; the CSV contains separate per-class latency distributions. Across all 18 arms, 150 requests completed during drain and no measured request started after its window ended. These counts are not paired latency samples or normal-tier qualification.

| Clients | Repetition | Arm | Text successes | Image successes | Proxy peak MiB | Child peak MiB |
| ---: | ---: | :--- | ---: | ---: | ---: | ---: |
| 1 | 0 | bypass | 16 | 46 | 33.33 | 0.00 |
| 1 | 0 | proxy | 12 | 33 | 135.42 | 40.34 |
| 1 | 1 | proxy | 12 | 33 | 134.92 | 44.12 |
| 1 | 1 | bypass | 16 | 45 | 32.53 | 0.00 |
| 1 | 2 | bypass | 16 | 46 | 34.05 | 0.00 |
| 1 | 2 | proxy | 12 | 35 | 135.88 | 40.42 |
| 8 | 0 | bypass | 64 | 441 | 34.42 | 0.00 |
| 8 | 0 | proxy | 44 | 308 | 333.27 | 89.91 |
| 8 | 1 | proxy | 44 | 308 | 327.72 | 87.30 |
| 8 | 1 | bypass | 60 | 418 | 33.89 | 0.00 |
| 8 | 2 | bypass | 65 | 449 | 34.41 | 0.00 |
| 8 | 2 | proxy | 44 | 308 | 321.44 | 90.67 |
| 16 | 0 | bypass | 61 | 894 | 34.03 | 0.00 |
| 16 | 0 | proxy | 43 | 664 | 444.78 | 90.28 |
| 16 | 1 | proxy | 42 | 645 | 445.45 | 89.77 |
| 16 | 1 | bypass | 60 | 886 | 34.03 | 0.00 |
| 16 | 2 | bypass | 64 | 950 | 33.52 | 0.00 |
| 16 | 2 | proxy | 43 | 661 | 449.11 | 89.08 |

Bypass proxy RSS is the unused proxy process, not backend memory. All 24 supplemental phases had observed resource samples and remained within the unchanged limits.

### Supplemental supervisor interruption

The initial supplemental supervisor stopped after eight completed jobs (ten measured phases). A tool-session interruption closed its progress-output pipe; the resulting `BrokenPipeError` occurred after the second one-client mixed repetition had saved both arms and verified process cleanup. The completed measurement commands all recorded exit status 0. The original stopped plan and traceback were archived privately.

Before continuation, all eight completed report hashes were independently checked, and no owned measurement processes remained. The continuation skips those completed jobs and runs only the seven pending jobs, retaining their original commands, source/build identity, environment checks, resource budgets and failure handling. Supervisor progress is redirected to a durable private file. This is an interrupted campaign with an explicit continuation, not an uninterrupted successful invocation. The continuation exited with status 0 after the remaining seven jobs completed. All 15 original report hashes were verified, including the unchanged eight reports produced before the interruption; no benchmark executables remained running.


## Final local verification

- Python discovery after the review correction: 258 tests, 4 opt-in skips, no failures or errors. Native extraction/mixed smoke and the real supplemental measurements are recorded separately above; skipped opt-in tests are not counted as executed.
- Go race checks: all 20 tested packages pass with Go 1.26.11.
- Lint: `go vet` and the full configured Staticcheck checks pass. The first invocation used an older default analyzer built for Go 1.23 and failed on the Go 1.26 toolchain. Repeating lint with the existing compatible Staticcheck 2026.2.2 binary passed; no checks or source code were changed to suppress that failure.
- Aggregate verification: 15 supplemental report hashes, 30 environment snapshots, 24 phase accounting/resource checks, 18 mixed-window boundary checks, and 2,088 quantiles across 696 appendix rows pass independent checks.
- The eight reports produced before the supplemental supervisor interruption retain their original hashes. The continuation exited with status 0 and no benchmark executables remained running.

The fresh whole-branch review identified one Important issue and no Critical or Minor findings. The correction and its regression evidence are recorded below. Required CI and merge status belong to the publishing PR. These development checks do not establish performance qualification.


### Review correction and measurement provenance

The review found that a count-based one-client mixed run could stop immediately after its last text request, omitting the final images; count-one warmup batches could therefore contain no images. Count mode now completes each text-plus-three-images cycle before successful completion. Failed operations still abort promptly and remain recorded, and duration mode keeps its existing time-window stopping behavior.

Three new regression methods first reproduced the defect, then passed after the minimal completion-condition change. The full Python suite passes, and an isolated native count-one check completed two arms with one text and three images each, warmup covering both classes, concurrency one, and complete proxy extraction-stage timing. These eight diagnostic measured operations are separate from the baseline corpus.

The 24 supplemental measurement phases remain attributed to frozen source `9dd4fcdde6c867c9cd1a3abd5143b0052a19f74c`, before this review correction. Their retained warmup satisfies both required minima and their measured mixed phases use duration windows. They have not been relabeled or rerun as evidence for the later runner. The unchanged Go source/build definitions retain the successful race and lint evidence.

Review scope decisions:

- Production optimization follows the measured baseline; the existing performance failures remain work to address.
- Soak, recovery, and live qualification belong to later roadmap work; this baseline does not establish those properties.
- Pre-existing identity-stride exhaustion in extremely long benchmark runs was not changed. All contracted measurements are below that boundary and their operation identities were checked; this is not an unbounded-run qualification.
- Historical campaigns remain preserved under their original source identities. Their results do not establish the performance of later runner revisions.

No Critical or Important review finding remains unresolved, and no Minor finding was deferred.
