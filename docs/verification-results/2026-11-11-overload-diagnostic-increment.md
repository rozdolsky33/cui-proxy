# Open-loop overload diagnostic increment

This increment connects the bounded scheduler to isolated native proxy/bypass runs and adds `scripts/workflow/overload_performance.py`. Task 04 remains in progress; every report is explicitly unqualified.

For each declared text size and offered rate, the runner performs a preflight and warmup, schedules a fixed window, drains outstanding work, and records resources, achieved concurrency and request outcomes. Defaults are the contract's 1, 2, 4 and 8 requests/second for 120 seconds each with at most 16 outstanding attempts. Each rate runs in bypass and proxy mode with identical offered scheduling and fake-backend pacing.

Scheduled slots equal dispatched attempts plus client-unsent slots. Dispatched attempts do not prove upstream receipt. Completed responses and HTTP 4xx responses are counted separately; HTTP 4xx counts alone do not establish documented overload admission. Late or capacity-limited slots remain in the report. Unexpected request failures stop the command with a failure exit status after saving evidence.

Sampling or cleanup failures after scheduling preserve the entire scheduler result, including unsent slots, and mark `collection_failed`. The CLI saves that partial result and exits unsuccessfully. Setup and warmup failures retain available request outcomes and identify the attempted size, offered rate and proxy/bypass arm. Existing report files are rejected before backend startup; atomic replacement preserves the most recent complete report if a subsequent write fails. Reports retain aggregate final metrics rather than recent request history.

## Verification

Workflow discovery completed successfully: 104 tests passed and four native opt-in tests were skipped (108 discovered). The opt-in native overload suite passed five tests, and `git diff --check` passed.

Regression tests cover invalid settings before setup, retained accounting on sampler cleanup failure, failure exit status with saved counts, and refusing to overwrite existing evidence. A native phase smoke and an actual CLI smoke both passed. Each smoke arm scheduled four slots: two attempts completed successfully and two remained client-unsent because the configured outstanding cap was two. Both proxy and bypass reports retained all four slots.

These smoke runs use one-second windows and reduced warmup. They establish functional accounting and report handling, not acceptance thresholds. Full reference windows, repetitions, mixed-load fairness, churn/stress and consolidated performance evidence remain outstanding.
