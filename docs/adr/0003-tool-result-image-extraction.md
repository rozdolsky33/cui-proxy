# ADR 0003: Extract text from images in Codex tool results

- **Status:** Accepted
- **Date:** 2026-11-10
- **Deciders:** project maintainer
- **Related:** [ADR 0001](0001-govsec-policy-enforcement.md), `internal/adapter/codex/attachments.go`, `internal/attachment/extract.go`

The maintainer approved the recommended approach on 2026-11-10.
Implementation extends the existing extraction policy to function/custom
tool-result images; it does not enable raw image forwarding.

## Context

The Codex adapter supports local attachment extraction under
`policy.attachments: extract_text`. Its request walker currently permits
conversion only for a direct content part of an unambiguous user message.
Images in tool results therefore fail the placement check, even when their
inline image representation would otherwise be supported.

The investigated failure consisted of six HTTP 422 responses with
`reason=sanitize`, `kind=bad_body`, and `detail=attachment_image_shape`.
The matching local session recorded two `input_image` parts in a
`custom_tool_call_output` immediately before the failures. Those parts used
the expected image fields and a supported detail value. The current placement
restriction explains the refusal. Repeated requests retained the same image
history, so retrying did not resolve it.

Session records establish the client-side context, but are not a complete
capture of the HTTP request. Implementation must verify the actual wire
representation before defining accepted tool-output envelopes. No request
payloads or image data are included in this public ADR.

## Goals and constraints

- Let supported tool-result images participate in the existing local
  extraction and sanitization policy.
- Preserve fail-closed behavior, blocking rules, strict-profile behavior,
  and value-free logging.
- Preserve tool-call identity, output ordering, and neighboring text.
- Forward sanitized extracted text only; never forward the original image,
  its data URL, or uninspected attachment metadata.
- Keep the existing attachment default of `block` and existing user-attachment
  behavior.

This proposal does not add upstream image forwarding, visual redaction,
remote fetching, new image formats, arbitrary nested attachment discovery,
or general support for computer-use screenshots and every multimodal item.
OCR loses layout and visual information and can miss text. Successful OCR
does not prove that every sensitive mark was recognized. This proposal
inherits the existing extraction policy's limits; it does not provide visual
comprehension or a guarantee of complete image inspection.

## Approaches considered

| Approach | Benefit | Cost or limitation |
|---|---|---|
| Extend `extract_text` to explicit tool-result image positions | Matches the configured intent and reuses local extraction, limits, and sanitization | Requires careful protocol validation; loses visual information |
| Require tools to return text and keep rejecting their images | Smallest proxy change; useful immediate workaround | Breaks tools that return screenshots and leaves the configuration surprise unresolved |
| Add a separate tool-image policy | Lets operators enable user attachments and tool attachments independently | Adds configuration complexity while retaining the same extraction implementation |

**Selected approach:** extend `extract_text` to a narrowly defined set of
tool-result image positions. No new policy setting is needed.

