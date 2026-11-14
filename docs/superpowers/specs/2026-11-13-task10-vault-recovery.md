# Task 10: mapping lifetime and crash recovery

## Intent and current evidence

Cooperating agents must share stable mappings across graceful restart and abrupt termination. Existing file mode writes changed vault snapshots every 30 seconds and during shutdown; it retries failed saves. The save interval is not a hard crash-loss bound: slow or failed saves extend it. Restoration only knows mappings present in the reopened vault. A synthetic already exposed upstream but absent from the last snapshot can remain unresolved, or later be assigned to another original after a collision. Characterize this boundary before changing it.

The owner authorized autonomous roadmap execution and opted out of routine design approvals. The choices below are implementation decisions, not preferences attributed to the owner.

## Approach

Compare retaining periodic-only snapshots, committing a snapshot before forwarding, and adding an encrypted append journal. Periodic-only snapshots minimize request work but cannot guarantee recovery of model-visible mappings. A journal could lower large-vault write cost but introduces a new format, replay and compaction lifecycle. Choose a serialized pre-forward snapshot checkpoint if the regression confirms the gap: retain the existing encrypted file format and coalesce concurrent checkpoints using the vault change counter. Record this persistence change in ADR 0004 and measure its cost. Do not claim a performance qualification from a correctness fix.

File-mode requests must not reach the upstream until all mappings created before their forwarding boundary are included in a successful snapshot. Failed persistence refuses forwarding with a value-free service-unavailable error. Cancellation before the checkpoint must not start forwarding. Concurrent requests, periodic saves and final shutdown saves share serialization; a failed attempt never advances the saved counter. Memory mode remains ephemeral and does not acquire disk persistence. Reload keeps the shared vault and its durability boundary.

Snapshot success must include successful file sync, atomic rename, and parent-directory sync. Errors after rename still report failure and are retried; the existing snapshot may already be newer, but forwarding remains refused until persistence succeeds. This is a process-crash/filesystem contract, not a guarantee against faulty storage, lost keys, power-loss behavior outside filesystem guarantees, or previously lost mappings from older versions.

Do not add mapping expiry, eviction, per-agent vaults, or automatic key rotation. Active and resumable histories can refer to old mappings indefinitely. Cooperating agents share one proxy/vault; unrelated trust domains use separate proxy instances and keys. Existing lifetime file locking must continue to reject multiple writers. Explicit retirement requires draining agents and deciding that histories will no longer resume; deleting a vault is not routine cleanup.

## Acceptance and measurement

Prove the pre-forward on-disk boundary, persistence-failure refusal/retry, graceful restart, abrupt termination/restart, exact restoration without reassignment, coherent concurrent reload, and locking. Exercise four concurrent callers with growth to 10,000 mappings and reopen every mapping exactly; measure allocation/save/reopen time and encrypted file size at 1,000 and 10,000 mappings. Record the absence of a finite retention bound and full-snapshot write cost. Tests and diagnostics use fabricated values and private captured output only.

Run affected and full race tests, workflow tests, lint/staticcheck, HTTP corpus and manual verification. One fresh whole-branch review and required remote CI/security gates precede merge. Task 08 performance and Tasks 11–12 qualification/release gates remain open. If snapshot checkpoint cost is unacceptable, keep the safety regression and evidence; revisit the journal in a separate ADR rather than silently weakening persistence.
