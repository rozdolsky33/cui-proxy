# Task 06 refusal recovery and preflight evidence

The proxy preserves existing statuses and error codes/messages while adding
closed recovery categories, retry guidance, fixed remedies and aggregate
recurrence counts. Extraction queue deadlines now have a distinct busy detail.
Malformed structured metadata is distinguished from policy decisions.

The guarded preflight endpoint reports running-process build identity and
current policy. Dependency checks use service PATH and the extractor's exact
restricted child environment. Results have bounded output, time, concurrency
and cache lifetime. CLI reads the service report rather than probing locally.

## Verification

- Recovery tests observed failures before implementation for category mapping,
  unknown-input safety, recurrence and malformed metadata classification.
- Repeated rejected-image requests return guidance and increasing category
  counts without any upstream call; legacy status/type/code remain unchanged.
- Concurrent recurrence counts and service probes pass race checks.
- Preflight tests cover missing tools, English data, restricted environment,
  excessive output, cancellation, concurrent cache reuse, policy changes,
  guard enforcement, CLI service querying, redirects and invalid metadata.
- A real CLI invocation against a running dashboard with an intentionally empty
  tool PATH returned exit 1, identified that service's build, and reported four
  missing executables plus the unperformed language check. This confirms it did
  not substitute the invoking shell's environment.
- Dashboard JavaScript syntax and strict manual generation passed. The browser
  tool had no available browser, so no visual screenshot verification was
  possible; dashboard endpoint/security behavior is tested. This limitation is
  explicit rather than represented as visual evidence.
- Repository gates: Go race suite (22 packages), Python suite (266 tests with
  four existing opt-in skips), vet, full staticcheck and deterministic HTTP
  workflow corpus. Captured outputs are kept outside the repository.

## Decisions and limits

Routine implementation proceeds under standing authorization. Recurrence means
an aggregate closed failure category across clients, never request identity.
This loses per-client retry attribution in exchange for avoiding bodies and
reusable content fingerprints. State is memory-only and expires after a minute.

Retry guidance cannot control third-party clients. The documented recovery is
to cancel futile retries, remove rejected content from retained history, and
start a fresh permitted session when necessary. No automatic deletion or policy
weakening occurs. Runtime dependency discovery and language listing do not prove
successful extraction for every document. Unknown/dev build identity cannot
establish an exact binary revision. Task 04 performance findings remain intact.
