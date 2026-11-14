# Bounded scan canonical processing comparison

Status: microbenchmark evidence only; full mixed-workload acceptance remains open.

Baseline `9eea84d`, candidate `5d7112e`, frozen isolated worktrees, Go 1.26.11. Identical pre-existing canonical request-sequence benchmark and fixture were used for both builds. Eight alternating repetitions, 32 iterations per benchmark, fixed and rotating request variants. All 32 samples completed; source, fixture and binary identity checks remained unchanged.

| Variant | Baseline median ms | Candidate median ms | Time change | Allocated bytes change |
| --- | ---: | ---: | ---: | ---: |
| fixed-8 | 47.177408 | 40.657992 | -13.819% | -0.693% |
| rotating-8 | 45.912862 | 39.022421 | -15.008% | -1.191% |

This measures request-processing CPU/allocation effects, not HTTP latency, extraction queueing, fairness, throughput or soak stability. The previous full mixed pair failure remains retained and unqualified. The next diagnostic is the same four-client, 768 KiB mixed text/image probe used for previous candidates; a short probe cannot replace the full original matrix.

Private captured evidence:
- `/private/tmp/cui-task11-bounded-attested.py` SHA-256 `ee1d28f21ffa521145ada676636657f969ef6a9c387be06a84b660e6c8d31451`.
- `/private/tmp/cui-task11-bounded-attested-summary.json` SHA-256 `0293a5471d4949b6f512d782a0bff9bc020f80b11c2b9458ab69c79fbd9699bd`.
- `/private/tmp/cui-task11-bounded-attested-controller.log` SHA-256 `0f4a421cc5c226a05ec053013057d20cb2a8af684f5a29e775770a4cd296b2c4`.
