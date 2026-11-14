# Bounded regex candidate scanning

The full canonical mixed pair misses the unchanged 150 ms added first-byte p95 gate. Reduce unnecessary regex work while preserving every original match, capture, refiner and fail-closed decision. This is an implementation choice under the owner's standing authorization, not a change to qualification criteria.

## Approaches

Per-position slices risk hiding a later match behind an earlier context match and incur many regex invocations. Replacing built-in regexes with handwritten validators increases semantic risk. Select merged candidate windows using the original regexp, supported by the standalone diagnostic; it is generic for conservatively proven finite digit-start patterns.

## Design

Compile immutable metadata from regexp/syntax: ASCII-only consumption, nonnullable, strictly digit-only first consumed characters, finite maximum consumed width at most 256 bytes. Reject unsupported syntax, unbounded repetition, absolute/line anchors and native literal-prefix patterns. Group-mode detectors always keep their existing overlap semantics. Word boundaries can remain because windows preserve a byte of context. Metadata failure means the original regex path.

For input at least 4096 bytes, merge overlapping or adjacent candidate windows spanning one byte before each digit through maximum width plus one byte after it. Run the original regexp in each disjoint window and translate every nonnegative capture offset. Non-ASCII input and excessive candidate coverage (over one eighth of input) fall back to the original whole-input operation. No cached request contents, configuration changes, or changes to refiners/finders. Small inputs keep the existing path.

Correctness argument: every match starts at a candidate and ends within its finite width. Merging prevents duplicate/hidden matches; a window begins with a nondigit context byte or original byte zero, so context cannot introduce a new digit-start match. Its right context is beyond any possible match ending at its last candidate. Disjoint windows cannot contain competing overlapping matches. ASCII context preserves word boundaries. Reject anchors conservatively.

## Verification

Test eligibility, then differential raw submatch indices and final detector spans, including nullable/unbounded/Unicode/invalid UTF-8 fallbacks, boundaries, captures, adjacent windows, overlap, dense inputs, adversarial near matches and existing validators. Fuzz differential results. Benchmark sparse/dense and complete canonical scans; then short and full mixed reference measurements. Preserve every failed result. Production qualification requires the original roadmap matrix, throughput, extraction, stress and four-hour soak, not this microbenchmark. Final whole-branch review and required CI precede merge.
