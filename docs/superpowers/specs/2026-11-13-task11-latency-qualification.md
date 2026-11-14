# Task 11 full-sample latency qualification

Continue the authorized roadmap after PR47, preserving every original workload-contract threshold. The allocation optimization passes its focused experiment but the short candidate p95 of151.693ms does not establish the150ms latency gate. Before investing in a four-hour soak, test the normal four-client fairness gate with the required warmup and sample count.

## Approach and scope

Use the existing captured-output fairness runner and immutable merged-main binary. Start with768KiB, the previously marginal case; require60seconds and100completed warmup requests, then1000measured text requests in each bypass/proxy arm. Run up to three repetitions, resetting generated state and alternating arm order with repetition-offset. Stop the campaign after the first failed required criterion, retaining its complete evidence. If all three768KiB repetitions pass, run256KiB and1MiB under the same protocol. This is the fairness latency gate only, not pure-text throughput, PDF qualification, cancellation, stress or sustained-soak acceptance.

The alternative of starting the soak immediately would spend four hours before exposing an already-marginal latency failure. Repeating short samples cannot establish the contract. Full-sample fail-fast qualification is the chosen routine execution detail under standing authorization.

## Evidence and gates

Require complete matched individual samples, zero unexpected failures, exact body/checkpoint stage coverage, measured warmup minima,1000completed text requests per arm, added first-byte p95<=150ms/p99<=500ms, matched logical-prefix relay p99<=100ms, image extraction p95<=5000ms/queue p95<=2000ms, and available resource samples within512MiB proxy/1GiB child budgets. Preserve negative paired differences, all outcomes, all resources and stage samples; report nearest-rank p50/p95/p99 per repetition. Missing evidence is not a pass. Original stage spans are diagnostic and are not added as if disjoint.

Keep builds and CPU-heavy tests idle during timing. Capture all runner output and raw reports privately. Summarize only numeric aggregates, rule/test names and statuses. No runtime/policy changes or weakened thresholds. Any missing or defective runner evidence is a harness issue to fix with regression tests, not grounds to silently qualify.

## Follow-through

Record the result and remaining gates. A failing measured criterion leads to a focused investigation; passing this subset still leaves the full throughput/lifecycle/soak contract open. Review documentation and any harness changes, pass required checks, integrate and synchronize main autonomously.
