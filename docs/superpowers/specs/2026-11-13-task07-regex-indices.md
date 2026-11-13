# Task 07: conservative regexp prefilter

Task04 optimized CPU profiles identify regexp matching as the dominant measured
cost. Derive one necessary ASCII literal from the compiled regex syntax and skip
non-group matching only when that literal is absent. Otherwise use the unchanged
regexp, refiners and finders. Optional branches, Unicode and case-folding cases
without a provable literal fall back to the reference path. Keep groupMatches
unchanged for overlapping secrets. Exclude regexps with an existing literal
prefix or a start-of-text search condition to avoid redundant or slower scans.

Expected improvement: avoid expensive regex scans for impossible candidates.
The literal is at most 64 bytes per immutable detector; it contains configuration
metadata, not request data. There is no body cache or mapping-lifetime dependency.
Policy reload reconstructs detectors and their derived literals. No audit change.

Acceptance: exact span and policy equality across deterministic and fuzz inputs;
normal/stress benchmark improvement above paired run-to-run noise, no material
regression in supported workloads, and semantic/policy gates pass. If evidence
does not support this optimization, do not claim or ship a performance win.

Compare reference and candidate in alternating paired runs on the same host.
Report sample counts, distributions, workload bounds and limitations. Use the
existing deterministic HTTP harness and representative normal/stress sizes,
including concurrent clients. Keep raw fixtures/reports captured privately.

## Paired measurement protocol

Eight repetitions, alternating arm order, with identical per-pair workload seeds
and build flags. Detector microbenchmarks include clean, mixed and dense 64 KiB
inputs plus 2 MiB and 8 MiB stress inputs, and existing-prefix/anchored controls. HTTP cases are normal 256 KiB/four
clients, 2 MiB/four clients, 8 MiB/one client, 200-rule 1 MiB/four clients and
100,000-mapping 1 MiB/four clients. Each client performs one warmup; normal uses
two measured requests per client and stress cases one. All attempts and failures
are retained; no live model calls or competing benchmark jobs are used.

Compare paired phase medians, with seeded bootstrap intervals for the geometric
mean latency ratio. Improvement is evidence-backed only when its 95% interval
is below 1.0. A greater-than-10% latency or memory regression is material; report
uncertain outcomes rather than hiding them. Stress cases with one client have
one measured request per phase, so phase tail quantiles are not a separate tail
qualification. These checks do not replace Task11 sustained acceptance.
