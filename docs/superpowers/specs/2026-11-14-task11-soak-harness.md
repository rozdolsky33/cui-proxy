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


## Finite churn horizon and explicit queue drain

The churn arrival horizon remains two hours at 100 new pairs per minute,
starting at time zero: exactly 12,000 offered pairs in the half-open interval
[0, 7,200 seconds). A closed-loop client may still be processing an earlier
request when the last arrivals become due. Ending admission immediately at the
horizon can therefore leave offered pairs unassigned, even with correct steady
operation. Such backlog remains a failure; the target must not be reduced.

The sustained churn runner explicitly enables a bounded admission drain after
the arrival horizon. It continues each client's next deterministic schedule
slots, including images and lifecycle work, until all offered pairs have been
assigned or a maximum 45 seconds of additional admission time has elapsed. No
new pair is offered beyond the original horizon. Every post-horizon request is
marked `drain` and retained in the exact matched replay; no extra hidden probe
or unscheduled text-only request is permitted. Default phase execution without
this explicit mode still stops admission at its original duration.

The original measurement horizon and all 15-minute windows remain unchanged.
Drain traffic cannot fill missing in-window samples or improve the window gates.
Its outcomes and resource use remain part of safety, privacy, persistence and
post-drain recovery evidence. The existing per-request 45-second maximum also
applies during the drain, so already-admitted work can require up to another
45 seconds after the admission deadline. Both actual elapsed time and the
original horizon are reported. A drain that exhausts its deadline with backlog,
a missing persisted pair, or a failed semantic/boundary check cannot qualify.

## Campaign assembly and corpus selection

The full campaign must compose the existing executors rather than treating the
short smoke scripts as its entry point. It owns one seeded proxy environment
across fixed and churn phases: fixed completion does not recreate the vault,
reset policy generation or discard historical mappings. It retains a separate
fresh bypass environment for exact schedule replay. Warmup uses distinct request
identities and is excluded from measured schedule counts, control deadlines and
acceptance windows. Both proxy and bypass satisfy at least 60 seconds and 100
completed supported warmup requests before measurement. Reconcile warmup audit
records separately from measured requests.

The image executor consumes two already-selected fixtures; it does not select
from the whole corpus. The campaign therefore requires a deterministic corpus
selector. Validate all 800 image descriptors and hashes before any measurement,
including four template groups of 200, unique IDs and hashes, relative confined
paths, and PNG/JPEG formats. Preserve the separate PDF corpus for its dedicated
qualification; do not replace image slots with unsupported tool-result PDFs.
Select two distinct template groups for every W05/W06 request, using its stable
phase/client/serial identity. Cycle each group's image index independently of
thread completion order. Persist only fixture IDs and hashes in schedule evidence;
never expected OCR text or image bytes. Replay resolves those same IDs against
the same attested manifest, rejecting missing, duplicate or changed descriptors.
Report actual per-template and per-image coverage rather than assuming that
elapsed duration implies coverage. Two-fixture smoke selections must never be
reported as full-corpus execution.

Root evidence must be created in a new private directory and durably record
source commit, clean-state attestation, proxy/seed-helper hashes, compiler and
host metadata, workload/corpus identity, declared negative-policy variant,
settings and phase links. Preserve original measurement horizons and separately
record bounded churn drain time. Each fixed/churn/replay phase keeps its existing
hash-chained event file and completion seal. The root completion record refers
to those seals and their hashes; a completed event chain is not qualification.
Any setup, warmup, measurement, drain, snapshot, audit or cleanup failure retains
an explicitly incomplete root record and all evidence already collected.

Keep the resource monitor active across measured phases and drains. Record phase
and control boundaries on the same monotonic clock. Collect idle/goroutine and
equivalent observed-GC baselines after warmup, and post-drain observations without
hiding unavailable samples. Schedule ten graceful restarts and ten reloads over
the combined four-hour proxy horizon, retaining generation and restart counts
across the phase boundary. Control scheduling begins after warmup; overdue
controls cannot be silently counted as executed. Matched bypass replay preserves
request identities, selected fixtures and original arrival/drain markers while
recording its own observed timing and outcomes.

