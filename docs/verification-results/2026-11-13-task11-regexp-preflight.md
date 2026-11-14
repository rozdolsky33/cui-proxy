# Task 11 regexp preflight — 2026-11-13

Status: bounded-search optimization rejected; production behavior restored to main. Integrated in [PR #45](https://github.com/rozdolsky33/cui-proxy/pull/45), squash `2b0d999`, after a fresh review found no issues and all exact-head remote checks passed. This short preflight does not complete Task 08 or Task 11 and does not replace sustained qualification.

## Change under test

The rejected experiment let custom non-group patterns use bounded literal-prefix candidate discovery when their syntax proves finite ASCII width and supplies the necessary boundary context. Original regular expressions validate every candidate. Unsupported expressions, short or non-ASCII inputs and dense candidate sets use the reference implementation. Built-in detector, blocking, audit and vault behavior remain unchanged.

Metadata is capped at 4 KiB source, 128 syntax nodes, 4,096 compiled instructions and 256 consumed ASCII bytes. Each call stores at most 32 candidate positions before choosing the reference fallback. No request text is cached.

## Semantic checks

Eligibility, boundary context, captures, validation and blocking tests were observed failing before implementation and pass afterward. Race tests passed in detection, transformation, Codex adapter and proxy packages. A 20-second differential fuzz run compared full capture offsets against the original regexp engine. Shared helper state also passed a parallel race test.

## Initial measurements

Eight alternating reference/candidate microbenchmark pairs used identical private normal-workload fixtures. Reference source is integrated main `62445ed`; candidate implementation is `2cf428f`.

| Metric | Reference median | Candidate median |
| --- | ---: | ---: |
| Detection | 21.917 ms | 16.112 ms |
| Request walk | 18.537 ms | 18.508 ms |
| Detection allocated bytes/op | 918,476 | 1,028,995 |
| Request-walk allocated bytes/op | 3,278,182 | 3,243,190 |

Detection latency improves about 26%; request-walk latency is effectively unchanged. Detection allocated bytes rise about 12%, exceeding the provisional acceptance threshold. Iteration counts and pool allocation variance differ; equal-work measurements are required to investigate this observation, without discarding these initial results.

Eight control runs show dense-prefix latency about 1.2% higher, non-ASCII fallback about 2.7% higher, and no observed short/unsupported latency regression. Sparse and maximum-width eligible cases improve substantially. The non-ASCII allocation control rises by 43 bytes/op; allocation interpretation remains open.

## Mixed-load rejection

The first complete paired repetition used four clients, 20 measured text requests per size and arm, and concurrent image work. All 465 reference and 468 candidate operations succeeded.

| Payload | Reference added first-byte p95 | Candidate added first-byte p95 |
| --- | ---: | ---: |
| 256 KiB | 107.106 ms | 118.511 ms |
| 768 KiB | 164.153 ms | 186.389 ms |
| 1 MiB | 162.552 ms | 156.921 ms |

Both larger candidate cases miss the 150 ms target, and 768 KiB worsens about 13.5%. The candidate fails the preflight acceptance criterion despite its isolated detection improvement. One short repetition cannot establish a general regression; it is sufficient to reject this attempt under the requirement that every measured repetition pass.

Remaining repetitions were stopped early to avoid spending time on an already-failed acceptance run. The second candidate repetition was interrupted; its partial report and interruption status remain private evidence, separate from the complete pair above. The campaign, runner and owned proxy processes were confirmed stopped. The planned equal-work allocation diagnostic was not run because latency already rejects the change; the initial allocation concern remains unresolved.

The matcher implementation and its tests were removed from the final branch; experiment source remains in historical commit `2cf428f`. The final branch retains the opt-in per-detector diagnostic benchmark, this evidence, plans and Task 10 integration/follow-up bookkeeping. No production performance change is being proposed.

Full local gates passed: 23 Go race-test packages, 281 Python workflow tests with four explicit opt-in skips, vet, staticcheck, all 125 HTTP corpus operations, and manual build/verification. Production detection/proxy/vault/transform diffs against main are empty. Final restored binary hashes are recorded separately from the rejected experimental binary in the private manifest. Independent review found no issues; exact-head Linux/macOS, packaging, documentation and security checks passed before PR #45 merged. Next work must identify the end-to-end mixed-load bottleneck rather than repeat this failed optimization. In particular, the new pre-forward mapping checkpoint currently falls outside the named timing stages; measure that cost explicitly before attributing the unexplained request time to detection. The existing mixed stage summaries combine text and image requests and cannot establish text-only stage causality. Task 08 and Task 11 remain open, including the original latency, throughput, resource and four-hour soak requirements.

Private captured evidence uses `/private/tmp/cui-task11-*`; only aggregate measurements belong in this report.
