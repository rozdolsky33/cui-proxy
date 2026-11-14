# Task 11 ASCII folded detector prefilter

## Required behavior
Preserve every existing detector match, ordering, policy decision, panic
boundary, worker bound and Unicode/invalid-UTF-8 behavior. The prefilter may
only prove a match impossible; it never provides matches or replacements.
No request cache, persisted text, new logging, policy option or global queue.

## Derivation and proof boundary
Parse the existing compiled regex with the same Go syntax flags. For literal
nodes, map each rune to its lowercase ASCII representative if one exists in
its permitted simple-fold cycle; leave non-ASCII-only runes non-ASCII. Clear
folding only in this private analysis tree. Reuse the existing conservative
necessary-literal derivation (required concatenation members, mandatory
repetitions, common alternate substrings, bounded64-byte literal metadata).
A literal with no ASCII representative cannot occur in ASCII input. If that
literal separates other ASCII portions, any derived portion remains necessary.
Optional branches without a common required substring yield no prefilter.
Anchored and already literal-prefix-optimized regexes remain excluded.

At request time, inputs smaller than4,096 bytes and every input containing a
non-ASCII byte (including invalid UTF-8) use existing detection unchanged.
For eligible larger input, prepare lowercase ASCII text once per Scan. A
compiled owned regex can be skipped only if its derived folded substring is
longer than its existing ordinary required substring and is absent. Dictionary
regexes use the same derivation and absence test; their original whole-entry
boundary and shorter-entry fallback logic runs unchanged whenever not skipped. Blocking
wrappers may expose this metadata but keep their normal Find method whenever
not skipped. Unknown detector implementations never opt in.

## Verification and acceptance
Require semantic regression, original detector suite, race checks, generated
necessary-condition fuzzing, exact parallel/sequential matching and blocking
parity. Include Kelvin sign/long s folding, optional branches, alternatives,
non-ASCII and invalid UTF-8 fallback. Capture outputs privately.
Measure fixed and rotating full requests with identical fixtures and alternating
binary order, followed by matched real mixed-load diagnostics. Preserve added
allocation costs and reject regressions. Diagnostic improvements do not close
the full1,000-sample fairness gate or unrelated workload gates. Complete ordinary
correctness/security CI and one independent whole-branch review before merge.
