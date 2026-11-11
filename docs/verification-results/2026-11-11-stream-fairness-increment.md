# Stream timing and mixed-load diagnostic increment

This increment adds measurement infrastructure for roadmap Task 04. It does not qualify performance or complete that task.

The fake upstream records monotonic timestamps immediately before writing each logical delta and after flushing it. The client records when the matching restored prefix becomes available. Stream boundaries follow semantic text boundaries so substitutions of different lengths do not shift the comparison. Paired proxy/bypass results retain signed differences, including flush timing jitter. Missing, malformed or incomplete timing samples are unavailable, never zero latency.

`performance_load.py` provides a four-client closed-loop scheduler (one text client, three image clients), a bounded open-loop scheduler, continuous stage-history collection, and `run_fairness_phase` for isolated native diagnostics. The mixed phase validates text restoration and image extraction independently and retains failed attempts. Stage polling preserves entries across history rotation and rejects incomplete coverage. Long-run qualification still requires the reference workload and repetitions from the workload contract.

The open-loop scheduler accounts for scheduled slots, dispatched attempts, completed responses, HTTP 4xx responses and client-unsent slots. A dispatched attempt is not proof of upstream receipt. HTTP 4xx counts are not a claim of documented overload admission. Late slots are retained as unsent rather than issued in a catch-up burst; outstanding work is capped at 16. The scheduler is infrastructure only: the native overload phase and CLI integration remain outstanding.

For mixed image traffic, the fake upstream returns only the shared text workload. Proxy requests separately validate the extracted text layout; bypass requests validate raw body transport. The bypass backend does not run OCR or receive the expected extracted text. This isolates the text-client fairness measurement from a second OCR implementation.

## Verification

On 2026-11-11, workflow discovery completed 101 tests successfully (native tests remain opt-in), and the explicitly enabled native mixed-load suite completed 12 tests successfully. `git diff --check` passed.

Portable regression tests cover semantic prefix alignment, timestamp validity, signed pairing, shared-text attachment replies, mixed-client lifetime, failure retention, open-loop capacity and late slots, invalid-setting preflight, stage-history rotation and attachment-oracle field boundaries. Opt-in native tests run both proxy and bypass with two measured text requests while three image clients remain active. These short runs are smoke tests; their source state and duration do not meet reference qualification requirements.

Remaining work includes the overload phase and CLI, full reference matrices and repetitions, mixed-load fairness acceptance measurements, churn/stress validation and consolidated evidence. No performance thresholds are claimed by this increment.