Before deleting an owned environment, archive its audit and diagnostic logs,
initial/final named vault snapshots and failure metadata privately. Reconcile
all request IDs, recheck rejected upstream absence, and scan retained logs for
all seed/churn originals plus negative canaries. Root metadata includes only
counts, hashes, rule names and bounded diagnostic codes. Check cleanup results;
a failed cleanup or absent evidence prevents acceptance rather than producing
an apparently successful partial report.

### Corpus selector interface and partitions

`CorpusSelection(images).select(plan, warmup=False)` consumes the existing
`plan_request` identity tuple (or its JSON list representation), validates an
image slot, and returns defensive copies of two descriptors. Metadata validation
requires the existing four format/dimension groups, 100 measured indices 0–99
and 100 warmup indices 100000–100099 per group. The 800 descriptors are validated
together, but measured requests cycle only the 400 measured images; warmup uses
only its disjoint 400-image partition. This preserves the original OCR corpus
contract. Coverage reports distinguish partition and template.

Selection uses image ordinal `((serial // 10) * 2 + serial % 10 - 7) * 4 + client`.
The first template is ordinal modulo four; the second is its next template.
Index is ordinal divided by four modulo 100, offset by 50 in the churn phase,
and by 100000 for warmup. Selection therefore depends only on stable identity,
not request completion order. File containment, actual bytes/hash and image
validation remain separate preflight requirements; metadata validation alone
does not attest fixture contents.

`CorpusSelection.evidence(plan, warmup=False)` returns only ordered ID/hash
pairs. `resolve(plan, evidence, warmup=False)` rejects changed order, cardinality,
hashes, extra fields, partition or request identity before returning private
descriptors. `ScenarioExecutor(..., corpus=selector)` uses that selection for
image requests and emits `fixture_evidence` in the observed result. When a replay
plan carries `fixture_evidence`, the executor resolves it before extraction and
rejects replay evidence without a selector. The campaign replay assembler must
join these result identities to the sealed schedule; this does not change the
strict existing journal schema. Physical file verification remains required
before any campaign declares the corpus attested.

The executor's `warmup=True` option forwards the partition choice through
selection, replay resolution and recorded fixture evidence. Campaign warmup
must still use distinct request identities and audit reconciliation; choosing
the image partition alone does not establish that separation.

`verify_image_file(root, descriptor)` performs bounded per-file preflight. It
walks relative components through directory descriptors with no-follow flags,
opens the leaf nonblocking, requires a regular file of the declared size, hashes
at most that size plus an EOF check, and rejects changed file identity/times or
a mismatched digest. It returns only ID, byte count and hash. This attests the
bytes observed during the check; campaign identity checks must bracket use, and
the corpus remains private and owned throughout execution. Full corpus hashing
and real extraction verification run outside active reference timing.

### Root evidence collector

`CampaignEvidence(root, identity, settings)` creates a new private directory
and hash-chained root event stream. `link_phase(name)` accepts the four ordered
phase names `proxy-fixed`, `proxy-churn`, `bypass-fixed`, `bypass-churn`; it verifies
the existing child seal and framing, retains event/seal hashes and event count,
and rejects incomplete children. `finish()` requests completion after checking
all linked identities. Only a clean context exit writes a completed root seal;
exceptions after that request retain an incomplete seal. Child hashes are checked
again on exit so post-link changes cannot silently pass.

This collector proves structural links and recorded completion states only. It
does not validate host/workload identity semantics, phase durations, schedule
correspondence, latency/resource gates or the full acceptance contract. It always
records `performance_qualified=False`; Task 4's evaluator must independently
prove every original requirement. The campaign driver must perform cleanup
inside this context before its clean exit.

`attest_corpus(root, manifest)` validates the version-2 corpus structure, creates
a defensive selector snapshot, verifies all 800 image files and both two-page
PDF files, and returns the selector plus counts and identity hashes. The manifest
hash includes the independent OCR expectations and rendering provenance; the
file-set hash includes sorted observed file IDs, sizes and digests. PDF kinds
must include both text-layer and scanned cases, and identities, paths and hashes
are unique across images and PDFs. A failure anywhere prevents an attestation.
This byte/metadata attestation does not establish OCR accuracy or qualification.

