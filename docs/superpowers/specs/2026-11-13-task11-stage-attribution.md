# Task 11 checkpoint and workload-stage attribution

The rejected bounded-regexp experiment improved a microbenchmark without meeting mixed-request latency acceptance. Existing stage summaries combine text and images, and the Task10 mapping durability fence is outside named stages. Measure these gaps before selecting another performance change.

Add one closed numeric stage, `mapping_checkpoint`, around the configured pre-forward persistence callback. Preserve all failure, cancellation, panic recovery, audit ordering and upstream behavior. A nil callback produces no observation. Append the enum value so existing stage identities remain stable. Aggregate and optional correlated recent-request metrics use the existing bounded machinery; retain no new request values.

Extend the mixed workload report with numeric per-stage arrays separated into text and image requests. Use existing measured correlated rows, exclude warmups, and validate measured-row coverage against the completed workload. The normal mixed fixture has extraction stages only on image requests, including cache hits. Treat inconsistent or missing attribution as invalid evidence rather than inventing zero durations. Keep legacy extraction-only samples unchanged. Report checkpoint coverage explicitly when examining an older binary that lacks the new stage.

No runtime performance optimization, persistence weakening, new configuration switch, unbounded history or latency-gate change. Record a short diagnostic with text-only stage percentiles and checkpoint coverage; do not claim full qualification. Task08/11 and the four-hour soak stay open.

Acceptance: callback outcome/absence timing tests, bounded closed-stage metrics, exact workload stage separation and warmup exclusion, malformed/duplicate/missing-row rejection, unchanged existing reports/tests, manual build, required local/remote gates and one fresh final review.
