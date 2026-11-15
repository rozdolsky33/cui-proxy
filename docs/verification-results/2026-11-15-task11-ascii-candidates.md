# Task 11: exact ASCII candidate scanner verification

The fixed built-in candidate scanners preserve the original regex candidate indices and improve the measured diagnostic workload. Full performance qualification remains open. The preceding frozen runtime failed its third 1 MiB fairness repetition at 153.052 ms added first-byte p95 against the unchanged 150 ms limit; that evidence remains retained.

## Change and correctness

Only the fixed IPv6/address-port and card/card-run candidate languages use the new stateless byte scanners. Their existing finders, validators, classifications, blocking tags and overlap resolution remain in place. Custom regex rules retain the original engine path. No request data is cached.

Differential tests compare exact greedy, nonoverlapping index pairs to Go regexp, including empty/short inputs, long runs, separator boundaries, Unicode and invalid UTF-8. Exhaustive enumeration covers 111,111 short strings. Integration tests compare complete built-in scans and blocking scans against copies using the original matcher. The full Go race suite, vet and all staticcheck checks passed. A 30-second, four-worker differential fuzz run completed 3,128,153 executions without a mismatch. Detailed test output remains private.

## Alternating request benchmark

Three alternating baseline/candidate measurements used the existing 768 KiB, 40-field synthetic request fixture. Median request-walk time fell from 41.228 ms to 28.838 ms, approximately 30%. This is a microbenchmark, not end-to-end qualification. The private numeric summary has SHA-256 `c8256b94477f0c89d8844cd6875bf1849e1e4e05828236c41a2873c45fc69792`.

Earlier diagnostic experiments with larger parallel thresholds, dictionary-tail checks, alternative-literal prefilters and shared candidate results were not adopted. They did not show a sufficient improvement; their private artifacts remain retained.

## Short mixed diagnostic

The optimized candidate was built from clean source `4e5251ef103b79f93942e93a70a5c8417517764b` using Go 1.26.11 with `-trimpath -buildvcs=false`. Executable SHA-256: `173a0ddb35e08b94c81cb19d183a00b7c5e57afb9662334c424507cac7927b3d`. The frozen harness source was `8ea39b07774a51f1184e25d3e0ee1d7c57f70b42`.

A single bypass-first/proxy-second diagnostic used the 1 MiB workload, four clients and 80 measured text requests per arm. Diagnostic warmup requested 5 seconds and 10 completions, rather than the full reference minima. Both arms completed with zero unexpected failures: 80/80 proxy text requests, 80/80 bypass text requests and 216/216 proxy image requests succeeded. Request-stage coverage was complete with zero stage-poll errors.

| Observation | Result |
| --- | --- |
| Added text first-byte p95 / p99 | 118.386 ms / 125.689 ms |
| Matched relay p99 | 54.724 ms across 2,560 samples |
| Image extraction p95 | 227.568 ms |
| Extraction queue p95 | 109.059 ms |
| Maximum measured extraction budget | 327.841 ms |
| Peak proxy / child RSS | 66,781,184 / 142,639,104 bytes |

The unmodified full-gate evaluator returned exit 2 and `passed: false`: the diagnostic does not meet the required 1,000 paired text samples or full warmup. That result is expected and retained. The observed latency supports proceeding to full qualification; it does not replace any full-run criterion or qualify the operating envelope.

Private artifacts:

- `/private/tmp/cui-task11-ascii-mixed-1048576-1.json`, SHA-256 `f6ebc9a59ca26cd25a08820303aa615ad2dc2c48bff179451d1c7c9cd1afc16c`.
- `/private/tmp/cui-task11-ascii-mixed-1048576-1.evaluation.json`, SHA-256 `ed12e54c5868e677602f83c1054ac775169470d935ef93c3cc869af5f407b05c`.

Next gates are final independent review, required CI and integration, then a new immutable runtime and the unchanged complete reference campaign. Earlier runtime measurements cannot be relabeled as evidence for this binary.
