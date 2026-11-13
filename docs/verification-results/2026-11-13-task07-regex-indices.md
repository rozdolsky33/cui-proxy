# Task 07: conservative regexp prefilter results

Measured on 2026-11-13. Base: `11f4c99973fbcfc1f4853fb83f50b5c72c3c7972`; implementation: `f0156c8`.

## Change and invariants

Task 04 identified regexp matching as the dominant detector CPU cost. Each compiled non-group detector may now retain a necessary ASCII substring (at most 64 bytes). An absent substring safely rejects an impossible match; otherwise the original regexp matching path runs unchanged. This is immutable rule metadata, not a request cache. Policy reload reconstructs it. Group overlap, refiners, finders, blocking tags, resolution, audit ordering and fail-closed behavior are unchanged. Existing literal-prefix and start-of-text search strategies are excluded to avoid adding unnecessary scans.

## Fixed comparison method

Eight paired repetitions per case, alternating reference/candidate order, with identical seeds and workloads within each pair. Both proxies used Go 1.26.11 and `-trimpath -buildvcs=false`. Each client completed one warmup; normal requests then ran twice per client and other cases once. No competing builds or tests ran during the campaign. All 80 phases achieved the requested concurrency, and all 336 measured requests completed as supported HTTP 200 successes. Every before/after environment snapshot reported AC power and low-power mode off.

Ratios use the geometric mean of eight paired phase-median ratios. Intervals are percentile bootstrap 95% intervals from 10,000 paired resamples with seed 20261113. Improvement requires an interval upper bound below 1; a latency or memory regression over 10% is material. These thresholds and sample count were set before the final campaign. No samples were trimmed.

| Workload | Clients | Reference median ms | Candidate median ms | Latency reduction | Ratio 95% interval | Peak RSS ratio (95% interval) |
|---|---:|---:|---:|---:|---|---|
| normal256KiB_c4 | 4 | 2033.5 | 1888.7 | 7.2% | 0.924–0.931 | 1.015 (0.988–1.046) |
| rules1MiB_c4 | 4 | 4177.9 | 2044.0 | 50.9% | 0.487–0.497 | 1.004 (0.939–1.054) |
| stress2MiB_c4 | 4 | 3473.6 | 2258.5 | 35.0% | 0.647–0.653 | 0.998 (0.960–1.043) |
| stress8MiB_c1 | 1 | 8124.9 | 3440.3 | 57.8% | 0.420–0.425 | 0.979 (0.932–1.021) |
| vault1MiB_c4 | 4 | 2639.5 | 2031.1 | 22.3% | 0.763–0.794 | 0.988 (0.960–1.017) |

Reported millisecond columns are medians of the phase medians; they are descriptive and their quotient need not equal the paired geometric ratio. Normal/large/vault cases use 50 configured regexes; the rules case uses 200. Mapping populations are 10,000 except the vault case (100,000).

## Detector controls

| Case | Runtime reduction | Candidate/reference 95% interval |
|---|---:|---|
| Clean 64 KiB | 41.9% | 0.551–0.600 |
| Mixed 64 KiB | 21.5% | 0.779–0.793 |
| Dense 64 KiB | 4.3% | 0.948–0.968 |
| Mixed 2 MiB | 20.3% | 0.791–0.803 |
| Mixed 8 MiB | 20.6% | 0.790–0.800 |
| Existing prefix hit | 0.0% | 0.993–1.007 |
| Anchored miss | −1.0% | 1.007–1.013 |

Detector controls used eight alternating pairs per case, 100 ms benchmark duration and GOMAXPROCS=4. The approximately 1% anchored-control overhead is below the material-regression threshold.

## Rejected experiments

An initial capture-index API substitution failed its allocation contract both before and after the change and was discarded. Go still collected captures internally; builtin non-group patterns had no captures to remove.

An initial prefilter variant did not exclude existing prefix and anchored search strategies. Targeted controls exposed 2.27× prefix-hit and 754.66× anchored-miss regressions. Its HTTP campaign was interrupted and preserved separately. A regression test was observed failing, the eligibility rule was corrected, and every final measurement above was rerun on the corrected binary. Interrupted/initial measurements are not mixed into these results.

## Verification and limits

Corrected differential fuzzing passed 74,510 full-scan executions and 3,140,485 arbitrary-pattern executions. Tests compare exact spans and blocking behavior to the original implementation, including Unicode, invalid UTF-8, empty matches, alternatives, captures and existing search strategies.

Local gates passed: Go race tests across 22 packages; 266 Python tests with four opt-in skips; go vet; staticcheck 2026.2.2 with all checks; and the HTTP semantic/policy harness. Fresh branch review and required PR checks remain integration gates.

This is a bounded single-machine comparison, not a sustained capacity or universal custom-pattern guarantee. In particular the 8 MiB case has one measured request per phase; its tail statistics cannot qualify sustained p95/p99 performance. Task 04 remains unqualified against the broader operational targets; Task 11 still owns sustained four-agent acceptance. Attachments and live-model quality are not newly qualified here. Raw fixtures, requests and detailed reports remain private.

Acceptance: the measured processing improvement exceeds observed run-to-run noise in every HTTP case; all peak-memory interval upper bounds remain below 1.10. Final integration also requires the semantic/policy corpus, race tests, static checks, review and CI.

## Descriptive request tails

Pooled request percentiles below use nearest rank. Requests in one phase share a process and are not independent repetitions; these figures are descriptive only.

| Case | Arm | Requests | p95 ms | p99 ms | Max ms |
|---|---|---:|---:|---:|---:|
| normal256KiB_c4 | candidate | 64 | 1920.9 | 1937.0 | 1937.0 |
| normal256KiB_c4 | reference | 64 | 2085.5 | 2102.0 | 2102.0 |
| rules1MiB_c4 | candidate | 32 | 2242.8 | 2262.7 | 2262.7 |
| rules1MiB_c4 | reference | 32 | 4398.3 | 4408.7 | 4408.7 |
| stress2MiB_c4 | candidate | 32 | 2309.3 | 2310.0 | 2310.0 |
| stress2MiB_c4 | reference | 32 | 3548.4 | 3549.9 | 3549.9 |
| stress8MiB_c1 | candidate | 8 | 3468.1 | 3468.1 | 3468.1 |
| stress8MiB_c1 | reference | 8 | 8187.2 | 8187.2 | 8187.2 |
| vault1MiB_c4 | candidate | 32 | 2200.3 | 2228.9 | 2228.9 |
| vault1MiB_c4 | reference | 32 | 2685.4 | 2689.6 | 2689.6 |

## Reproduction metadata

- os: `Darwin 25.6.0`
- architecture: `arm64`
- hardware_model: `MacBookPro18,3`
- logical_cpus: `8`
- memory_bytes: `34359738368`
- go_version: `go1.26.11`

The existing `performance.run_phase` was invoked with proxy mode, seed `20261113 + repetition`, no duration window, zero timed warmup and one warmup request per client. Recipes were normal/262144/4/2, large/2097152/4/1, large/8388608/1/1, rules/1048576/4/1 and vault/1048576/4/1 (tier/bytes/clients/count). The private campaign manifest pins SHA-256 hashes of both proxies, seed helper and runner.
