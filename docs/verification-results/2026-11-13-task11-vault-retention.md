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

## Full-request controls

Eight alternating equal-work pairs used the same32changing768KiB bodies and32timed iterations per arm. Relative to merged PR47, rotating-request median latency changed-0.337% and fixed-request latency-0.230%, effectively unchanged. Rotating allocated bytes increased2.301% and fixed allocated bytes1.558%; this fix reduces **retained backing storage**, and is not presented as a per-request allocation reduction. Detection and cold/warm restoration controls did not materially regress.

Private evidence: `/private/tmp/cui-task11-retention-micro-summary.json`. No timing campaign ran alongside another build or test. Nine source/AST checks confirmed the workload generator, fake backend, fixture builder, phase setup and normal mixed-loop behavior are unchanged; the only mixed-loop addition is exception cleanup. Any resumed run will retain the completed bypass artifact and separate provenance explicitly rather than pretending its proxy arm completed.

## Regression verification

All local regression gates passed on the final runtime change: 23 Go race-test packages, 300 workflow tests (4 opt-in skips), go vet, staticcheck, and the HTTP corpus with all three boundary mutations detected. Detailed output is retained privately. The HTTP report records a dirty tree because verification documentation was being updated; runtime source was unchanged from ff6272f.

Independent review and exact-head remote CI remain required before merging. Full performance qualification remains open; the interrupted proxy arm is not acceptance evidence.
