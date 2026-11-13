# Task 09 Extraction Reuse Implementation Plan

> Use superpowers:executing-plans inline, with one fresh whole-branch review.

**Goal:** measurably reduce repeated image OCR work with bounded retention and unchanged policy checks.
**Architecture:** process-local LRU of validated raw image OCR success output, keyed by private content/helper/options identity. Independent misses use existing helper admission; hits still charge per-request budgets and reenter current sanitization.
**Tech Stack:** Go 1.26 standard library; existing Python workflow harness.
**Spec:** docs/superpowers/specs/2026-11-13-task09-extraction-reuse.md

## Global Constraints
32 entries, 4 MiB retained output, 30-second expiry, 2 MiB per output; no persisted cache or retained images. Clear retained byte buffers on removal without claiming secure erasure. Fingerprint helper content only up to 32 MiB; inability to fingerprint means uncached execution. PDFs remain uncached. Preserve 8/count, 10 MiB decoded-input, 2 MiB text, pixel and deadline limits; current policy always runs. Capture fixture/config/runner/report/test output privately. Primary checkout remains clean current main.

## Review Focus
1. A cache hit cannot skip current blocking rules or weaken logical/raw output budgets.
2. Helper replacement, path/options changes or missing helpers cannot reuse the wrong result.
3. Cancelled/failed callers cannot publish results that poison independent requests.
4. Concurrent insert/get/expiry cannot exceed entry/byte bounds, mutate returned data, leak timers or retain removed buffers.
5. The measured workload distinguishes repeated-image benefit from unique-image cost; Task 08 and sustained qualification remain honest.

### Task 1: Bounded success cache and private identity
Files: internal/attachment/cache.go, cache_test.go.
Interfaces: newTextCache(entries, bytes int, ttl time.Duration) *textCache; get(key [32]byte) ([]byte,bool); put(key [32]byte, data []byte); close(). imageCacheKey(input []byte, format, path string, args, environment []string) ([32]byte,bool).
- [x] Add tests for LRU/entry/byte bounds, owned copies, expiry without access, concurrent use and close; observe RED.
- [x] Implement mutex-protected LRU, bounded expiry timers and buffer clearing; race tests GREEN.
- [x] Add identity tests for input/format/path/executable/options/environment changes and unreadable/oversized executable fallback; observe RED then implement private hashes; tests GREEN.
- [x] Commit after targeted race verification.

### Task 2: Image integration and policy/budget proof
Files: internal/attachment/extract.go, reuse_test.go; internal/adapter/codex or proxy integration tests.
Interfaces: Extractor holds a cache pointer; New(ctx) uses the shared default, tests may construct an uncached extractor. Only validated inline image OCR uses the cache. helperEnvironment() returns the same fresh fixed environment used for execution and identity.
- [x] Add counting-helper tests: repeated images hit; different inputs/identity miss; raw/text/count/byte limits still apply; errors/cancellation never populate; concurrent cold calls remain independent. Observe RED.
- [x] Integrate lookup after existing image validation and store only after successful validation/budget/cancellation checks; preserve PDF path. Tests GREEN under race detector.
- [x] Add the HTTP/current-policy reload regression before integration: require helper reuse and refusal under newly active rules with no original forwarding. Observe RED for missing reuse, then GREEN with both reuse and current-policy assertions satisfied.
- [x] Run affected attachment/adapter/proxy race suites, then commit.

### Task 3: Measurement, docs and integration
Files: docs/extraction-reuse.md, manual/navigation, verification results and roadmap; internal/observe and internal/metrics cache-hit metadata; extraction_performance.py/performance_load.py/performance.py collectors and tests.
- [x] Measure exact image reuse patterns and compare repeated and unique image runs against unchanged main; capture private evidence and report aggregate latency/helper work/RSS.
- [x] Retest mixed text fairness with the original target; document retained plaintext lifetime, helper-upgrade invalidation limits and evidence limitations.
- [x] Run full Go race, Python workflow, vet/staticcheck, HTTP corpus and manual verification; all exited zero before the frozen campaign. Re-run affected gates if further code changes occur.
- [ ] Fresh whole-branch review, one RED/GREEN fix pass for material findings; archive all rulings and defer minors.
- [ ] Push/PR, require all CI/security gates on exact head, merge and synchronize clean main.
