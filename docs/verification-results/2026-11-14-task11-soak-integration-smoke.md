# Task 11 sustained harness integration smoke

The new harness has passed isolated real-proxy checks for the five text/tool scenarios and the two image scenarios. These are integration smoke tests, not sustained-run or performance qualification.

The proxy binary is the retained candidate built from `600c803c198f64642dc41d4b98a3c1ca5569cc91`, whose runtime tree matches merged `main` at `9eea84d`. The tests use the normal workload policy and seeded file vault in temporary environments. They do not modify the user's running proxy or configuration.

| Scope | Result |
| --- | --- |
| Full workflow unit/integration suite before live smoke | 404 tests passed in 38.097 seconds |
| W01 identifier response | Complete response, restored semantics and independent upstream-body hash passed |
| W02 file-edit tool effect | Complete structured stream, actual temporary-file effect and upstream-body hash passed |
| W03 test-output follow-up | Complete structured stream, typed arguments/call association and upstream-body hash passed |
| W04 command arguments | Complete structured stream, independently parsed command arguments and upstream-body hash passed |
| W08 conversation request | Expected response association and upstream-body hash passed; policy-reload coherence remains untested here |
| W05 text plus two tool-output images | Real OCR proxy path and original-image bypass path passed |
| W06 user image followed by tool-output image | Real OCR proxy path and original-image bypass path passed |

Text/tool smoke took 10.857 seconds. Image smoke took 8.926 seconds and used both available image formats selected from the retained reference corpus `/private/tmp/cui-real-extraction-reference-v2/manifest.json`. Image source hashes are checked by the existing attachment builder. The proxy path verifies the expected extracted upstream structure and restored response; bypass verifies the original body and does not pretend to perform OCR. All nine smoke requests returned HTTP 200 and passed their semantic checks.

## Retained local evidence

- `/private/tmp/cui-task11-soak-full-workflow.log` and `-summary.json`
- `/private/tmp/cui-task11-soak-live-text-smoke.py`, `.json`, `.log`, and `-summary.json`
- `/private/tmp/cui-task11-soak-live-image-smoke.py`, `.json`, `.log`, and `-summary.json`

The remaining rejection/cancellation scenarios, actual restart/reload orchestration, sustained phases, all-window acceptance, and original qualification gates are still required. The separately retained full fairness pair failed its unchanged first-byte p95 limit. None of these smoke results overrides that failure.

## Continuation integration follow-up

An additional isolated real-proxy smoke passed all 10 requests in 21.087 seconds: W07 parent dispatch, three distinct child sessions and result collection; W09 history, resume and opaque compaction representation; W10 establishment and restoration of previously issued substitutions sent without their originals. Every request passed complete-stream semantic and upstream-boundary checks. The lifecycle state recorded one complete W07 workflow, one complete W09 workflow and one continuation. No actual restart occurred, so this is not restart qualification. Evidence is retained under `/private/tmp/cui-task11-soak-live-continuation-smoke` (`.py`, `.json`, `.log`, `-summary.json`). The focused harness suite now has 87 passing tests.

## Actual graceful restart follow-up

A separate isolated smoke now passed a real graceful shutdown and restart, followed by restoration of old substitutions without resending their originals. The reopened vault retained 10,000 entries, and the post-restart request passed semantic and upstream-boundary checks. The smoke took 4.872 seconds; 93 focused harness tests pass. Evidence is retained under `/private/tmp/cui-task11-soak-live-restart-smoke`.

Two earlier failed smoke attempts are retained with `-attempt1` and `-attempt2` names. They exposed a harness timing assumption: `vault_size` is refreshed after requests, so it reads zero immediately after startup. The corrected check first verifies old-substitution restoration, then checks the refreshed count. This single isolated restart does not satisfy the required ten restarts under load.

## Reload coherence follow-up

A live four-client smoke changed from policy generation 0 to 1 while requests were active. All six requests passed semantic and upstream-boundary checks. Audit records observed both generations, and each request contained only one generation. Only rule labels changed; matching patterns, dictionary configuration and fail-closed settings were preserved. The smoke took 6.467 seconds; 99 focused harness tests pass.

