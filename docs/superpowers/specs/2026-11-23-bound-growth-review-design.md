# Bound investigation of positive fixed-state growth

The task 11 specification permits investigation and acceptance of positive
fixed-state memory growth whose numeric limits pass. The current evaluator has
no input for that investigation: any positive growth leaves `growth_review`
incomplete. This extension implements that missing behavior in a new evaluator
revision; it does not change the frozen collector or any collected evidence.

## Constraints

- Preserve both original fixed-resource numeric limits, availability and gap
  requirements, all churn accounting, and every other required gate.
- Missing review plus positive growth remains incomplete. Nonpositive growth
  without a review retains the existing passing path.
- No tests, imports of workflow modules, builds, extraction, profiling, probes,
  or benchmarks run while the ongoing soak owns the global reference lock.
- All source work stays in the isolated evaluator worktree. Neither frozen c8
  source/artifact nor ongoing or sealed evidence is edited.
- No review or acknowledgement for the ongoing run is created during preparation.
  The parent may prepare one only after new completed data and an independent
  investigation are available.

## Approach and trust boundary

An optional `--growth-review` input is read separately from the sealed campaign.
It supplies an explicit independent review and a hash/size-verified investigation
artifact. A boolean acknowledgement, a diagnostic sidecar from an earlier run,
or a record without its investigation cannot close the gate.

The evaluator verifies its own clean source provenance, file integrity,
current-run binding, numeric context, and completeness. External authorship and
independence are explicitly declared attestations. The reviewer's independent
judgment remains a trusted manual input; a hash or a JSON assertion is not proof
that an investigation was performed. Reviews may be supplied by an independently
tasked agent or an operator; neither is falsely described as a human signature.
This extension never synthesizes an acceptance decision from memory statistics
and never creates a review file.

Alternatives considered were accepting an unbound acknowledgement, which cannot
establish which run was investigated, and modifying the sealed collector output,
which would invalidate collection evidence. Both are rejected. A separately
captured, strongly bound input preserves the collection and evaluator boundary.

## Review and investigation schema

Both JSON documents have `schema_version: 1` and a closed `scope`. The review
scope is `fixed_resource_growth_review`; the investigation scope is
`fixed_resource_growth_investigation`.

The shared `bindings` object contains five exact SHA-256 digests:

- `root_events_sha256` and `root_seal_sha256` from the completed campaign reader;
- `campaign_identity_sha256` from canonical encoding of the captured identity;
- `expected_identity_sha256` from canonical encoding of the independent expected
  identity, distinct from the CLI's existing digest of that identity file;
- `fixed_resource_result_sha256` from the exact recomputed resource result.

The review requires `decision: accept_bounded_growth`, a reviewer identity,
`independent_of_collection: true`, and a nonempty explanation of that independence.
Reviewer identity is retained in acceptance output only as a digest. It also
requires producer provenance and an investigation descriptor with a relative
`path`, exact `bytes`, and `sha256`.

The investigation repeats the exact bindings, observed `growth` object and
window medians from the recomputed result. It contains conclusions for both
`rss_median_bytes` and `live_heap_median_bytes`: `bounded_positive_growth` for
positive values, `nonpositive_growth` otherwise. A nonempty analysis summary,
limitations, and producer provenance are mandatory. These narrative fields are
inspected locally and never copied into public acceptance output.

Each external document's `producer` has exactly `kind: manual`, an `identity`,
and a `provenance` description. This honestly describes manually composed
analysis and review; it does not invent a committed identity for an editor or
scratch JSON serializer. Acceptance retains the identity and provenance text
only as digests. Independence is the reviewer's declared relationship to
collection, not a cryptographic property of these fields.

Machine context is separately recomputed by the committed
`growth_review_context(campaign, expected_identity, resource_result)` API. It
emits facts only: bindings, observed growth, and window medians. A parent may use
that committed API after freezing a clean evaluator, then manually author the
investigation and independent judgment. The existing CLI's verified
`evaluator_source` identifies the clean commit and hashes all workflow modules;
review acceptance additionally requires its `soak_growth_review.py` hash to
match the actual imported helper. This is the reproducible machine-context
provenance, separate from declared manual authorship. No unrecorded external
generator is required or claimed.

## Validation and integration

The review and investigation must be regular files outside the campaign root.
The investigation is a relative child of the review directory. Absolute paths,
parent traversal, symlink components, duplicate JSON fields, nonfinite values,
missing/oversized files, changed-during-read files, and digest/size mismatches are
rejected. Limits are 64 KiB for the review and 128 KiB for the investigation.

`evaluate_completed_soak(..., growth_review=None)` preserves legacy callers.
The growth review is evaluated only after other gates are available. It can pass
only when the recomputed fixed-resource gate and every other required gate pass.
It cannot turn a failed limit, missing evidence, unavailable runtime, incomplete
churn accounting, or any other failed gate into a pass.

Acceptance records the review and investigation digests, exact bindings,
reviewer-identity digest, and producer provenance. It does not include narrative,
raw file paths, reviewer name, fixture content, or raw reports. The CLI still
requires a clean evaluator source revision and writes a new output outside the
sealed campaign.

## Verification deferred under the reference lock

Tests will be written before the helper implementation and kept with a
test-only baseline snapshot for later red/green execution. During the current
soak, only source inspection, file preparation, static parsing, and static review
are authorized. The parent will run targeted and full native verification
sequentially after releasing the reference lock, then freeze a clean evaluator
revision. No test is represented as passing before that execution.
