# Live OCR removal and recovery — 2026-11-23

The real dependency recovery check passes on the frozen macOS arm64 candidate.
It covers the separate live missing-extractor requirement in the
[Task 11 specification](../superpowers/specs/2026-11-14-task11-soak-harness.md).
The sustained soak, reference campaign, capacity observations, and release
closeout retain their own acceptance gates.

## Exact identities

| Item | Identity |
| --- | --- |
| Candidate source | `c8aaef5799c75bccb19a7121304198507d36649b` |
| Candidate executable SHA-256 | `12d0ad778c61a308838075db1ca6e7646e9cbc6315b38eb1368f26cf88ef551c` |
| Probe producer | `c3c7900123e7bed2dffa34eb10a4a0ad48729f47` |
| Toolchain | Go 1.26.11; native Tesseract and Poppler |

The retained report binds the clean candidate source and executable separately
from the clean probe producer. Its five producer-file digests were independently
checked. The producer checkout remains at that revision; this documentation is
maintained in a separate worktree. Fixture, tool, and capture digests remain in
protected local evidence. No fixture values or request IDs are published here.

## Observed sequence

1. An uncached image succeeds through the real OCR tool and canonical response
   and upstream-body oracles.
2. Only the private Tesseract launcher is removed. The same proxy process reports
   `missing_tool` after its readiness cache expires. The other three private
   extractor launchers remain available.
3. A second, distinct uncached image receives HTTP 422 with error code
   `sanitize`, type `cui_proxy_blocked`, and recovery code
   `attachment_image_missing_tool`. Dependency guidance and the failure's audit
   binding are verified. The request has zero upstream arrivals.
4. All three overlapping text requests preserve their expected client semantics
   and sanitized upstream bodies while the image is rejected.
5. Restoring the private launcher restores processing of the previously rejected
   image in the same proxy process. Cache counters confirm uncached extraction.

Actual extractor binaries and shared symlinks are preserved throughout.

## Retained acceptance

| Check | Result |
| --- | --- |
| Native probe exit / complete evidence | 0 / pass |
| Successful images / text requests | 2 / 3 |
| Canonical upstream receipts | 5 verified |
| Text requests enclosing the rejected image request | 3 |
| Missing-tool request upstream arrivals | 0 |
| Audit outcomes | 5 sanitized, 1 blocked |
| Audit privacy and metadata-only schema | pass |
| Post-shutdown captured logs | 3 checked; 0 leaking files |
| Known-value privacy oracle | 10,006 values checked |
| Private launcher/environment and child cleanup | all checks pass |
| Candidate, producer, and tool identities stable | pass |

The [probe](../../scripts/workflow/ocr_recovery_probe.py) fails closed on missing
identity, canonical receipt, overlap, uncached behavior, audit, shutdown privacy,
or cleanup proof. Its raw failure-response reader preserves request bytes, uses
the existing proxy-disabled opener and `/v1/responses` route, bounds the response,
and closes it. Successful transformations require the exact `sanitized` audit
decision and empty failure metadata.

## Validation and scope

All 851 native Python workflow tests pass, including 21 probe regressions.
Relevant regressions were observed failing before their fixes. Documentation
contract checks and the strict MkDocs build pass with zero warnings. Independent
source and retained-evidence review found no outstanding Critical or Important
findings.

Initial native attempts exposed probe integration errors: a missing private
`pdfinfo` launcher, fixture-specific sending, an incorrect route, and error/audit
oracles that did not match the candidate's wire contract. Those attempts remain
failed; only the complete producer-bound run above is accepted. Candidate Go
behavior and executable bytes were unchanged by these probe corrections.

This is functional extraction and recovery evidence. It does not establish
live-model reasoning quality, full image vision, or completion of the separate
eight-hour soak and 123-job reference campaign.
