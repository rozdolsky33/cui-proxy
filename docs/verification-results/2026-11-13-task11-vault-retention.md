# Task 11 retained request buffers and interrupted workloads

Status: focused regressions and retained-memory diagnostics pass; full validation/review/integration pending.

## Findings

The vault accepted small string views into request fields. Reservations stored those views as map keys, duplicate admissions replaced existing keys with fresh request-backed views, and reverse mappings retained the supplied real string. This kept entire backing strings alive even when only a short value was needed. Storage-ownership regressions failed on all these paths before the fix, then passed with exact restoration unchanged.

New reservations now clone only their value bytes, existing reserved keys remain unchanged, and reverse values are detached on insertion. Production code uses no unsafe operations. Tests compare storage ownership while keeping the source strings alive; they do not depend on noisy process-memory thresholds.

Interrupting a count-based mixed run also left its workers dispatching until the original count finished, because executor teardown waited before a stop signal was set. Interrupts during submission could leave workers waiting at their start barrier. Controlled real-thread tests reproduced both paths (including KeyboardInterrupt/SystemExit while waiting). The exception path now sets stop and aborts the barrier before joining, while propagating the original exception. In-flight operations retain their existing bounded timeout; this does not claim immediate cancellation of a blocked HTTP read.

## Retained-memory diagnostic

Identical private diagnostics against merged reference40e3122 and the candidate. Each process uses256values of32bytes, each sliced from a separate262176-byte backing string. Three alternating reference/candidate pairs per scenario, with explicit GC before and after and exact membership/restoration checks. All18invocations passed. The table shows median retained-heap deltas; this isolates backing retention rather than estimating whole-proxy RSS.

| Scenario | Reference retained MiB | Candidate retained MiB |
| --- | ---: | ---: |
| reserve | 66.017 | 0.031 |
| new mapping | 66.092 | 0.113 |
| duplicate admission | 66.004 | 0.011 |

Private evidence: `/private/tmp/cui-task11-retention-diagnostic-summary.json` and matching captured logs. Targeted vault/transform/proxy race tests passed (3packages). The complete Python workflow suite passed300tests with4opt-in skips. Initial test-only mock setup errors were corrected before observing the actual interrupt assertion failures; those setup errors are not counted as regression evidence.

## Interrupted full-sample qualification

The completed bypass arm retains1000unique text cases plus2942images, zero failures, and116warmup requests over60.574seconds. Its measured window was1936.825seconds. The proxy arm was interrupted for the ownership investigation at a live counter of1665requests including warmup, zero proxy errors/blocks and4active requests. Complete client outcome records for that unfinished arm are unavailable; it is **not** a passing or complete qualification result.

The live metrics snapshot, interruption manifest, completed bypass report and captured runner output remain private. Live heap had increased from186043968bytes at1185requests to195650672bytes at1515requests while mapping count stayed10000; the isolated diagnostics above establish the retention mechanism separately. A plain Ctrl-C initially left the runner waiting; gracefully terminating only its owned test proxy allowed cleanup. Both owned processes exited before further testing began.

The distinct earlier macOS Python3.15.2 cleanup-test flake remains unconfirmed; this fix does not claim its root cause was resolved.

## Remaining verification

Run the real interrupt smoke, full-request allocation/latency comparison, full regression gates and independent review, then exact-head CI. Resume full-sample performance acceptance with explicit provenance for any reused completed bypass arm. Original150ms latency, throughput, resource and sustained-soak gates remain open.
