# Task 11 JSON output allocation experiment

Status: focused performance and local regression checks passed; independent review and integration pending.

## Why this path

The normal 768KiB changing-request profile measured about 7.8MiB allocated per request. Buffer growth accounted for 41.53% of sampled allocated space. CPU samples were dominated by runtime frames, so this investigation uses allocation evidence rather than claiming a CPU root cause. Fixed and rotating body results did not establish a persistent sanitizer cache.

The candidate retains Go's standard JSON encoder with HTML escaping disabled. Direct writes remove intermediate buffers on small and unfragmented outputs. Eligible larger rewritten containers collect at most 4095 actual encoded fragments, allocate an exact owned result, and copy those fragments. A bounded tree count avoids scanning string contents. Unchanged nodes return an owned raw copy. No persistent caches, pools, unsafe aliases or policy changes are introduced.

## Rejected revisions retained

- Direct string output alone: eight alternating equal-work pairs reduced changing-request allocated bytes 9.797%, below the stated 10% threshold; latency changed +0.192%. The fixed-body allocation result was better, but did not replace the changing-body gate.
- Exact character counting: the first control run regressed normal encoding approximately 84%; a faster counter still regressed Unicode-heavy encoding approximately 89%. Both were rejected before expensive live campaigns.
- Initial fragment planning: eight control pairs found an extra allocation and 10.59% latency regression on unchanged JSON. A failing single-allocation regression test reproduced the issue; a raw-copy path fixed it. These earlier measurements remain in private evidence.

## Correctness evidence so far

Adapter, transformation and proxy race tests pass. Fragment differential fuzzing completed 1,116,753 executions without mismatch. Tests compare standard-encoded output, retain duplicate keys and numeric spellings, cover canonical and raw-node behavior, check output ownership, bound metadata counting and integer overflow, and verify that discarded large image data does not determine output allocation. Disabling fragment planning makes two targeted capacity tests fail. That mutation check is sensitivity evidence; the fragment implementation preceded those additional tests.

## Final encoding controls

Eight alternating pairs, 100 iterations per case, sequential execution. Median latency changes: small -20.59%, normal -9.68%, large -6.43%, control-character expansion -11.82%, invalid UTF-8 expansion -10.12%, Unicode expansion -7.44%, shrink -23.35%, unchanged raw -8.57%. Normal encoding allocated bytes decreased 52.30%; unchanged raw allocation stayed equal. These are encoder controls, not whole-request performance results.

Private evidence prefix: `/private/tmp/cui-task11-json-v4-final-`. Raw logs and fixtures stay local. Full-request evidence is below; live mixed-load evidence is below.

## Full-request allocation preflight

Eight alternating reference/candidate pairs, each with 32 equal-work iterations and the same 32 distinct 768KiB bodies. Rotating-request allocated bytes decreased **22.783%**, with median latency **0.438% lower**. Fixed-request allocated bytes decreased 23.171%, with latency 0.803% lower. Detection and cold/warm restoration controls did not regress materially. This passes the experiment's allocation preflight without changing its threshold.

## Short mixed-load diagnostics

Three alternating paired reference/candidate runs, each with four clients, 20 measured text requests per arm, 768KiB bodies, four warmup requests and real image extraction. All pairs passed the predeclared no-material-regression check. Reference runs completed 463 successful operations; candidate runs completed 462. Every run had zero failures, valid paired latency/relay samples and exact body/checkpoint stage coverage for all proxy text and image requests. The image stream runs alongside a fixed count of text requests, so its completed count can vary by one; this is not an equal-duration throughput qualification.

| Pair | Reference added first-byte p95 | Candidate added first-byte p95 | Completion p95 change | Peak proxy RSS change |
| --- | ---: | ---: | ---: | ---: |
| 0 | 149.486ms | 137.800ms | -32.20% | +4.28% |
| 1 | 157.987ms | 139.494ms | +9.72% | +0.60% |
| 2 | 153.688ms | 151.693ms | -42.88% | +1.30% |

Pair 1 completion latency is close to the unchanged 10% regression limit; it remains visible rather than being averaged away. Child RSS comparisons were nonregressing where the reference sampler observed a positive peak; zero child samples do not establish zero child memory. The final candidate's 151.693ms first-byte p95 exceeds the original 150ms target. These 20-text-sample diagnostics therefore support retaining an allocation improvement, **not closing the original latency gate**.

Private mixed-load evidence: `/private/tmp/cui-task11-json-v4-mixed-{reference,candidate}-{0,1,2}.json` and their numeric summary. Every outcome and resource/stage sample is retained locally.

## Remaining gates

Local gates passed: 23 Go race-test packages, 298 Python workflow tests (four opt-in skips), go vet, staticcheck with all checks, the HTTP regression corpus and manual build. Independent review and exact-head CI remain required before integration.

This experiment cannot close Task 08 or Task 11's original latency, throughput, cancellation, lifecycle or four-hour soak criteria. Short diagnostic percentiles are not qualification evidence.
