# Task 12 release-gate infrastructure

Status: implemented and locally verified; final independent review and exact-head CI are required before integration. This does not close the pending Task 08/11 performance milestone or the final Task 12 operating-envelope publication.

## Changes

Unsupported vault-format guidance now preserves the vault/key and directs operators to a compatible binary. Release, rollback and uninstall guidance distinguishes preserving mappings from intentionally retiring dependent histories. The offline manual includes the new release-readiness page.

CI adds a bounded, report-only comparison after correctness and packaging checks. It uses identical Go benchmark source, one shared generated 768 KiB normal-tier fixture, five alternating baseline/candidate pairs and ten iterations per invocation. Two Go execution slots and a 30-second test timeout bound each invocation; the whole CI step has a five-minute limit. Executable hashes are checked throughout, tracked source modifications are rejected, and existing evidence is not overwritten. Only numeric summaries, fixed identifiers and build/workload metadata are uploaded for seven days. Changed harnesses remain unavailable, and missing/skipped/malformed measurements fail instead of becoming zero-valued successes.

## Verification

- 23 Go race-test packages, vet, staticcheck and the isolated HTTP corpus passed. The embedded manual was regenerated and built successfully.
- The final local workflow suite passed 323 tests with four opt-in skips. Parser, sample-count, finite-number, equal-work, interruption, immutable-binary and evidence-preservation regressions are included.
- Real runner verification completed all ten benchmark invocations. An earlier missing-fixture attempt was correctly rejected when Go skipped the benchmark; explicit shared fixture setup resolved it. A regression covers Go's optional throughput column.
- Detailed logs and generated inputs remain private under the Task 12 verification prefixes in the local temporary evidence directory. These short diagnostic results are not reference-machine performance acceptance.

## macOS interruption cleanup

Remote CI reproduced the previously recorded permission failure. A controlled real Darwin zombie-only process group reproduces the same failure class against the old liveness check; the candidate passes that exact kernel condition without skipping. Mocked boundary regressions also establish that live groups, failed/empty/malformed process-table observations and genuine permission denials cannot be accepted as completed cleanup. Process inspection has a one-second bound.

Apple's [process-group signal implementation](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/kern_sig.c#L1580) excludes zombie members and can return `EPERM` when no eligible group member remains. Cleanup now requires successful process-table evidence that no live members remain before accepting this condition. A signal/exit race uses the same conservative check. Ten repeated real interruption trials passed after the fix; the temporary repetition loop was removed from routine CI in favor of the deterministic real-kernel regression.

The historical intermittent CI failure did not include its precise call site, and the diagnostic repeat happened to pass. Do not claim certainty about every historical occurrence: the new regression proves and fixes the matching Darwin cleanup failure path. Future failures retain value-free exception type, errno and function/line metadata.

## Remaining milestone work

Full matched latency/fairness repetitions, sustained-load/recovery acceptance and the final measured operating-envelope assessment remain open. No live model spend or installation mutation was performed. This PR provides infrastructure and safe operational guidance; it does not publish a release or assert four-agent qualification.
