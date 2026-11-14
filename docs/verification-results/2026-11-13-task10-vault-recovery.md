# Task 10 mapping recovery evidence — 2026-11-13

Status: integrated in [PR #44](https://github.com/rozdolsky33/cui-proxy/pull/44), squash `62445ed`, after independent review and exact-head CI/security/packaging passed.

## Recovery boundary

The pre-forward regression failed on the preceding implementation: an upstream request was already visible while no readable snapshot held its mapping. The new file-mode callback checkpoints before forwarding. Four focused regressions were observed failing and then passing: missing checkpoint, save-error forwarding, cancellation, and ignored directory-sync errors. Closed audit/explanation catalogues also rejected the new refusal code before their required additions.

A subprocess was killed without Shutdown, Close or periodic save, then restarted with the same vault/key. Both text and structured tool arguments restored exactly. Disabling only the checkpoint callback made the tool-argument recovery test fail with the expected incorrect-restoration assertion; restoring it made the test pass. Restart also proves the dead process releases its lifetime lock; existing tests retain live-writer exclusion.

A deterministic candidate collision shows why key-only reconstruction is insufficient: an empty vault can assign the formerly exposed candidate to a different original. Reopening the persisted mapping reserves that candidate and preserves its original exactly. Concurrent checkpoints verify 32 immediate reopen results. Concurrent reload/growth followed by graceful restart verifies all 64 mappings and the shared vault identity. Five affected packages pass under the race detector, including existing policy/reload/header/audit tests.

## Growth and snapshot cost

Three independent non-race runs on the local macOS machine, each with four allocating callers and fresh vaults at 1,000 and 10,000 mappings. Every mapping is reopened and compared exactly: 33,000 mappings verified across six samples. No competing build/test campaign ran during the timed runs. Timings exclude test compilation and use monotonic elapsed durations. Values below are observed minimum–maximum, not confidence intervals or latency percentiles.

| Mappings | Growth ms | Snapshot save ms | Reopen ms | Encrypted bytes | Total allocated bytes |
|---:|---:|---:|---:|---:|---:|
| 1,000 | 52.41–66.78 | 11.20–14.11 | 1.55–2.25 | 49,061–49,077 | 15629768.00–16126664.00 |
| 10,000 | 1466.26–1483.84 | 13.39–17.33 | 15.00–15.64 | 503,859–503,883 | 485312176.00–485539480.00 |

Allocation totals cover growth, the expected-value oracle, save and reopen; they are cumulative allocations, not live RSS or a cache bound. Heap-after samples ranged about 1.6–3.5 MB at 1,000 and 7.0–7.5 MB at 10,000 and include test/oracle/runtime state. File-size variation comes from concurrent collision-assignment order. A full checkpoint writes the complete snapshot; its observed 11–17 ms cost is added only when a save is needed, and concurrent callers may share a completed checkpoint. Contention, slower storage and larger vaults can cost more. No full request-throughput or sustained fairness claim is made.

Production checkpoint source: `c232907` (subsequent commit `f18f435` adds recovery/growth tests). Private evidence: `/private/tmp/cui-task10-growth-{0,1,2}.json`, corresponding logs, `/private/tmp/cui-task10-crash-tools.log`, `/private/tmp/cui-task10-crash-mutation.log`, and `/private/tmp/cui-task10-task2-race.log`. Only aggregate metadata is published; generated originals and mapping pairs stay private.

## Limits and operations

See [mapping recovery](../vault-recovery.md) and [ADR 0004](../adr/0004-mapping-durability.md). The old periodic-only exposed-mapping crash-loss window was nominally 30 seconds plus save time and unbounded during repeated failures. This change moves the successful checkpoint before exposure; unforwarded changes can still be lost. It cannot reconstruct already-lost pre-upgrade histories and does not guarantee recovery from lost keys, faulty storage or arbitrary power failure.

Mappings and reserved originals are not evicted; memory and disk grow with workload history. Cooperating agents share a proxy/vault, while unrelated trust domains use separate instances and keys. Retiring mappings requires retiring the corresponding resumable histories. Storage failures return 503 `vault` without forwarding and without discarding in-memory state. Sync operations already underway are not cancellable.

The current snapshot format remains readable by older versions, but rollback removes the pre-forward durability guarantee. Task 08 mixed-text performance remains open; sustained acceptance and release gates remain Tasks 11–12. The snapshot cost supports keeping the existing format for this fix; a journal remains a measured follow-up option if larger or sustained workloads require it.

## Final local gates

All 23 Go packages passed race tests; 281 Python workflow tests passed with four explicit opt-in skips; go vet and staticcheck passed; all 125 HTTP corpus operations passed. The bundled manual was rebuilt and verified. An offline-link failure stopped the first full run; links to unbundled ADR/evidence pages were corrected to repository URLs before the successful rerun. The new closed storage-refusal outcome classifier was observed RED→GREEN and rejects unrelated or malformed 503 responses. Private exact-runtime/binary/manual hashes are retained in `/private/tmp/cui-task10-final-manifest.json`; aggregate gate statuses are in `/private/tmp/cui-task10-final-gates.json`.

Follow-ups for Task 12: improve the pre-existing unsupported-vault-format startup wording to preserve key/vault pairs and explicitly retire old histories. One macOS Python 3.15.2 interruption-cleanup test failed on the first CI head; ten local repetitions and both operating systems on the final head passed. Value-free exception-kind/errno diagnostics were added, but the intermittent root cause remains unconfirmed. No cleanup assertion or quality gate was relaxed.
