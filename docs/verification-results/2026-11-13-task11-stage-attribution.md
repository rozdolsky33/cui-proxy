# Task 11 stage attribution — 2026-11-13

Status: implementation and short diagnostic complete; integrated in [PR #46](https://github.com/rozdolsky33/cui-proxy/pull/46), squash `c4edb8d`, after all local/remote gates and a clean independent review. Task 08/11 qualification remains open.

## Measurement change

A closed `mapping_checkpoint` stage now measures the existing pre-forward persistence callback, including any wait for its serialization lock. The callback still finishes before audit success and upstream forwarding; failure, cancellation and panic behavior are unchanged. A missing callback remains unobserved. Existing bounded metrics and opt-in correlation carry the new numeric stage.

Mixed workload reports now separate numeric stage samples into `text_requests` and `image_requests`. The normal fixture's extraction-stage presence distinguishes the two workload classes; this is not a general production request classifier. Completion records are deduplicated by session pseudonym and completion timestamp, consistent with the collector. Repeated requests in one session are valid. Warmups are excluded, exact measured counts are required, and missing observations remain empty arrays with zero coverage rather than invented zero durations. Identities and timestamps are not copied into the new report section.

The existing report privacy guard retains its raw-content prohibitions. Only seven numeric metadata field names were added to its immutable allowlist. A regression test verifies raw image content is still rejected.

## Short four-client diagnostic

One bypass/proxy pair at 768 KiB used 20 measured text requests per arm with concurrent image requests, a short four-operation warmup and the normal detector/dictionary/vault population. This is diagnostic evidence, not the contract's warmup, sample-size, repetition, throughput or soak acceptance.

All 80 bypass operations and 76 proxy operations succeeded. Proxy attribution covers exactly 20 text and 56 image requests, including checkpoint observations for every request.

| Text stage | p50 | p95 | p99 |
| --- | ---: | ---: | ---: |
| Body inspection | 74.812 ms | 79.867 ms | 83.609 ms |
| Audit write | 4.645 ms | 6.592 ms | 6.592 ms |
| Ingress read | 3.282 ms | 3.801 ms | 10.421 ms |
| Mapping checkpoint | 0.000958 ms | 0.002709 ms | 0.004834 ms |

The vault had 10,000 entries before and after the run. These checkpoint samples measure steady-state unchanged mappings; they do not measure durable writes during mapping growth or imply disk persistence is free. Task 10's separate growth/save evidence remains relevant.

Paired added first-byte latency was p50 137.295 ms, p95 148.665 ms and p99 150.230 ms. The p95 is narrowly below 150 ms in this short sample; no performance improvement or qualification claim follows from it. Earlier short runs exceeded the target. Peak observed proxy RSS was 108.47 MiB, not a sustained memory bound.

The next useful target is body inspection under a representative changing request sequence. This run rules out checkpoint waiting as a material contributor only for its steady-state population. Stage percentiles cannot be added together or treated as causal decomposition of the paired end-to-end percentile.

## Verification and retained failures

The checkpoint test initially used an invalid direct-handler request; after correcting server-side loopback request construction, removing only the timing produced the expected missing-observation failures for all four invoked outcomes. Restored timing passed affected race tests. A fake clock verifies exact independent duration and once-only recording.

Live attribution tests exposed two incorrect assumptions, both fixed with regressions: sessions may repeat across completions, and new numeric report fields need registration in the existing closed schema. Malformed collections, nonfinite/negative/oversized durations, duplicate completions, missing coverage, unknown stages and raw-content output are covered. The live mixed proxy/bypass integration test passes with exact stage coverage assertions.

Full local gates passed: 23 Go race-test packages, 298 Python workflow tests with four explicit opt-in skips, vet/staticcheck, all 125 HTTP corpus operations, and manual build/verification. The opt-in live mixed proxy/bypass integration test also passed separately. Independent review found no issues and independently passed affected race tests, all 298 workflow tests and randomized accounting checks. Exact-head Linux/macOS, packaging, documentation and security checks passed before merge. Private captured evidence is under `/private/tmp/cui-task11-stage-*`, including the diagnostic report, numeric summary, RED/GREEN logs and gate manifest. Only aggregate metadata is published here.
