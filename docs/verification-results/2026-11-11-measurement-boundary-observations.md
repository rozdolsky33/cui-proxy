# Observed performance measurement boundaries

Reference acceptance needs the clock values actually used by the runners.
Text/throughput reports now retain `window_start_s`, the monotonic origin
already used by the runner to count completions. Extraction and fairness
phases retain `warmup_seconds`, measured immediately after warm-up and before
post-warm-up collection or measured workload execution. These fields record
existing execution boundaries; they do not change pacing, sample counts,
thresholds, extractor behavior or policy.

Four controlled-clock regression tests exercise the actual phase orchestration
with captured fake dependencies, without spawning native workload processes.
They distinguish a pre-dispatch window origin from a later operation start,
prove elapsed warm-up differs from its configured minimum, and confirm both
fields survive private report validation. The three missing-observation tests
failed before implementation and all four pass after it.

Historical reports stay unchanged. A missing clock observation remains
incomplete in the acceptance evaluator; it must not be backfilled from the
first operation or a requested duration. New runner fingerprints require a
new campaign identity. Preserve completed text results and retain separate
source/build/script provenance when collecting the affected remaining
workloads. Do not modify an active campaign's source or checkpoint hashes.

PR #35 merged after a clean independent review and all required remote CI.
Both OS jobs completed 199 discovered workflow tests (195 passed, four native
opt-in skips), plus race/lint/vulnerability/fuzz and packaging checks.
This closes report-field gaps, not Task 04. Actual corrected-source reference
measurements remain required.
