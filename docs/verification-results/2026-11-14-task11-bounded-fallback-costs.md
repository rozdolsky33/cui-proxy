# Production bounded-scan fallback costs

Diagnostic microbenchmarks, not full runtime qualification. Exact source `5d7112e`, Go 1.26.11. Original regexp and production boundedMatches ran in the same binary: three 300 ms repetitions for each of three detectors and three input shapes, 54 samples total. The temporary benchmark overlay was removed after completion.

| Detector/input | Original median ms | Candidate median ms | Change |
| --- | ---: | ---: | ---: |
| addrport/dense | 39.272464 | 39.766500 | 1.258% |
| addrport/late_unicode | 13.048330 | 13.625672 | 4.425% |
| addrport/sparse | 13.036332 | 0.520331 | -96.009% |
| ipv4/dense | 39.360885 | 39.466333 | 0.268% |
| ipv4/late_unicode | 13.208904 | 13.525727 | 2.399% |
| ipv4/sparse | 13.065897 | 0.523482 | -95.994% |
| ssn/dense | 27.314561 | 27.578858 | 0.968% |
| ssn/late_unicode | 13.037144 | 13.539643 | 3.854% |
| ssn/sparse | 13.264026 | 0.537167 | -95.950% |

Dense fallback adds 0.105–0.494 ms in these cases; a non-ASCII byte near the end adds 0.317–0.577 ms. The latter requires an ASCII scan before returning to the original engine. This cost is retained explicitly, not described as zero regression. Sparse scans improve approximately 96%; the separate canonical complete-processing benchmark improves 13.8–15.0%.

Decision: proceed to the original full mixed qualification, preserving its unchanged latency and resource gates. These local fallback costs are bounded tradeoffs, not a new or weakened qualification threshold. Wider runtime and stress evidence remains required.

Private report `/private/tmp/cui-bounded-fallback-benchmark.json` SHA-256 `0438cfb368040aad0175b0e42efb7130dc5ec443b8aa797f22347eac8deef1b1`. Harness `/private/tmp/cui-bounded-fallback-benchmark_test.go`; captured output `/private/tmp/cui-bounded-fallback-benchmark.log`.