The root evidence collector synchronizes its new directory entry in the parent
after writing its start record. A parent-sync failure closes the writer and
retains an incomplete seal when storage remains writable. This is best-effort
evidence retention on a filesystem failure, not a promise that failed storage
can provide durability.

`load_manifest(root)` reads a regular `manifest.json` without following symlinks,
with a 4 MiB metadata limit, bounded reads and file-change checks. It rejects
invalid UTF-8/JSON, duplicate keys at any depth, non-finite numbers (including
finite-looking exponent overflow), and non-object roots. Descriptor/schema and
physical fixture checks remain the subsequent attestation step.

### Warmup identity separation

Warmup executors use the explicit `soak-warmup` operation/session namespace,
while measured requests retain the original `soak` namespace. Text construction
and sending forward the warmup flag; image operation IDs and sessions follow
the same separation. Identical phase/client/serial coordinates therefore cannot
collide across warmup and measurement. No guessed high-serial offset is needed.
Warmup accepts only supported text/image schedule slots and rejects lifecycle or
negative scenarios before sending a request. Its elapsed-time and completion
minimums still require the campaign warmup driver; namespace separation alone
does not satisfy them.

`run_warmup(execute, minimum_seconds=60, minimum_count=100, ...)` drives four
closed-loop clients using supported text/image schedule slots. The supplied
executor must have warmup identity/partition enabled. It requires both minimums
and at least one verified success from each client, records starts before send
and every observed result, and counts only completed, semantic-, boundary- and
receipt-verified supported successes. An explicit operation exception remains a
failure even if its retained fields resemble success. Cancellation, recording
failure or worker failure stops new admissions; in-flight requests retain their
existing bounded executor deadlines. Failed warmup raises `WarmupFailure` with
its partial report. Warmup time and outcomes remain outside measured windows.

### Owned environment retention

`performance.Environment(..., retained_directory=path)` uses a new explicit
0700 evidence directory whose lifecycle survives environment cleanup. Child
process termination and log closure still use the existing environment cleanup;
only directory deletion is disabled for this explicitly selected owner. Parent
directory metadata is synchronized at creation. Existing paths are rejected,
and there is no temporary-directory finalizer that later deletes retained data.
The default remains an ordinary auto-cleaned temporary directory.

The campaign creates one retained directory for its persistent proxy environment
and a separate retained directory for bypass. This preserves final shutdown logs,
vault snapshots, private configuration and setup failures without copying files
while processes still write them. The root collector can finish only after the
environment exits and owned child/log cleanup is verified. These private
artifacts must never be committed or printed in the assistant session.

`bind_fixture_replay(execute, report, corpus)` requires a completed proxy phase
and an exact schedule/outcome identity bijection. It resolves all image proofs
against the attested selector before returning an executor wrapper, then adds
the recorded fixture evidence to each matching replay call. Changed request
coordinates, scenario, size, churn indices or supplied fixture evidence are
rejected. It leaves the strict journal format unchanged; the recorded replay
runner still validates original timing, ordering and drain markers. The report
must itself come from sealed, identity-checked campaign artifacts.

### Persistent mode orchestration

`run_proxy_phases` warms up in a distinct namespace, observes idle/GC baselines,
then executes fixed and churn phases against one persistent environment and one
restart/reload controller. The fixed-phase final persisted mappings become the
churn baseline. An incomplete phase or unmet mapping requirement prevents the
next phase; partial reports are retained. Idle/GC, negative-arrival and lifecycle
checks follow both phases. Returned summaries remain pending finalization.

`run_bypass_phases` requires both completed proxy reports before warmup, resolves
all measured image proofs through the attested corpus, and replays the original
fixed/churn schedules with their original durations and churn drain setting.
Its warmup uses the separate warmup namespace and corpus partition. A warmup or
phase failure retains its report and prevents subsequent phases. The caller must
authenticate source reports against linked campaign artifacts before calling.
Neither mode runner asserts qualification: environment cleanup, final source and
corpus identity, audit reconciliation, privacy checks and the original acceptance
evaluator remain mandatory campaign-level work.