Protocol evidence: the official [Responses API reference](https://developers.openai.com/api/reference/resources/responses/methods/retrieve)
defines function/custom tool output arrays containing `input_image` and
`input_text` parts. The local session confirms array-valued custom output
with a nonempty call ID. Adapter and fake-upstream tests exercise these
documented representations and the text replacement. A session record is
not claimed to be a captured HTTP request or a live upstream acceptance test.

## Proposed decision

### Recognize attachment context explicitly

Replace the request walker's user-only attachment eligibility flag with an
explicit attachment context: none, user content, or supported tool output.
Only validated protocol envelopes may establish those contexts. Do not infer
eligibility from an `image_url` key, a tool name, a role-like field, or a
deeply nested object alone.

Initially consider direct `input_image` parts in array-valued `output` of
top-level `input` items whose types are `function_call_output` or
`custom_tool_call_output`. Each envelope must have an unambiguous type,
valid call identity, and a supported output representation. Reject duplicate
or conflicting envelope fields. Preserve required protocol fields and scan
other retained fields using the existing request policy. Adding a new
envelope requires protocol evidence and regression coverage.

Before implementation, confirm which of these candidate forms the deployed
Codex client actually sends and which text replacement form the upstream
accepts. Session serialization alone is insufficient. Unsupported candidates
stay blocked; do not broaden the walker speculatively.

Objects inside text containing JSON remain ordinary decoded content. They
must not acquire tool-output attachment privileges merely because they
resemble an envelope. Tool declarations, schemas, assistant messages,
reasoning items, unknown item types, and arbitrary nested objects also remain
ineligible.

### Convert locally, then sanitize

For an eligible image part, reuse the current strict field validation and
inline PNG/JPEG extraction. Reject duplicate or unknown fields, invalid
field types, invalid detail values, references, invalid encoding, and
unsupported formats. Retain the existing detail allowlist unless a separate,
evidence-backed compatibility change is made.

Use the same request-scoped `attachment.Extractor` for user attachments and
tool outputs. After extraction, replace only the image part with the
protocol-supported text part for that output position. Do not change the
surrounding item into a user message or flatten unrelated outputs. Preserve
the call identity and relative order of text and converted image parts.

Run extracted text through the existing request sanitizer before any
forwarding. Preserve detection events and restoration mappings. A blocking
match refuses the entire request; there is no partial forwarding. Never
mark extracted text as opaque protocol content or skip its decoded/embedded
JSON inspection.

### Retain resource and failure boundaries

All attachments in one request share the existing budgets: eight
attachments, 10 MiB of decoded attachment bytes, 2 MiB of extracted text,
20 PDF pages, and the 30-second extraction context. The existing image
limit is 16 million pixels per image; extractor subprocess concurrency is
limited to two globally. The proxy request-body limit continues to apply.
Tool outputs must not reset any budget or create a new extractor per image.

Any extraction, validation, cancellation, or sanitization failure rejects
the entire request before forwarding. Attachment mode `block` still rejects
these parts. No failure may fall back to forwarding raw images, including
when `on_error` is configured differently.

## Operability

Roll out as an adapter compatibility change under the existing opt-in
`extract_text` mode. Document that it covers supported user attachments and
supported tool-result images, and that both become text. Operators using
`block` retain their existing behavior.

Keep HTTP 422 and the existing enumerated attachment diagnostics. Explain
that `attachment_image_shape` may indicate an unsupported location or
envelope, not corrupt image bytes. Document this in operator guidance and
the fixed `explain` wording. A new diagnostic, if needed, must use a closed
vocabulary and remain compatible with decision-log validation.

Monitor rejection counts, attachment diagnostic codes, request latency,
and extraction failures through existing value-free records. Never log
image bytes, OCR text, tool output, subprocess output, or arbitrary JSON
paths. Inspect captured verification output locally rather than printing
fixtures or detailed reports into assistant sessions.

Recovery remains specific to the failure: provide text for unsupported
placements, repair local OCR dependencies for missing-tool failures, and
reduce inputs for limit failures. If incompatible image history keeps being
resent, begin a fresh session without that history. Disabling extraction
restores refusal of attachments; it does not make affected requests succeed.
Do not advise weakening blocking rules or enabling passthrough.

## Validation required before acceptance and release

1. Establish the actual tool-output wire envelope with captured local
   inspection and a minimal synthetic compatibility probe. Record only
   shape metadata and outcomes. Confirm the upstream accepts the proposed
   text replacement for each supported item type.
2. Add a regression reproducing an image in the verified tool-output
   location; demonstrate the current placement failure before changing code.
3. With deterministic extraction in tests, verify mixed text/image ordering,
   multiple images, preserved call identity, sanitized OCR text, blocking
   matches, and response restoration. Verify the upstream receives neither
   original image bytes nor unsanitized extracted values.
4. Cover duplicate/conflicting envelope and image fields, unknown metadata,
   unsupported locations, embedded JSON lookalikes, references, malformed
   images, invalid detail values, and attachment mode `block`.
5. Verify budgets shared across user and tool attachments, timeout and
   cancellation, missing OCR dependencies, extraction failure, empty output,
   and zero upstream calls for every refusal. Include strict-profile and
   passthrough-configuration refusal cases.
6. Retain user-attachment regressions and verify metadata-only audit/error
   output. Run race tests, lint, and appropriate request-walker fuzz checks.
   Run a captured local OCR integration check separately from deterministic
   adapter tests.

## Consequences and remaining decisions

Supported screenshot-producing tools can continue through the proxy using
sanitized text. Image-based visual tasks remain limited, and tools returning
screenshots without readable text still fail with the existing empty-text
diagnostic. Long histories may repeat extraction work and hit shared limits;
cross-request OCR caching is outside this proposal because it introduces
retention and policy-invalidation concerns.

The maintainer selected local extraction under the existing policy. The
documented function/custom output arrays use `input_text` replacements;
other envelopes remain unsupported. If an actual
protocol cannot represent the replacement safely, retain the refusal and
revisit this ADR rather than silently moving content between roles.

## Status log

| Date | Status | Note |
|---|---|---|
| 2026-11-10 | Proposed | Recommend local text extraction for explicitly validated tool-result image positions; implementation and protocol validation pending. |
| 2026-11-10 | Accepted | Maintainer approved implementation using the existing extraction policy; support is limited to the documented function/custom output arrays. |
