# Native soak replay pacing correction

The November 21–22 qualification attempt is **not accepted**. The existing
timeline evaluator rejected the bypass phases because they ended before their
required two-hour horizons. No threshold was relaxed and no successful phase
was substituted into the sealed campaign.

## Retained observations

The measured binary came from clean source `7b1c6b2688b8973e3838e8891d29958b2549455c`.
Its application sources match parent `3d76b8c`; the extra commit adds a progress
ledger. Subsequent detection changes on `main` have distinct source identities
and do not inherit these measurements.

| Phase | Completed operations | Recorded failures | Elapsed seconds |
| --- | ---: | ---: | ---: |
| Proxy fixed state | 14,407 | 0 | 7,201.60 |
| Proxy mapping churn | 14,260 | 0 | 7,202.97 |
| Bypass fixed state | 14,407 | 0 | 6,835.86 |
| Bypass mapping churn | 14,260 | 0 | 6,752.87 |

All ten planned restarts and ten planned policy reloads completed. Finalization
and the independent corpus-bound provenance checks passed. These observations
do not replace the failed timeline gate or establish overall soak acceptance.
The complete protected campaign, rejected evaluator results and captured logs
remain local.

The separately collected reference matrix has 123 completed, provenance-checked
reports with zero scoped acceptance failures. Three native cancellation trials
also passed their evaluator. The reference-stage summary deliberately does not
claim full milestone qualification.

## Cause and correction

Replay consumed each client's recorded requests immediately after its preceding
request finished. It preserved order and stored schedule offsets, but did not
wait for those offsets. Faster bypass execution therefore compressed the trace.

Replay now waits against a single monotonic phase start and each request's
recorded offset before recording or dispatching that request. Overdue work runs
immediately; four client streams remain concurrent and each remains sequential.
Waits respond to internal or external cancellation. Recorded drain requests may
run beyond the nominal horizon, and a fast replay retains the full phase horizon.

Regression coverage checks actual dispatch timing, cancellation before emission,
overdue work, concurrent client order, drain timing, execution/recording failures
and completion of the full horizon. Existing order-only tests use advancing
virtual clocks rather than a frozen clock.

Validation: all 818 native workflow tests passed, including 26 focused phase,
drain and replay checks. The original fast-bypass regression failed before the
fix. Removing request pacing while retaining the final horizon wait makes the
dispatch-deadline regression fail, so sleeping only at the end cannot satisfy
the test. Independent static review found no Critical or Important issues.

## Remaining acceptance

Freeze the reviewed runner source and collect a complete new two-hour fixed-state
plus two-hour churn proxy run and its matched four-hour bypass replay. Evaluate
the sealed evidence with independent corpus-bound expected provenance. Investigate
any sustained resource growth, including growth within the numeric limit. Keep
this rejected attempt distinct from its replacement. The four-agent milestone
and current released artifacts remain unqualified until their required evidence
passes.
