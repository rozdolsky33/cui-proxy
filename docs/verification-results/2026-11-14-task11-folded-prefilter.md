# Task 11 folded prefilter experiment

This experiment addresses the failed full fairness result recorded separately.
It does not qualify four-agent latency, throughput, fairness or soak behavior.
All results use fabricated fixtures; raw captures remain outside git.

The candidate derives bounded necessary ASCII-folded substrings for owned
regex and dictionary detectors. It prepares lowercase text once per large
ASCII field and skips only provably impossible matches. Non-ASCII/invalid
UTF-8 input and small fields retain existing matching. Original regex and
whole-entry dictionary behavior supplies every actual match.

## Full-request diagnostic

Eight alternating baseline/candidate pairs, 32 full requests per workload per
invocation, normal eight-CPU setting, same private changing fixture and
benchmark source. Baseline is the qualified retention-fix build from PR48;
candidate is explicitly an experimental dirty build with frozen binary.
The workload checks expected sanitization/restoration outside timing.

| Workload | Baseline time | Candidate time | Time change | Allocated-byte change |
|---|---:|---:|---:|---:|
| fixed-8 | 53.258 ms | 47.059 ms | -11.639% | +13.662% |
| rotating-8 | 53.354 ms | 45.954 ms | -13.869% | +13.637% |

Lowercasing adds temporary allocation; it does not retain request text or add a
cache. Resource and tail-latency effects still require real mixed-load evidence.
The private dictionary coverage diagnostic found32of40large fields skippable
using15bytes of necessary metadata; no input or dictionary text is exported.

The earlier32KiB worker-threshold candidate regressed full-request latency
65.351% and was completely reverted. Regex-only folded prefilter variants
improved normal-CPU rotating latency3.089% but were not accepted as a complete
fix. All intermediate evidence remains private and distinct from this result.

Semantic tests establish required-substring behavior and matching/blocking
parity, including Unicode folding, optional branches, non-ASCII and invalid
UTF-8 fallbacks. Initial necessary-condition fuzzing completed3302192executions.
Full verification and mixed-load diagnostics are pending; no release claim.