Evidence is retained under `/private/tmp/cui-task11-soak-live-reload-smoke` and `/private/tmp/cui-task11-soak-live-reload-audit.jsonl`. The first failed attempt, retained as `-attempt1`, exposed a harness dashboard-URL mismatch and was corrected with a regression test. This establishes one under-load transition, not the required ten transitions over the full sustained run.

## Upstream fault attribution follow-up

Two isolated real-proxy requests verified explicit upstream failure and malformed successful-stream attribution. Their outcomes were `upstream_failure` and `transformation_error`; neither was counted as supported success. The smoke took 0.769 seconds, and 104 focused harness tests pass. Evidence is retained under `/private/tmp/cui-task11-soak-live-fault-smoke`. Cancellation/resource-release and independent upstream-boundary receipt checks remain required for complete fault-scenario qualification.

## Independent upstream receipt follow-up

Four live checks passed in 2.797 seconds. A successful request and both upstream-fault requests each produced exactly one upstream receipt with the expected sanitized-body SHA256. Malformed JSON returned 422 and produced zero upstream arrivals. Receipt capture stores only hashed operation IDs, counts and body hashes; incomplete capture cannot prove absence. The focused suite has 112 passing tests. Evidence is retained under `/private/tmp/cui-task11-soak-live-receipt-smoke`. Full sustained acceptance must recheck receipts after draining requests to catch any late arrival.

After the shared receipt changes, the complete workflow suite passed all 435 tests in 38.126 seconds. Captured evidence: `/private/tmp/cui-task11-soak-receipt-full-workflow.log` and `/private/tmp/cui-task11-soak-receipt-full-workflow-summary.json`.

## Stream cancellation follow-up

A real isolated proxy cancellation passed after a valid initial stream event and independent sanitized-body receipt. The client disconnected; upstream handler completion with an observed disconnect was confirmed about 47.65 ms later, and proxy active requests drained to zero. The outcome is `cancelled`, not supported success. The complete smoke took 0.688 seconds. Evidence is retained under `/private/tmp/cui-task11-soak-live-cancel-smoke`; the first failed endpoint-path attempt is retained as `-attempt1`.

All 119 focused tests and all 442 workflow tests passed after shared request-header and completion-tracking changes. The full suite took 38.046 seconds; evidence is under `/private/tmp/cui-task11-soak-cancel-full-workflow`. This stream cancellation check does not establish queued or active extraction cancellation timing.


## W11 rejection integration checkpoint

The isolated live rejection check passed all three cases: explicit policy
block, unsupported audio input, and malformed JSON. Each returned HTTP 422
with the expected reason, kind and recovery code. Independent backend receipts
reported zero arrivals both immediately and at the final recheck. A normal
request passed its semantic and sanitized-body checks. Four harness-owned logs
were scanned locally against 61 distinct protected values; none appeared.

The negative-policy variant preserves the normal rules and appends one explicit
blocking regex for an independent refusal canary. This follows the workload
contract's explicit negative-policy provision. It is not the canonical fairness
policy; its extra detector must be recorded in soak provenance. An attempted
variant that changed the existing dictionary action was rejected because it
also blocked normal traffic. No production policy was changed.

The unsupported-audio recovery code is `attachment_audio_unsupported`, while
malformed JSON uses `sanitize_bad_body`. Both have reason `sanitize` and kind
`bad_body`; the oracle requires the distinct recovery codes. The explicit rule
block requires reason `policy`, kind `rule_match`, and recovery `policy`.

Private evidence: `/private/tmp/cui-task11-soak-live-rejection-smoke` with
`.json`, `.log`, and `-summary.json` suffixes. Six unsuccessful setup/expectation
attempts are retained with `-attempt1` through `-attempt6` suffixes. The final
smoke took 2.756 seconds. This is integration evidence, not sustained-load,
final absence-after-drain, or performance qualification.
