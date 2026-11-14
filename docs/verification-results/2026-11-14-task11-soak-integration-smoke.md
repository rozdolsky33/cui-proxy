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
