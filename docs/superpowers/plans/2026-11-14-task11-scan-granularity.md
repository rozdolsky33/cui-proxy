# Task 11 scan scheduling experiment

Spec: ../specs/2026-11-14-task11-folded-prefilter.md.

Continue the authorized roadmap inline. The complete fresh-control fairness pair
failed the unchanged 150 ms added first-byte p95 gate at 156.154 ms, with all
1000 text and 2669 image operations successful. Retain that failed evidence.

## Hypothesis and scope
Body inspection p95 was 88.562 ms; admission and mapping checkpoints were
negligible. The prior full-request CPU profile attributed substantial sampled
CPU to runtime condition-variable signaling/waiting. This does not alone prove
causality. Test whether launching up to three extra scan workers for 4 KiB
leaves costs more than their parallel detection saves under this workload.
No detector, matching rule, policy, vault, audit or acceptance threshold changes.

## Steps
1. Compare the immutable main benchmark with a diagnostic candidate raising
   only the parallel-scan minimum leaf size to 32 KiB. Use identical fixtures,
   full requests, alternating order and five samples; preserve raw captures.
2. Reject the candidate if it does not improve full-request performance. If it
   does, verify parallel/sequential equivalence, panic and token cleanup, race
   safety and complete request semantics. Add a regression only where needed.
3. Run short matched mixed-load diagnostics before repeating the hour-long
   full acceptance pair. A diagnostic cannot close the acceptance gate.
4. Independent whole-branch review, required checks, merge and current main
   only if evidence supports the change; otherwise retain findings without
   runtime modification. Then resume remaining original qualification gates.

## Review focus
Preserve matching, ordering, fail-closed behavior and global worker bounds.
Do not convert scheduling diagnostics into performance qualification. Keep
failed and rejected results, immutable binary identities and exact provenance.

## Second experiment: ASCII-only folded necessary literals
The threshold experiment regressed median full-request latency by65.351% and
was reverted. Per-detector measurements identify case-insensitive scans among
the costly paths. All40large fixture leaves are ASCII. Derive a necessary
ASCII-folded substring from regex syntax, and prepare lowercase ASCII input
once per scan. Apply only to immutable owned regex detectors with a necessary folded substring longer than its
existing ordinary literal precheck; preserve blocking wrappers and detector
order. Non-ASCII input always uses the original detector. This is a necessary
condition only, never a replacement matcher. Bound metadata to64bytes and
retain no request cache. Tests must establish Unicode folding, invalid UTF-8,
alternation/optional branches, blocking wrappers and exact sequential parity.
First run semantic RED/GREEN tests, then identical full-request comparisons.
Reject an optimization that does not improve those measurements. Final review
must focus on proof that no actual match can be skipped, not only speed.

Extend the same necessary-condition proof to compiled dictionary alternations.
A dictionary regression must fail before adding metadata/skip eligibility and
then pass with exact original Find boundary/shorter-entry semantics. The private
representative fixture has40large fields,32of which lack the dictionary's
15byte necessary folded substring. Measure this final candidate separately;
the regex-only normal-CPU diagnostic improved rotating latency only3.089% and
increased allocated bytes13.082%, so it does not establish the required gate.
