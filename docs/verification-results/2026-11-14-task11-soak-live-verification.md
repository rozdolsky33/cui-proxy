# Soak harness live verification

Status: collector and diagnostic checks verified; the original four-hour proxy soak and matched bypass remain unqualified.

## Source and regression checks

The sampler integration fix is committed as `6caed76`. The full workflow suite passed 714 tests, with 4 skips; 23 focused resource and campaign tests passed. The fresh branch review found no confirmed code defects but required the full suite and original-duration live warmup before integration. Live validation subsequently found and fixed the resource sampler connection: the monitor received a function instead of a sample. A regression using the real sampler factory reproduced the failure before the fix.

## Original-duration warmup diagnostic

A fresh run completed 116 successful operations, including 24 images, over 61.708 seconds. The warmup verifier checked the original duration/count requirements and image evidence. Two resource observations passed the resource-series validator, and no owned processes remained. The driver intentionally stopped before a measured phase, so this is not sustained-load evidence.

The failed first warmup remains retained. It exposed unavailable resource samples rather than being relabeled as a successful observation.

## Missing-extractor recovery diagnostic

An isolated private OCR launcher was temporarily removed while three text requests were active. Four baseline image requests succeeded. The faulted image returned HTTP 422; its matching audit request ID recorded a blocked sanitization decision with `attachment_image_missing_tool`. The upstream receipt oracle verified absence. All three overlapping text requests succeeded. Restoring the launcher allowed four further image requests to succeed, and dependency preflight recovered.

The diagnostic checked 23 audit records, privacy, and shutdown of both owned service processes. Earlier failed diagnostic attempts remain retained. Corrections aligned the probe with the real dispatcher, dashboard port, ten-second preflight cache, structured operation failure, and request-ID-bound audit evidence. These probe corrections did not change production policy or weaken its failure behavior.

## Retained evidence

- Warmup verification: `cui-soak-full-warmup-2.verification.json`, SHA-256 `8963dceef9c2e1d16466b4b2adb1518bf59dc90ff77d75e43b235c6f5c8fe2ed`.
- Missing-extractor verification: `cui-soak-missing-tool-6.verification.json`, SHA-256 `42b959470718ed3d3c77e76283c1c7a6896c1a3f5d51d447f87e742717eb2f4b`.

Detailed evidence and logs remain private. Both verification artifacts explicitly retain `performance_qualified: false`. The original fairness matrix, throughput, extraction, cancellation, stress, resource-growth, recovery and full soak requirements remain separate gates. These diagnostics do not close roadmap Tasks 08, 11, or 12.
