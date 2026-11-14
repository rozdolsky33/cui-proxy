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
invocation, normal eight-CPU setting. Exact baseline source is f3a6197 and
candidate source eb17656. Both fresh builds had unchanged tracked source and
one identical, explicitly recorded benchmark overlay. Executable, tracked
build-source, benchmark-tree/overlay and complete fixture hashes were captured
before and after all 16 invocations; every identity remained unchanged.
The workload checks expected sanitization/restoration outside timing.

| Workload | Baseline time | Candidate time | Time change | Allocated-byte change |
|---|---:|---:|---:|---:|
| fixed-8 | 53.204 ms | 46.893 ms | -11.862% | +14.570% |
| rotating-8 | 52.875 ms | 45.674 ms | -13.618% | +12.105% |

Independent review found no runtime correctness defect but identified missing
contemporaneous identities in the original v3 microdiagnostic. Its numeric
results remain retained as an unattested diagnostic; they are not substituted
for the new table. The captured repeat above closes that evidence finding.
No runtime changes were made during the review fix pass.

Lowercasing adds temporary allocation; it does not retain request text or add a
cache. Resource and tail-latency effects still require real mixed-load evidence.
The private dictionary coverage diagnostic found32 of 40 large fields skippable
using15 bytes of necessary metadata; no input or dictionary text is exported.

The earlier32 KiB worker-threshold candidate regressed full-request latency
65.351% and was completely reverted. Regex-only folded prefilter variants
improved normal-CPU rotating latency 3.089% but were not accepted as a complete
fix. All intermediate evidence remains private and distinct from this result.

Semantic tests establish required-substring behavior and matching/blocking
parity, including Unicode folding, optional branches, non-ASCII and invalid
UTF-8 fallbacks. Initial necessary-condition fuzzing completed 3,302,192 executions.
All 23 Go packages pass race tests; vet and staticcheck pass. End-to-end
blocking/scan parity fuzzing adds 2,211,788 executions. Combined targeted fuzz
runs total 5,513,980 executions. These establish tested semantic preservation,
not exhaustive correctness for arbitrary policies.

## Short mixed-load diagnostic

A proxy-first 768 KiB pair used the unchanged four-client real-OCR fixtures
and backend pacing. Candidate outcomes were 80/80 text and 216/216 images;
control outcomes were 80/80 text and 237/237 images. Both arms had zero
unexpected failures. Added text first-byte p50/p95/p99 was
124.460/149.086/162.356 ms; matched logical-prefix relay p99 was 63.126 ms.
Candidate body inspection p95 was 72.127 ms and peak proxy RSS 64.234 MiB.

This diagnostic used only 80 measured text requests and a short warmup
(candidate 12 completions in 7.109 seconds), so the evaluator correctly marks
latency/relay evidence incomplete. The p95 observation is marginal against the
150 ms threshold and must be tested with the full warmup and 1,000 samples.
Do not treat it as a completed fairness gate, pool it with the earlier full
failed result, or infer that all later sizes/repetitions will pass.

Private evidence: cui-task11-folded-attested-summary.json, exact-source build
information and captured invocation logs; the original
cui-task11-folded-v3-summary.json remains separately retained; cui-task11-folded-mixed-768.json with provenance/evaluation companions;
cui-task11-folded-validation-status.json and captured gate logs. The code
state used by the mixed diagnostic is the runtime source committed in 08caf28;
its pre-commit dirty build remains explicitly labeled diagnostic-only.