`CampaignEvidence.load_report(name)` reads only an artifact registered by the
active collector. It uses the originally saved byte count as its read bound,
rejects symlinks and nonregular files, checks file metadata stability, and verifies
the original SHA-256 before parsing JSON. Replay assembly uses this authenticated
readback instead of trusting a mutable in-memory or independently loaded report.
The child phase seals remain separately bound by the campaign link checks.

### Campaign command and finalization boundary

`scripts/workflow/soak.py` composes the normal workload plus its declared negative
rule, stable identity probe, retained campaign coordinator and concrete audit/log
finalizer. Default fixed/churn durations are 7,200 seconds each, warmup is both
60 seconds and 100 successes, controls are ten restarts and ten reloads, and
churn drain is bounded at 45 seconds. Explicit shorter settings support diagnostic
smokes only; command completion never claims performance qualification.

The command requires `--binary`, `--seed-binary`, `--fixtures`, `--build-manifest`,
`--binary-commit` (full commit ID), and a new `--output` directory. The identity
probe binds the manifest hash, declared clean binary source commit, binary bytes,
embedded Go VCS revision/clean bit, clean harness source identity, policy hash,
fixture manifest and validated reference host. Current load is checked by the
reference preflight but excluded from immutable host identity. Harness and proxy
source revisions are recorded separately, since qualification tooling may be
maintained independently of the frozen proxy candidate.

The coordinator authenticates both proxy summaries before creating the bypass
environment. It checks every owned child has exited and every owned log handle
is closed, then invokes audit/privacy finalization, then rechecks source/build/host
and physical corpus identity before the next mode or root completion. Failures
leave the root incomplete and prevent later expensive work. Bypass executors
send directly to the backend URL; a dedicated routing regression distinguishes
it from the proxy endpoint.

Audit finalization includes warmup plus fixed and churn outcomes. Proxy audit
coverage must reconcile all request identities and every completed reload
generation. Bypass must leave the unused proxy audit empty. Audit input must be
regular, stable, newline-framed JSON objects with unique keys and finite numbers;
individual records are bounded at 1 MiB. All retained log files and any audit file
are checked against the initial pair set, negative canary and final persisted
mappings. Missing required logs, incomplete reports, malformed audit evidence or
privacy failure prevents finalization. This verifies collection integrity, not
original duration/window/resource acceptance; the separate evaluator remains
required before full qualification.

### Shared timing and retained stage observations

Phase reports retain their actual monotonic `started_s`; resource samples retain
`observed_s` on that same clock, in addition to monitor-relative elapsed time.
This makes phase-window resource comparisons possible without inferring starts
from file creation time or rounded durations.

The campaign stage collector preserves the existing per-request stage metadata,
filters distinct warmup session pseudonyms, and writes each first observation to
the root hash chain with its monotonic observation time. Repeated polls retain
the first observation time; polling errors are counted and recorded without raw
exception details. Evidence-write failure stops further phase admissions.
A final stage report is retained even if a measured phase raises. Stage first-seen
time is the collection time, not the original request start; the acceptance
evaluator must explicitly reconcile window attribution and sampling gaps rather
than silently treating those as identical timestamps.

The measured frozen binary may lack embedded VCS metadata. In that case the
command requires `--binary-source` pointing at the original clean source snapshot.
Preflight verifies its exact Git revision, rejects tracked/untracked changes and
ignored build inputs (Python bytecode caches are allowed), reproduces the recorded
restricted Go build into a separate temporary output, and requires a byte-for-byte
SHA-256 match with the measured binary. It rechecks source cleanliness after the
build. Subsequent identity probes may reuse that build proof only for the same
source/commit/binary/command identity and recheck source cleanliness each time.
Present but incorrect embedded VCS metadata remains a hard failure. The measured
binary is never replaced by the verification build.
