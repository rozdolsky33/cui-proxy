# Task 11 sustained acceptance harness

The binding acceptance contract remains
[version 1](2026-11-10-daily-use-workload-contract.md). This specification defines
its missing execution machinery; it changes no numerical gate or required
workflow. A shorter run may test the machinery but cannot qualify the milestone.

## Execution and evidence

Run one isolated proxy with normal policy, real extraction, file vault and audit.
Keep its state alive for two measured hours of fixed mappings followed by two
hours of deterministic mapping growth. Four sequential logical clients issue
70% text, 20% image and 10% lifecycle/negative requests through deterministic
cycles. Preserve every scheduled, sent, completed, failed and cancelled request.
Multi-request semantic workflows must consume their scheduled request slots;
counting a multi-request workflow as one request must not distort the mix.

Every W01–W12 workflow needs an independent semantic oracle, including actual
structured function arguments and controlled local edit/command effects.
The current text-only performance response cannot establish W02/W04. Extend
normal-pacing fixture/backend support while preserving the existing defaults.
The upstream constructs responses from received sanitized requests and never
receives original-to-synthetic mappings. Generate all bodies privately.

Record a metadata-only per-client trace with phase, ordinal, scenario, size and
assigned churn indices. Recreate exact cases for matched bypass replay; never
recompute churn from the bypass clock. Keep request identity and decoded logical
prefix association. Count injected faults separately without deleting their
outcomes or reclassifying unexpected failures.

## Initial interfaces

`soak_schedule.plan_request(client, serial, phase)` returns a mapping containing
`identity` (phase/client/serial tuple), `category`, `scenario` and `size_bytes`.
Clients are integers 0–3, serials are nonnegative integers, and phase is `fixed`
or `churn`; booleans are not integer identifiers. Every complete 50-request
cycle per client contains 35 text requests across W01/W02/W03/W04/W08, 10 image
requests across W05/W06, and five lifecycle requests across W07/W09/W10/W11/W12.
Every complete 150-request cycle equally weights the three normal sizes.
Partial final cycles remain visible and reconcile against their deterministic
plans. No body content, filesystem path, request payload or raw session ID is
part of this interface.

`soak_schedule.churn_due_indices(elapsed_seconds, assigned, capacity)` returns
the next contiguous pair indices due at 100/minute, bounded by available
occurrence capacity (1–40). Unassigned backlog is retained. The coordinator
reads its monotonic clock and allocates indices under one lock; assigned indices
are distinct from successfully persisted mappings. Non-finite/negative time,
invalid counters/capacity and counters ahead of the schedule are errors.
Bypass replay consumes the recorded indices rather than this clock function.

## Remaining required interfaces and gates

A persistent phase executor must provide real semantic operations, timestamped
one-second process-tree RSS/runtime samples, exact stage coverage, bounded
requests, and independent restart/reload/cancellation oracles. At least ten
graceful restarts and ten observable policy reloads happen under load. A reload
of indistinguishable policy alone cannot establish per-request coherence.

A fail-closed evidence evaluator must enforce full phase durations, normal
policy/fixture/binary identities, schedule and outcome reconciliation, all
15-minute latency/resource windows, fixed-state first/last median growth, and
post-drain goroutine recovery. Equivalent observed GC procedures are required
for idle heap comparisons; do not add an unsafe production GC endpoint.
Mapping churn reports assigned and persisted growth separately, without eviction.
Missing, duplicated, malformed or unavailable evidence cannot pass.

The separate 1,000-sample latency and equal 600-second throughput qualification
remain mandatory. Soak windows report their actual per-size/category counts;
the specified traffic mix does not imply 1,000 samples per size in each window.
Every sustained-growth finding requires an explicit investigation before final
acceptance, even if its numerical growth is below 10%.

The eight/sixteen-client stress envelope and all other original gates remain
required. Optional paid live-backend tests remain outside execution without a
user-supplied spend cap. Final release documentation must distinguish completed
qualification from diagnostics and unavailable evidence.

## Structured tool-effect boundary

`soak_semantics.build_tool_call(scenario, received_values, call_id)` is a fake
upstream helper for W02/W04. Its only data are the values received in the
sanitized request; it has no vault or original-value oracle. It emits a minimal
function-call item with JSON arguments. W02 specifies a fixed local effect file
and replacement; W04 specifies a quoted local command plan, never execution.

`verify_tool_effect(scenario, call, originals, expected_call_id, workspace=None)`
is an independent client oracle. It checks call association, exact function and
argument shape, duplicate JSON keys, exact restored values and semantic effect.
W02 edits only an existing fixed file in a caller-owned workspace, refusing
symlinks and incorrect initial contents. W04 parses tokens without executing a
command. Failure messages and returned evidence contain only closed labels,
never argument text or original values. These unit oracles do not establish
streaming, upstream-boundary or end-to-end W02/W04 coverage until integrated
with the real proxy and normal-policy fixture/backend path.

## Structured stream collection

`soak_tool_stream.tool_events(call)` supplies a fixed 32-argument-delta script
for the controlled upstream. The sender must apply the specified backend delay
and interval and record actual delta send/flush times. The script itself does
not sleep or fabricate send-time evidence.

`ToolStreamObserver(expected_call)` collects decoded events or fragmented SSE
bytes. It enforces item/call association, argument-prefix integrity and a matching
terminal call. It records 32 logical decoded-argument prefix availability times;
if the proxy buffers arguments, the resulting delay must remain visible.
Duplicate JSON fields, contradictory event names, invalid clocks, malformed or
unfinished data cannot establish valid completion. Pending frames are bounded
to 1 MiB for these controlled small-argument scripts. This is a harness bound,
not a new proxy protocol limit.

Backend send-time metadata is retained separately and may be unavailable.
Semantic stream validity does not qualify latency or relay evidence: the phase
and acceptance layers must reject missing, malformed or unmatched timing
samples. These collectors still require real-proxy request-path integration,
normal-policy boundary checks and actual tool-effect oracles.

The existing `performance.request` accepts an optional `observer_factory`
which constructs a fresh observer for each request. Omitting it retains the
existing text observer. Body/layout boundary hashes, real HTTP reading, deadline
handling, connection cleanup and outcome accounting remain in the common path.
In-memory socket tests cover structured completion, missing-terminal failure,
first-byte/prefix recording and default text behavior. They do not substitute
for real-proxy integration or measured backend pacing.
