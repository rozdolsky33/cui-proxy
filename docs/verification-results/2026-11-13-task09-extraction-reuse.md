# Task 09 extraction reuse evidence — 2026-11-13

Status: integrated through [PR #43](https://github.com/rozdolsky33/cui-proxy/pull/43), squash `5509f3a`, after independent review with no findings and all CI/security/packaging gates passed. Task 08 remains open; sustained qualification is not claimed.

## Method and provenance

Reference: unchanged main `046f40b00565c72c89b12571f5a41439e6c4fbed`. Candidate production source: `d7178a38e5a7808782c5e8acb9dc29f44c983fbd`. The candidate includes the regular-file/nonblocking fingerprint hardening. Later documentation/manual-only changes do not alter the measured runtime code. Private source/binary/runner hashes are retained in `/private/tmp/cui-task09-hardened-manifest.json`.

Candidate binary SHA-256: `eff66517ebb49a2c383f31e25d68fc6c7883b7b69acf76289276d47e8213bd94`.

Local macOS experiment, four clients, 20 measured image requests per phase, four warm-up requests, three repetitions per arm and pattern; reference/candidate order alternates between repetitions. All 12 phases completed, 240 measured requests succeeded, and no measured requests failed. Each repeated phase used one image across its 24 warm-up/measured invocations. Each varied phase used 24 distinct images without warm-up overlap. The same pre-generated corpus and runner were used for both binaries. No competing build/test campaign ran during timing.

The table pools 60 measured requests per cell; p95 is nearest rank. Extraction measures validation, helper identity hashing, cache lookup and output handling, excluding helper queue wait. First-byte and completion timings also include synthetic upstream streaming. RSS columns are maximum periodic samples across repetitions, in bytes, not hard process-memory bounds or exhaustive child peaks.

| Pattern | Arm | Extraction p95 ms | First byte p95 ms | Completion p95 ms | Cache hits | Proxy RSS bytes | Helper RSS bytes |
|---|---|---:|---:|---:|---:|---:|---:|
| repeated | reference | 204.190 | 1490.443 | 2356.942 | 0/60 | 53,149,696 | 92,110,848 |
| repeated | candidate | 1.045 | 1167.300 | 2071.032 | 60/60 | 55,951,360 | 0 |
| varied | reference | 193.891 | 1490.792 | 2360.647 | 0/60 | 55,951,360 | 54,689,792 |
| varied | candidate | 209.859 | 1489.589 | 2366.059 | 0/60 | 53,657,600 | 15,433,728 |

Repeated requests avoid measured helper work after warm-up; the cache-hit counter independently establishes reuse. Unique-image controls have no hits. Their pooled extraction p95 was 209.859 ms for the candidate versus 193.891 ms for the reference (about 8.2% higher); candidate repetition p95 values were 215.517, 194.423 and 193.876 ms. This records cold-path cost and observed variation rather than claiming an improvement for unique images. Differences between unique-image timings are run-to-run observations, not evidence that caching makes cold OCR faster. This experiment demonstrates repeated-image benefit, not a production image-reuse frequency or sustained throughput guarantee.

## Mixed text diagnostic — before final fingerprint hardening

A separate paired bypass/proxy diagnostic used the candidate before the regular-file/nonblocking fingerprint hardening, one repetition with 20 text requests per size/arm and concurrent varied images. It completed 467 total text/image requests successfully. It is retained as pre-hardening evidence, not a measurement of the final binary. The final extraction campaign and full correctness gates were repeated after hardening; this already-failing mixed qualification was not repeated solely for file-open flags.

| Text KiB | Added first-byte p95 ms | Added first-byte p99 ms |
|---:|---:|---:|
| 256 | 126.057 | 151.728 |
| 768 | 170.818 | 182.280 |
| 1024 | 159.068 | 160.246 |

The original 150 ms added-p95 target still fails at 768 KiB and 1 MiB. Task 08 therefore remains open. Varied images do not generally benefit from short-lived reuse; this change does not resolve that workload's fairness bottleneck. A full matched-load rerun and sustained acceptance remain required.

## Correctness and gates

The final runtime passed all 23 Go packages under the race detector, 277 Python workflow tests (four explicit opt-in skips), go vet, staticcheck and the 125-operation HTTP corpus. The bundled manual was rebuilt and verified. Regression coverage includes PNG/JPEG reuse, current policy changing from masking to blocking on a hit without forwarding, exact count/decoded-byte/raw-output/text limits, helper identity changes, FIFO paths, expiry/eviction/buffer ownership, concurrent cold requests and cancellation/failure isolation.

Observed RED→GREEN checks cover the cache primitive and identity, integration and current-policy replay, metadata collectors, and a FIFO that previously blocked fingerprint opening. Cache hits record known zero helper wait without weakening required-stage validation. Numeric hit metadata excludes image fingerprints and content. An isolated workflow test needed its new metadata reader mocked consistently; production validation was retained.

Private evidence: `/private/tmp/cui-task09-hardened-*.json`, corresponding captured logs, `/private/tmp/cui-task09-extraction-hardened-status.json`, `/private/tmp/cui-task09-v3-gates.json` and `/private/tmp/cui-task09-mixed-final.json`. Fixture identities were compared locally; only counts are reported. Earlier failed/incomplete instrumentation campaigns and the pre-hardening campaign are preserved separately and excluded from the final extraction table.

## Retention, limits and rollback

See [extraction reuse](../extraction-reuse.md). The process-local cache retains at most 32 successful inline-image OCR outputs, 4 MiB total, for 30 seconds without refreshing expiry on hits. Raw OCR can contain sensitive plaintext. Eviction/expiry clears owned buffers but does not guarantee secure erasure or total RSS. PDFs are uncached. Current request limits and current sanitization always run. Missing/unsafe/unfingerprintable helpers disable reuse; library/language-data updates require restart for immediate invalidation. Concurrent cold misses remain independent through the bounded helper gate.

Rollback to the preceding release removes reuse; restart clears process-local cache state. No vault migration or cache persistence is introduced. Remaining work includes Task 08 text fairness, Task 10 mapping recovery, sustained acceptance and release/regression gates.
