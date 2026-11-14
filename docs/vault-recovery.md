# Mapping lifetime and recovery

Mappings connect a synthetic shown to a model with the exact original needed by tools and replies. Cooperating agents and delegated children should use the same running proxy and vault. Unrelated workspaces or trust domains should use separate proxy instances and keys. A file vault has one lifetime writer lock: starting a second writer against the same vault is refused, including while the first writer is saving. Do not remove the lock file to bypass that refusal.

## File and memory modes

File mode checkpoints mappings before forwarding each request. A successful checkpoint includes the encrypted snapshot, file sync, atomic rename and parent-directory sync. Requests that introduce no changes normally reuse the last checkpoint. Concurrent requests, periodic saves and shutdown saves share serialization; a completed checkpoint includes every mapping required by that request. Structured Codex workspace metadata uses this same boundary.

If saving fails, the request is refused with HTTP 503, reason `vault`, and recovery category `dependency` / retry `after_repair`. No upstream request is made. Repair disk space, path availability or permissions and retry; keep the existing vault and key. The service retains mappings in memory and does not mark the failed save as complete. A failure after rename may already have replaced the file, so failure does not mean the old snapshot remains unchanged. Check safe log kinds and use the request ID with `cui-proxy explain`; health alone is not proof that new mappings can be persisted.

Periodic saves still run every 30 seconds for unforwarded changes, and graceful shutdown attempts a final save. The timer is not a hard maximum loss window for unforwarded mappings: slow or failing storage can extend it. Cancellation prevents forwarding, but cannot interrupt a filesystem sync already in progress.

Memory mode is deliberately ephemeral: its key and mappings are lost when the process exits. It is unsuitable for resuming model histories after restart. Do not switch a resumable workflow to memory mode as a storage-error workaround.

## Restart and abrupt termination

With file mode, successfully exposed mappings are checkpointed before the model sees them. A process crash can therefore reopen those mappings and restore the same original tool arguments. This guarantee depends on the filesystem honoring successful operations and on retaining the correct vault and key. It does not promise recovery from lost keys, arbitrary storage corruption, faulty hardware or every power-loss scenario.

Older versions used periodic/shutdown saves only. Their model-visible loss window was nominally up to one save interval plus save time, and unbounded during repeated save failures. A key alone does not reconstruct lost collision assignments. This release cannot recover mappings already lost before upgrade: retire affected histories and start fresh after diagnosing missing state, rather than assuming a successful startup makes old histories safe.

For graceful restart, stop new work, allow active requests to drain, check shutdown/save results, and restart with the same vault and key. If startup reports wrong key, corruption, unsupported format or another writer, resolve that condition; do not delete the vault to make startup pass. Keep backups of vault and key together under appropriate access controls. Restoring an older backup can lose mappings from newer histories, so those histories must not be resumed blindly.

## Retention and capacity

Mappings have no automatic expiry or eviction. Active, delegated and resumable histories may refer to them indefinitely; a per-agent vault or age-only eviction can break restoration. Explicitly retire a vault only after all cooperating agents are drained and its histories will no longer resume. Key rotation or migration needs a separate lifecycle design; neither happens automatically.

Memory grows with distinct mappings and reserved originals; disk grows with persisted mappings and safety flags. There is no total vault-memory cap, retention-duration guarantee or automatic compaction policy. Full snapshots write data proportional to vault size and use temporary plaintext/encoded buffers. A short save interval is not a substitute for capacity planning. Monitor vault-size metrics and save failures; test the workload and storage you will actually use.

The [Task 10 evidence](https://github.com/rozdolsky33/cui-proxy/blob/main/docs/verification-results/2026-11-13-task10-vault-recovery.md) records exact-recovery checks and costs at 1,000 and 10,000 mappings. These are local diagnostics, not sustained four-agent qualification. The [durability ADR](https://github.com/rozdolsky33/cui-proxy/blob/main/docs/adr/0004-mapping-durability.md) explains why snapshots were retained instead of introducing a journal immediately.
