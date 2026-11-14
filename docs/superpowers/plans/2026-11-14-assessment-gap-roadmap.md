# Assessment gap roadmap

**Created:** 2026-11-14
**Status:** All iterations `ready` or `blocked`; none started.
**Assessed tree:** `9eea84d` (v0.6.0 plus PRs #46–#50).
**Owner:** Project maintainer; record an implementer and reviewer when starting each iteration.

This roadmap turns the 2026-11-14 repository assessment into ordered,
independently shippable iterations. It complements the
[daily-use and multi-agent roadmap](2026-11-10-daily-use-multi-agent-roadmap.md):
that roadmap owns performance qualification (its Tasks 08, 11 and 12); this one
owns the remaining gaps between what the proxy promises and what is
demonstrated. It is a backlog, not a claim that any listed capability exists.

## How to use this roadmap

Work one iteration at a time. Each iteration:

1. Starts with a short spec in `docs/superpowers/specs/` and an implementation
   plan in `docs/superpowers/plans/`, written when the iteration begins, not
   before. Earlier iterations supply measurements that later ones need.
2. Lands as one or more small PRs from an isolated branch, with the existing
   gates passing (`make test`, `make lint`, `make fuzz`, `make vuln`).
3. Ends by updating its row in the tracker below and adding a dated entry to
   the status log, including rejected approaches and failed measurements.

Every iteration preserves fail-closed behavior, value-free logs and the
captured-output rules in `AGENTS.md`. Fixtures use fabricated values only.
An iteration may close as `not needed` when a measurement shows the gap does
not exist; record the measurement.

## Starting assessment

Qualitative engineering judgments, not completion percentages.

| Dimension | Assessment on 2026-11-14 | Evidence or gap |
|---|---|---|
| Local risk-reduction layer for supported Codex flows | 7.5/10 | Fail-closed request path, pre-send restoration self-check, value-free diagnostics; race tests, vet and staticcheck pass; core package coverage 81–100% |
| Dependable daily use with sensitive work | 5/10 | Detection recall unmeasured, model-quality effect unmeasured, latency gate open, one live smoke against one client version |

## Iteration tracker

Use `ready`, `blocked`, `in progress`, `done`, or `not needed`.

| ID | Iteration | Priority | Depends on | Status |
|---|---|---|---|---|
| G01 | Correct stale persistence documentation | P0 | — | ready |
| G02 | Measure detection recall and false positives | P0 | — | ready |
| G03 | Extend builtin detectors from measured misses | P0 | G02 | blocked |
| G04 | Run the paired live semantics corpus | P0 | — | ready |
| G05 | Decide and enforce unknown response-event policy | P1 | — | ready |
| G06 | Add upstream stream idle timeout | P1 | — | ready |
| G07 | Add a scheduled client-compatibility canary | P1 | G05 | blocked |
| G08 | Bound vault growth and checkpoint cost | P1 | daily-use Task 11 measurements | blocked |
| G09 | Verify real OCR and constrain extractors | P2 | — | ready |
| G10 | Decide the governance-control scope | P2 | — | ready |
| G11 | Commission an independent security review | P0 release gate | G01–G06 | blocked |

Recommended order: G01, G02, G04, G05, G06, G03, G07, G08, G09, G10, G11.
G01, G02, G04, G05 and G06 have no dependencies and can run in parallel
branches. The latency and soak gates stay in the daily-use roadmap and are not
duplicated here.

## G01 — Correct stale persistence documentation

**Gap:** `docs/architecture.md` and `docs/assurance.md` describe file-mode
mappings as saved every 30 seconds with a crash-loss window.
[ADR 0004](../../adr/0004-mapping-durability.md) and the code
(`internal/proxy/proxy.go` `forward`, `cmd/cui-proxy/app.go`
`persistMappings`) checkpoint before every forward.

**Scope:** Update both documents, `docs/vault-recovery.md` cross-links and
`docs/documentation-status.md`. State the actual guarantee and its limits
exactly as ADR 0004 does (successful filesystem operations and process
crashes, not faulty storage or power loss). Search the remaining docs for
other descriptions of the periodic-only behavior.

**Acceptance:** No operator document describes periodic-only persistence as
current behavior; the strict docs build passes.

**Non-goals:** Changing persistence behavior.

## G02 — Measure detection recall and false positives

**Gap:** The central promise depends on detection, and nothing measures how
much the builtin set and a typical dictionary catch or over-match. Known
builtin misses are listed in `docs/limitations.md`: usernames, local paths,
hostnames that encode an address, and cloud access keys.

**Scope:**

- Define a fabricated, generated corpus of realistic developer-session text
  (shell output, configuration files, stack traces, environment context, Git
  metadata, cloud CLI output) with machine-readable labels for every planted
  sensitive value and its category. Generate it from a seeded generator so no
  literal corpus is committed beyond small seeds.
- Add a runner that reports, per category and per rule: planted count, detected
  count, missed count and false-positive count on clean control text. Output
  contains rule names, categories and counts only.
- Record a baseline in `docs/verification-results/`.

**Acceptance:** A repeatable command produces the per-category table; the
baseline is recorded; the corpus generator and runner have their own tests.
The numbers are published as measurements, not as a pass.

**Non-goals:** Setting a recall threshold before the baseline exists; using
any real data.

## G03 — Extend builtin detectors from measured misses

**Gap:** Builtins omit categories that appear in almost every Codex session.

**Scope:** Using G02's baseline, add detectors for the highest-count missed
categories. Candidates, to be confirmed by measurement: home-directory paths
and the usernames in them, hostnames that encode an address, and common cloud
access-key formats (currently only an example regex in `examples/config.yaml`).
Each new detector needs a replacement class that restores losslessly, a
restoration self-check case, cost tests alongside `internal/detect/cost_test.go`
and fuzz seeds.

**Acceptance:** G02's runner shows the targeted categories' recall improved
with no increase in clean-control false positives beyond a threshold set in
this iteration's spec; `BenchmarkRequestWalk` shows no latency or allocation
regression outside the daily-use contract; `docs/limitations.md` and
`docs/rules.md` reflect the new set.

**Non-goals:** Semantic or model-based classification; claiming complete
detection.

## G04 — Run the paired live semantics corpus

**Gap:** `docs/protocol-support.md` defines a five-case paired corpus (`argv`,
`file_edit`, `config_edit`, `relationship`, `tokenization`) and
`scripts/workflow/live_semantics.py` summarizes it, but no trials have been
run. Whether masking changes model task success is unknown.

**Scope:** Run at least five paired direct/proxy trials per case under the
protocol already documented there, keep raw evidence private, and publish the
aggregate counts. If `relationship` or `tokenization` shows discordance,
document it as a user-facing limitation with guidance (for example, which
value classes break ordering or subnet relationships).

**Acceptance:** A dated verification result records all attempted pairs,
including failures, with the model revision and client version; limitations
are updated to match the observations.

**Non-goals:** Statistical equivalence claims; changing synthetic generation
in this iteration. A finding that format-preserving synthetics should keep
relationships becomes its own later iteration.

**Requires:** Maintainer-run live trials; this cannot be completed by an
unattended agent without an authorized account.

## G05 — Decide and enforce unknown response-event policy

**Gap:** Unknown delta events, non-summary reasoning events and non-JSON event
data pass through unrestored. If the backend adds a new event type that carries
tool arguments, a tool could execute against a synthetic value. This is not a
disclosure, but it is a silent correctness failure.

**Scope:** Write an ADR choosing between: keep passthrough; block unknown
events that contain a known synthetic; or count and surface them. The
recommended default to evaluate is detect-and-refuse: scan unknown event data
for vault synthetics, and cut the stream with `stream_rewrite` when one is
found, while leaving synthetic-free unknown events untouched. Implement the
chosen policy in `internal/adapter/codex/response.go` with a metric for
unknown event types (type names from a bounded set, otherwise `other`).

**Acceptance:** Tests cover an unknown delta with and without a synthetic,
split across chunks; existing `TestUnknownEventPassesThrough` and
`TestReasoningEventsPassThrough` are updated to the chosen contract;
`docs/protocol-support.md` and `docs/limitations.md` state it.

**Non-goals:** Restoring inside unknown events; guessing future schemas.

## G06 — Add upstream stream idle timeout

**Gap:** The transport bounds connection setup and time to response headers,
but `stream` in `internal/proxy/proxy.go` reads the event stream with no idle
bound. A stalled upstream holds the request and its admission slot until the
client disconnects.

**Scope:** Add a configurable idle timeout between upstream stream reads
(restart-only setting, with a conservative default chosen in the spec from
observed keep-alive intervals). On expiry, abort the client connection the
same way a mid-event upstream close does, count an error and log a value-free
line.

**Acceptance:** A fake upstream that sends headers and then stalls is cut
after the configured interval; a slow but live stream with comment keep-alives
is not; the admission slot is released; configuration docs and reference list
the setting.

**Non-goals:** An overall response deadline; retries.

## G07 — Add a scheduled client-compatibility canary

**Gap:** Compatibility rests on one recorded smoke (Codex CLI 0.167.0). The
client and backend change independently of this repository, and protocol
drift is the most likely cause of sudden breakage.

**Scope:** Package the documented synthetic live smoke as one command that
reports client version, model identifier, pass/fail per check, and the count
of unknown event types from G05's metric. Document a recurring maintainer
run (and after each client upgrade) and where results are recorded. Evaluate
whether a credentialed scheduled CI job is acceptable; default to a
maintainer-run command if not.

**Acceptance:** One command reproduces the smoke with captured output and a
value-free summary; `docs/verification.md` describes cadence and recording;
the first two runs on different client versions are recorded.

**Non-goals:** WebSocket, compaction or resume support. If the canary shows
the client has moved to an unsupported transport, that becomes a new
iteration with its own design.

## G08 — Bound vault growth and checkpoint cost

**Gap:** Mappings are never evicted. The restorer rebuilds its index whenever
the vault grows, and each request that adds a mapping writes a full encrypted
snapshot before forwarding. Cost grows with vault size for the lifetime of the
installation.

**Scope:** First measure: snapshot rebuild and checkpoint time at 1,000,
10,000 and 100,000 mappings under a changing-request workload, using the
stage attribution from the daily-use roadmap. Then choose in an ADR among an
incremental restorer index, the encrypted append journal deferred by ADR 0004,
and explicit operator-driven retirement with a drain procedure. Eviction that
can break resumable histories stays out unless the ADR defines how histories
are retired safely.

**Acceptance:** Measurements recorded; the chosen change keeps crash-recovery
tests and the pre-forward durability fence; per-request cost at 100,000
mappings meets a bound set in the spec.

**Non-goals:** Automatic time-based eviction without a history-retirement
contract; key rotation.

## G09 — Verify real OCR and constrain extractors

**Gap:** Actual Tesseract recognition was not exercised; extractors run as the
proxy user with time, size and concurrency budgets but no memory limit or OS
sandbox.

**Scope:** Add an opt-in test that runs the real Poppler and Tesseract
binaries on generated fabricated images and PDFs and checks that planted
values are recognized and sanitized; run it in CI on a runner with the tools
installed. Evaluate per-platform containment (resource limits on Linux and
macOS, a macOS sandbox profile) and implement what is supportable; document
what is not.

**Acceptance:** CI exercises real recognition; `docs/attachments.md` and
`docs/documentation-status.md` no longer carry the unverified caveat; a memory
bound or a documented reason for its absence exists per platform.

**Non-goals:** Preserving layout or visual semantics; new attachment types.

## G10 — Decide the governance-control scope

**Gap:** ADR 0001's approval verification, account allowlists and policy
locking are unimplemented and ADR 0002 is deferred. Other same-user processes
can reach the listener, and the audit chain is tamper-evident but not
externally anchored. The project name suggests a compliance role the
documentation disclaims.

**Scope:** A decision, then optional implementation. Write an ADR that either
commits to a minimal control set (candidates: upstream account allowlist,
policy lock that refuses weakening reloads, periodic export of the audit chain
head for external anchoring) or explicitly retires those ambitions and aligns
README, the executive overview and ADR statuses with a single-user
risk-reduction tool. Implement only what the ADR accepts, each as its own
plan.

**Acceptance:** ADR 0001 and 0002 statuses match reality; no document implies
an unimplemented control; accepted controls have plans.

**Non-goals:** Listener authentication for shared-network use; compliance
claims.

## G11 — Commission an independent security review

**Gap:** The codebase is young, changes quickly, and has had no review by
someone outside its development loop.

**Scope:** Prepare a review package: threat model and trust boundary from
`docs/assurance.md`, the inspection-exception list, fuzz targets and how to
run them, and the open items from this roadmap. Engage an external reviewer
and track findings privately per `SECURITY.md`, landing fixes with regression
tests.

**Acceptance:** A dated review summary (scope, reviewer independence, counts
of findings by severity, and remediation status) is recorded without
vulnerability details that are still unfixed.

**Requires:** A maintainer decision on reviewer and any cost; this roadmap
does not authorize spending.

## Status log

- 2026-11-14: Roadmap created from the repository assessment. No iteration
  started.
