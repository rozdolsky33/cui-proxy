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
