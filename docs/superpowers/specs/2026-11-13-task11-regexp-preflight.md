# Task 11 preflight: bounded regexp candidate search

## Evidence and intent

Task 08's mixed-text p95 target remains open. The retained CPU profile attributes most sampled work to regexp execution. A three-run per-detector diagnostic on the existing normal fixture (50 regex declarations, 1,000 dictionary entries, 10,000 mappings) shows many custom regex scans taking about 4 ms each; 30 custom patterns begin with a word boundary and have finite width. The previously attempted capture-index API substitution was rejected in Task 07 and will not be repeated.

The owner authorized autonomous roadmap execution and opted out of routine design approvals. This is a focused optimization experiment before sustained acceptance, not a replacement for the four-hour soak or its numerical gates.

## Decision and alternatives

Keep the reference regexp matcher as the semantic authority. Compare unchanged whole-input search with a bounded candidate search for eligible custom regex detectors. A shared ASCII case-folded variant was considered but the measured custom patterns are case-sensitive; a dictionary-only optimization addresses a smaller share of cost. Choose prefix candidate discovery for finite-width, leading-word-boundary patterns, with the original pattern validating every candidate.

Eligibility is conservative: custom non-group regex only; source at most 4 KiB; finite positive maximum width at most 256 ASCII bytes; at most 128 syntax nodes and 4,096 compiled instructions; root concatenation begins with word-boundary or non-word-boundary assertions and the remaining pattern has a nonempty ASCII literal prefix. Reject begin/end text or line anchors, unbounded repetition and unsupported syntax. Optional compilation failure means reference fallback, not configuration failure.

Use the optimized search only for ASCII inputs at least 4 KiB long. Discover at most 32 overlapping prefix positions before validating any. A 33rd candidate causes immediate reference fallback. For a candidate at offset zero, validate with a start-anchored copy of the original pattern. Otherwise include the preceding byte and consume it with a noncapturing all-character prefix before the original pattern. Include one trailing context byte beyond the maximum match width when available. Preserve original byte offsets and capture indices. Process candidates in order and skip starts inside an accepted full match, matching the reference non-overlap behavior. Group-based overlapping-secret detection stays on its existing path.

One-byte boundary context is sufficient because the optimized input is ASCII and text/line anchors are excluded. Unicode and invalid UTF-8 always use the reference path. There is no cross-request input cache or changed policy decision. Compiled helper metadata is immutable; per-call candidate storage is bounded. No changes to helper admission, vault durability or audit behavior.

## Acceptance

Exact span and policy equality against the existing reference across captures, validators, boundary positions/newlines/NUL, overlapping prefixes, long matches, dense fallback, short inputs, anchors, unbounded/group patterns, Unicode and invalid UTF-8. Differential fuzzing and race tests must exercise the optimized path, not only fallback.

Measure alternating reference/candidate detection/request-walk microbenchmarks and matched normal text/fairness HTTP diagnostics, retaining all failures and regressions. Do not ship a performance claim unless improvement exceeds paired variation and no material supported control regresses. A greater-than-10% latency or memory regression is material and requires investigation. Short preflight samples do not close Task 08 or Task 11; the original full sample counts, throughput windows and four-hour soak remain required. If the experiment fails acceptance, keep the evidence and reference behavior.
