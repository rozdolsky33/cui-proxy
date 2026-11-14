# Task 11 full fairness result: not qualified

The 768 KiB, four-client pair completed with the required warmup and 1000
text requests per arm. Candidate source ca6dda7164d9ec4bec1e09de083e98a46b0eb744
is the retention fix integrated by PR48. The candidate arm ran before a fresh
matched control. The older control was rejected because its environment record
did not establish the quiet-machine precondition. Its data remain diagnostic.

| Metric | Observed | Gate |
|---|---:|---:|
| Added text first byte p95 | 156.154 ms | <=150 ms: failed |
| Added text first byte p99 | 188.750 ms | <=500 ms |
| Added logical-prefix relay p99 | 61.102 ms | <=100 ms |
| Candidate supported text | 1000/1000 | zero unexpected failures |
| Candidate supported images | 2669/2669 | zero unexpected failures |
| Control supported text/images | 1000/1000; 2949/2949 | zero unexpected failures |
| Image extraction p95 | 237.452 ms | <=5000 ms |
| Image queue p95 | 116.226 ms | <=2000 ms |
| Peak proxy RSS | 70.766 MiB | <=512 MiB |
| Peak extraction child RSS | 152.375 MiB | <=1024 MiB |

All request-stage coverage was available, with zero stage polling errors.
The candidate and control warmups completed 110 and 126 operations in 62.026
and 61.564 seconds respectively. Individual matched differences retain negative
jitter. The overall image/fairness result is not qualified because simultaneous
text latency failed. Remaining repetitions were automatically stopped.

Text body inspection p95 was 88.562 ms, ingress 6.223 ms and audit 6.695 ms.
Admission p95 was 0.018 ms and mapping checkpoint 0.002 ms. These spans are
not additive percentile estimates. No throughput, lifecycle, soak or complete
four-agent release qualification is claimed. Investigate the measured latency
failure before resuming the remaining eight fairness pairs.

Private evidence: cui-task11-retention-qualified-768-1-fresh.json and its
provenance/evaluation companions. Raw reports and fixtures remain outside git.
