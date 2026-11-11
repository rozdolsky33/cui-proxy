# Concurrent text diagnostics — Task 04 increment

**Status:** Diagnostic evidence only. Task 04 and the four-agent milestone remain incomplete.

## What ran

- Development matrix: 12 cells (three normal sizes × 1/4/8/16 clients), three repetitions each, 72 proxy/bypass phases, 1,044 measured requests, zero failures, 36 valid paired comparisons. Source changed during development, so only outcome/coverage counts are summarized from this matrix; its latencies are not pooled or used for qualification.
- Fixed-revision diagnostic: 768 KiB, four clients, three repetitions, five measured operations per client per phase; 120 total requests, zero failures and three complete paired comparisons.
- Fixed source: `ba9c7406f15052aab895993a71c3851df79707d7`; clean working tree at measurement. Build SHA-256: `2646247792e55835005634d6c011d699e6bacf89254b52bd43ad2e29982eec40`.
- Population verified in every phase: 64 compiled detectors (builtin set plus 50 regex declarations and one dictionary), 1,000 dictionary entries and 10,000 file-vault mappings. Audit and sanitization remained fail-closed.
- Fake backend: 1,000 ms wait after request read, 32 delta events at 20 ms spacing, two protected values split across events. The client checked exact sanitized-body digests and complete restored output.

## Fixed-revision paired measurements

All numbers below are milliseconds. Each repetition has only 20 pairs; these tail quantiles are diagnostic and do not meet the required 1,000 samples per size/concurrency/repetition. Negative deltas remain as observed jitter.

| Repetition | Added first byte p50 | p95 | p99 | Added logical-prefix p99 | Added completion p95 |
|---|---:|---:|---:|---:|---:|
| 1 | 626.03 | 664.69 | 667.57 | 659.19 | 664.38 |
| 2 | 632.53 | 672.27 | 684.10 | 674.29 | 671.78 |
| 3 | 624.04 | 641.27 | 679.82 | 662.76 | 669.86 |

These are paired individual-sample differences, not differences of percentiles. The logical-prefix comparison tracks decoded output availability rather than assuming wire-write boundaries survive restoration. No rate or latency threshold is declared passed by this short run.

## Resource and stage observations

| Repetition (proxy) | Peak sampled RSS (MiB) | Peak sampled child RSS (MiB) | Observed client concurrency |
|---|---:|---:|---:|
| 1 | 66.70 | 0.00 | 4 |
| 2 | 65.94 | 0.00 | 4 |
| 3 | 68.39 | 0.00 | 4 |

Sampling occurs once per second and can miss short peaks. The Go runtime counters refer to the whole proxy process; live heap is from the latest GC and is accompanied by a GC-cycle count. No forced GC or production debug endpoint was used. Stage snapshots include setup/warm-up and are bounded rolling observations.

## Component profile smoke

One three-iteration run used the normal 256 KiB fixture and 10,000 mappings. CPU, heap/allocation, mutex and block profiles were captured locally. The following different component workloads are attribution probes, not additive parts of one request.

| Component | ms/op | bytes/op | allocations/op |
|---|---:|---:|---:|
| `BenchmarkDetection-8` | 191.970 | 866453 | 6214 |
| `BenchmarkRequestWalk-8` | 184.942 | 2495509 | 8896 |
| `BenchmarkRestoration/cold=false-8` | 0.975 | 264176 | 8 |
| `BenchmarkRestoration/cold=true-8` | 1.762 | 1695224 | 68 |
| `BenchmarkVaultSave-8` | 16.247 | 3229149 | 53 |
| `BenchmarkMappingChurn-8` | 1.055 | 1432520 | 84 |

The full-string detection probe was the slowest of this small set. Request walking scans separate leaves, so comparing those two timings does not establish a precise end-to-end cost fraction. Detector scaling and body-inspection profiles are candidates for deeper investigation; three iterations are insufficient to select an optimization conclusively.

## Remaining work

- Full contract warm-up/sample counts and equal ten-minute mixed-size throughput windows.
- Verified quiet reference hardware/power conditions and repeated normal/stress baseline runs.
- Real PNG/JPEG/PDF generation and extraction, cold/repeated images, image/text fairness and extractor cancellation.
- Open-loop overload, end-to-end mapping churn, sustained resource stability and evidence-based bottleneck selection.
- Record min/max across fully qualified repetitions; preserve the existing thresholds.

Detailed reports, payloads, vault material and profiles remain local. This document contains only aggregate metadata. The roadmap remains in progress.
