# Task 11 optimized-binary cancellation evidence

## Intent and constraints

Close the workload contract's distinct queue/cancellation gate with retained evidence from the optimized reference binary: queued work cancels within 1 second, active extraction cancellation releases its slot within 2 seconds, and each harness operation remains bounded by 45 seconds. Race tests remain correctness evidence. Preserve the normal workload, fail-closed policy, privacy, source identity and all failed attempts. Never run this diagnostic alongside a reference measurement or modify the user installation.

## Approach

The chosen approach uses an isolated OCR launcher that delegates to the real extractor except while a private gate is armed. Two requests occupy the existing two extractor slots; another request waits in the actual queue. Existing extractor gauges establish these states. A private gate is preferable to hoping a large image runs long enough: uncontrolled OCR duration cannot reliably distinguish queueing from active work. Existing race tests are valuable but explicitly cannot establish a performance gate.

This is an implementation choice under the owner's standing autonomous authorization. No production concurrency, policy, extractor environment or request limits change. The wrapper has an absolute interpreter/delegate path, bounded stdin, private files, and replaces itself with the real extractor after restoring stdin. Version probes delegate immediately. It must not spawn an untracked subprocess or inherit credentials into the helper.

## Measurement and proof

Reuse the normal corpus, seeded vault, four-client warmup (at least 60 seconds and 100 successes), scenario executor, upstream receipt oracle, audit/privacy checks and owned-process cleanup from the soak harness. Run three independent cancellation trials with distinct image variants. The existing wrapper launch environment is isolated to the owned proxy. Baseline and post-fault images must complete through the real extractor with semantic, boundary and upstream receipt proofs.

Observe the first and second gated helpers in sequence to bind their PIDs to the corresponding request. Admit a queued image and one concurrent text request; require extractor-active=2 and extractor-queued=1 before cancellation. Cancel the queued connection and measure through the observation that queued=0 while active remains 2. Include transport cancellation completion; oracle polling is separate and must not inflate or hide transport timing. The queued request must never start a helper or reach upstream.

For active cancellation, place a fresh image in the queue, close one identified active request, and require its helper to terminate and the waiting image to acquire the released slot within 2 seconds. Keep the other active helper held until this evidence is captured. Then release the private gate and prove the remaining image requests succeed. An aggregate active-count decrease alone is insufficient: the identified old helper and new queued request's admission establish slot reuse.

The diagnostic records monotonic intervals, numeric gauges, opaque request IDs, bounded helper identities, outcomes and receipt/audit proofs. It retains observed failure attribution; scheduled cancellation must not relabel an unrelated failure as success. Reject missing/unavailable gauges, premature responses, ambiguous helper binding, non-finite timing, stale/symlinked markers, unverified cancellation and leaked processes. Matching request IDs bind audit decisions. Keep full logs and request details private.

## Integration boundary

Use a narrowly scoped diagnostic hook around the existing collector's first measured phase, after its original warmup. The hook runs the cancellation trials and intentionally ends that diagnostic campaign before ordinary measured phases. The collector root remains explicitly incomplete; a separate bound cancellation verification artifact is the only resulting claim. It never sets overall performance qualification or substitutes for the original full soak.

Instrument only owned proxy connections inside the isolated diagnostic process, with thread-local request ownership and restoration in a finally block. Preserve the existing request and scenario oracles. Cleanup restores all instrumentation and the private gate, closes connections, joins workers, shuts down owned services, and checks audit/log privacy even after failure.

## Pre-response audit binding

A cancelled extraction may never send response headers, so its client cannot rely on receiving the server request ID. Snapshot audit IDs before each cancellation, keep all other image helpers held, and require exactly one newly completed blocked audit record in the cancellation interval. Concurrent successful text records are separately verified. Retain that uniquely attributable record's request ID and reject multiple or unexplained new blocked records. This controlled differential binding replaces an impossible response-header lookup; it must not choose the first record from an ambiguous set.

Gate marker fields are pid, generation, started_ns and input_sha256. The marker establishes readiness, not process ownership. The driver must capture/validate the owned proxy parent and process start identity while each helper is held, then use that identity when checking termination or PID reuse. The private wrapper bounds stdin at 80 MiB, covering the extractor's maximum normalized image size, and never emits input bytes or marker metadata on stdout.
