# Task 08 bounded admission and detector scheduling

**Status: integrated in PR #42 after local gates, fresh review and all required CI/security/packaging checks passed. Mixed latency acceptance remains open.**

This increment bounds admitted requests and extraction queues and reduces detector CPU latency. It does not qualify the normal workload or complete roadmap Task 08. The unchanged added first-byte target is p95 ≤150 ms and p99 ≤500 ms.

## Method

Local macOS mixed traffic used three attachment clients and one text client, real extraction fixtures, 256 KiB/768 KiB/1 MiB text bodies, three paired bypass/proxy repetitions, and 20 measured text requests per arm per repetition. Each phase used one warmup count and no time-based warmup. There are 60 paired text samples per size; this is diagnostic evidence, not the ≥1,000-sample sustained release qualification required later. No competing builds or test suites ran during the campaign.

The before arm is the bounded-admission implementation at `037573d`, with sequential detector scanning. The after arm adds at most three shared extra detector workers, nonblocking acquisition, dynamic detector assignment, original result order, and caller-side panic recovery. These are separate campaign comparisons against their own bypass runs, not a randomized concurrent A/B trial.

## Mixed traffic results

| Text size | Before added p95 (ms) | After added p95 (ms) | After added p99 (ms) | p95 target |
|---|---:|---:|---:|---|
| 256 KiB | 145.6 | 125.4 | 143.2 | met |
| 768 KiB | 221.8 | 161.6 | 174.8 | not met |
| 1024 KiB | 274.2 | 171.7 | 175.1 | not met |

All 18 phases completed: 1390 successful requests and 0 failures. Variable attachment counts follow the fixed text-client measurement window; all comparisons retain 60 paired text samples per size.

Observed peak proxy RSS was 112.3 MiB and child RSS 90.2 MiB, below the 512 MiB/1 GiB budgets. Resource sampling is periodic and can miss brief peaks. Input reservations bound admitted bytes, not RSS.

## Interpretation and retained limits

The 768 KiB and 1 MiB pooled p95 results remain above target. The bounds and measured CPU improvement can be integrated after correctness, security, review and CI gates pass, while roadmap Task 08 stays in progress. Repeated-extraction work in Task 09 is a candidate for the remaining contention; it must be measured and does not guarantee the target will pass.

The retained defaults are eight active/eight queued API requests, 64 MiB reserved input, two small-request slots and 8 MiB reserved from bulk traffic, a five-second admission wait, and two active/two queued extraction helpers. Unknown-length bodies reserve the policy maximum. HTTP overload is fail-closed, returns typed 503 with retry guidance, and closes HTTP/1 connections to avoid draining unread slow uploads.

## Exploratory findings

- Serializing whole attachment sanitization did not improve the 1 MiB pilot (265 ms p95); it was removed. A one-client text-only control still measured 281 ms p95, showing intrinsic text processing cost.
- Bounded-width and space-region regex experiments produced insufficient microbenchmark improvement and were removed. Original regex matching semantics remain intact.
- Fixed detector partitions improved the 1 MiB pilot to 182 ms p95. A deterministic regression then demonstrated idle workers stranding detectors behind a slow scan; dynamic assignment passed that regression and measured 166 ms in its pilot. The full campaign above governs conclusions.
- The first saturation invocation requested an unsupported harness concurrency value and stopped before setup; it was corrected to the supported maximum of 16 without changing the harness limit.

## Evidence identity

- `cui-task08-balanced-candidate-proxy` SHA-256 `3ac1bf582430c7c0a5b4b62ada6d1bf3c5b5b0c0154052ad73fb1eff2958218d`.
- `cui-task08-balanced-final.json` SHA-256 `715593d20655d06c8a6aade02e78684c8dde0335455a8886d66ef625e0616638`.
- `cui-task08-fairness-v2.json` SHA-256 `e566d54f5eb3235387b53781f9783328762b3238abcc9e5c9912ff457c61637f`.

Raw fixtures, configurations, captured logs and reports stay private. This document contains only aggregate measurements and verification metadata.

## Bounded saturation run

Two offered rates (16 and 32 requests/second), 10 seconds each, used the runner's supported maximum of 16 outstanding clients with 256 KiB bodies. The generator records unsent work when its own bound is reached; these are not proxy refusals.

| Mode | Offered req/s | Scheduled | Sent | Completed | Proxy rejected | Client unsent |
|---|---:|---:|---:|---:|---:|---:|
| bypass | 16.0 | 160 | 87 | 87 | 0 | 73 |
| proxy | 16.0 | 160 | 56 | 56 | 0 | 104 |
| bypass | 32.0 | 320 | 96 | 96 | 0 | 224 |
| proxy | 32.0 | 320 | 56 | 56 | 0 | 264 |

All sent requests completed successfully. Peak proxy RSS was 73.9 MiB. The observed HTTP-handler peak was 16, including requests waiting for admission; this is distinct from the eight admitted-request slots. Queue-full typed 503, timeout/cancellation release, and no-forwarding behavior are verified by focused limiter and HTTP tests. This short run does not establish sustained throughput qualification.

## Validation history

The first full gate sequence passed 23 Go race packages, 268 Python tests, vet and staticcheck. All 125 HTTP scenarios passed, but the final stage-vocabulary assertion rejected the new admission timing stage. A new admission-observation test reproduced the failure, then passed after extending the strict allowlist by that one known stage; its companion verifies unknown stages remain rejected. The full sequence was restarted, with prior failure evidence retained privately.

The final bounded-worker differential fuzz run completed 41,034 executions with no mismatch against sequential spans and block decisions. The worker budget, unknown/cheap detector fallback, policy-wrapper fallback, panic containment, ordering and uneven-work scheduling have regression coverage.

Final local gates: all 23 Go packages passed `go test -race -json ./...`; all 270 Python workflow tests passed (four opt-in skips); `go vet ./...` and staticcheck with all checks passed; the HTTP corpus passed all 125 scenarios and its final observation assertions. Manual snapshot verification is included in the Go suite.

## Fresh branch review

The independent whole-branch review found no Critical or Important issues across all seven review areas and approved incremental integration subject to CI. One minor was deferred: the historical spec still says round-robin partitions although the implementation and recorded design ruling use dynamic assignment. Sustained qualification, long-duration memory behavior and extraction-reuse effectiveness remain unproven; remote required checks must pass before merge.
