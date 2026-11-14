# Task 11 retained campaign assembly: diagnostic evidence

This is short-run harness verification, not four-hour qualification.

## Build and corpus preflight

The measured bounded-scanner binary was reproduced from its clean original
`5d7112e47063a65efee62f2f326c7060e5df9cf6` checkout into a separate output.
Its SHA-256 matched the measured binary byte-for-byte. The original binary was
not replaced. This supplies source proof for a build without embedded VCS data.
All 802 corpus files (800 images and two PDFs) passed physical hash verification.
The assembled harness checkpoint is `6a53945`.

## First end-to-end smoke

The diagnostic requested 45-second fixed and churn phases, one-second/eight-
success minimum warmup, and one restart/reload each. Warmup completed eleven
requests, the idle GC observation completed, and the fixed phase completed with
no unmet mapping requirements. The churn phase stopped on an unexpected W06
image-request rejection. The incomplete root seal, phase reports, stage records,
logs and private environment were retained. No owned child processes remained.

The audit classified that request as a header-policy rejection. Reconstructing
its metadata locally and running the unchanged configured detectors identified
`X-Benchmark-Receipt-ID` matching `builtin.secret`. This was arbitrary hexadecimal
correlation metadata, not a protected workload value or an extraction failure.
The proxy correctly blocked it under the configured metadata policy.

The harness now encodes the same SHA-256 bits as 64 letters from `g` through `v`.
The mapping is bijective and preserves identifier length. The receipt store
continues to accept archived hexadecimal identifiers. The reconstructed failing
headers with this encoding pass the same detector set; no runtime detection
rule or exemption changed. Unit regressions failed before the encoding change
and passed afterward. A fresh complete live smoke is still required.

## Outstanding

The full duration/window/resource acceptance evaluator, complete live campaign
and deliberate setup/mid-phase failure smokes, independent final branch review,
remaining reference matrix, long soak and final envelope publication remain open.

## Subsequent diagnostics

The second 45-second smoke completed both phases without request failures, but
correctly failed the control gate: the deterministic W10 continuation first
appears at client serial59, too late for this short phase to exercise a restart.
The restart requirement was retained and the next smoke used150-second phases.

That third smoke completed316 fixed and308 churn outcomes with no unmet mapping
requirements and exercised one restart and one reload. Finalization then exposed
three harness integration defects: resource samples are an integer counter rather
than a collection; initial policy names had not been tagged generation0 for the
reload evidence verifier; and seed pairs are(class,original), so adding both pair
fields to the privacy oracle incorrectly treated the class label as protected
text. Each defect received a failing regression before correction. Rule patterns
and the actual protected-value set remain unchanged.

A separate offline diagnostic normalized only the missing initial labels in
memory and reconciled all635 audit records. The original retained logs passed
the corrected privacy oracle. Original audit files and the incomplete campaign
seal were not rewritten, and this diagnostic does not complete that campaign.

Deliberate live setup and mid-phase failure checks also passed: spawned children
stopped, owned logs closed, roots remained incomplete, and no later phase began.
The checks retained their private evidence and remain duration-unqualified.

## Fourth retained smoke: report serialization

The fourth smoke completed both 150-second proxy phases, restart/reload controls, and post-load observations, then stopped before bypass finalization could be recorded. Its root remains incomplete. Offline replay of the retained audit and privacy checks reconciled 596 outcomes. The policy coverage report used integer generation keys, which the metadata-only evidence writer correctly rejected. The report now encodes generation labels as strings while preserving counts and strict writer validation. A regression sends the real policy coverage report through CampaignEvidence save/load; it failed on the original code and passes with the correction. Offline serialization of the retained finalization report also passed. These diagnostics do not qualify the short run or repair its original completion seal.

## Fifth retained smoke: direct-boundary warmup contract

The fifth run completed both proxy phases and verified proxy finalization for 594 outcomes. Bypass warmup then stopped after four successful direct requests: response semantics and exact upstream receipts were verified, but the executor omitted the boundary-verification flag required by warmup. The bypass executor now sets that flag only after successful exact receipt verification; proxy mapping boundaries remain subject to their separate sanitization checks. A direct-executor/warmup integration regression reproduced the failure and passes after the correction. The original root stays incomplete and unqualified.

A separate timing diagnostic found four extraction records absent immediately before a restart in the fourth run. Those image requests had completed before the restart; their metrics had not yet been polled. The harness now snapshots completed stages synchronously before stopping the proxy, serializes that snapshot with background polling, and aborts the restart if the snapshot fails. Control reports retain their monotonic clock origin for exact fault-window alignment. This changes evidence collection only; the frozen runtime binary is unchanged.
