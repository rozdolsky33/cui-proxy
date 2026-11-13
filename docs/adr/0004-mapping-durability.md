# ADR 0004: Persist file-vault mappings before forwarding

- Status: Accepted for implementation; verification and integration in progress.
- Date: 2026-11-13
- Related: Task 10 mapping lifetime and recovery.

## Context

A live regression held the upstream response open and reopened the vault before periodic save or shutdown. The proxy had already forwarded a synthetic, but no readable snapshot contained its mapping. The default 30-second save timer therefore permits model-visible mapping loss on process termination. Slow or failed saves can extend the loss window. Reopening only an older snapshot cannot reconstruct all collision assignments from the key: assignment also depends on occupied candidates.

## Decision

In file mode, checkpoint the shared vault before any request is forwarded upstream. Serialize this checkpoint with periodic and shutdown saves. Compare the change counter under that serialization, and advance the saved counter only after successful persistence. Concurrent checkpoints can reuse a completed checkpoint when no additional change needs saving.

Retain the existing AES-GCM snapshot format, atomic temporary-file/rename workflow and lifetime writer lock. Successful saving requires file sync and parent-directory sync; propagate directory-sync errors rather than hiding them. A failed checkpoint returns a value-free HTTP 503 `vault` refusal with storage-repair guidance. It cannot be converted to sanitization passthrough. Preserve existing in-memory mappings and retry after storage recovers.

Keep periodic and shutdown saves as backstops for changes not yet exposed by forwarding. Keep memory mode explicitly ephemeral. Cooperating agents use one proxy and shared vault; do not split mappings per agent, evict mappings, or rotate keys while histories can resume.

## Alternatives and tradeoffs

Periodic-only saves avoid disk work on the request path but cannot meet recovery guarantees for model-visible mappings. An encrypted append journal could reduce large-vault write amplification, but adds a new format and replay/compaction lifecycle; defer it until checkpoint measurements justify that complexity. Full snapshot checkpoints reuse the proven format and make the boundary explicit, at the cost of serialization, filesystem latency, temporary memory and writes proportional to vault size. A request that creates no new mapping normally skips saving.

The guarantee concerns successful filesystem operations and process crashes, not faulty storage or arbitrary power-loss behavior. A filesystem sync already in progress is not cancellable; cancellation prevents forwarding afterward. Failed directory sync can occur after rename, so an error does not imply the old snapshot remains unchanged. Existing lost mappings from older releases cannot be recovered automatically; do not assume stale histories are safe merely because startup succeeds.

## Verification

Task 10 requires pre-forward snapshot inspection, failure/refusal/retry, metadata-only routes, cancellation, concurrent checkpoints, crash/restart exact restoration, lifetime locking and growth measurements. Record costs at 1,000 and 10,000 mappings and preserve the open mixed-text performance gate. No sustained qualification follows from this ADR.
