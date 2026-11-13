# Codex protocol support and semantic evidence

This matrix describes the repository adapter, not every version of Codex or the
Responses API. It qualifies representation and local effects. A model can reason
differently about synthetic values, even when every round trip is lossless.

Tests below live in `internal/adapter/codex/` unless another package is named.
Run `go test ./internal/adapter/codex ./internal/proxy ./internal/sse` with output
captured as described in [verification](verification.md). The deterministic HTTP
workflow corpus in `scripts/workflow/` remains the end-to-end boundary check.

## Routes and transports

| Form | Behavior and diagnostic | Executable evidence |
| --- | --- | --- |
| `POST /v1/responses`, JSON request, uncompressed SSE response | Sanitizes request and restores supported response fields | `TestMatch`, proxy workflow suite |
| `GET /v1/models` without a body | Passthrough, not content sanitization | `TestMatch`, proxy `TestPassthroughRouteWithBodyBlocked` |
| Other paths/methods, including separate compact/retrieve/resume routes | Unsupported route; no automatic translation | proxy `TestUnknownRouteBlocked` |
| WebSocket transport | Not an advertised transport; no WebSocket adapter or stateful resume contract | `TestMatch` route boundary; SSE-only response guard |
| Compressed request | Rejected, not decompressed and scanned | proxy `TestCompressedRequestBlocked` |
| Successful non-SSE or encoded model response | Blocked as `stream_rewrite` | proxy `TestSuccessfulUnrestorableResponsesBlocked` |
| SSE without Content-Type | Recognized by bounded prefix probe and restored | proxy `TestUnlabelledResponseRestoresSSE` |
| SSE CRLF, multiline data, comments, leading BOM, fragmented reads | Supported framing subject to reader size limits | sse `TestReadCRLF`, `TestReadSingleAndMultilineData`, `TestReadIgnoresComments`, `TestReadLeadingBOM`, `TestReadSplitAcrossReads`, `TestReadOverCap` |

## Request content and attachments

| Position/form | Behavior | Executable evidence |
| --- | --- | --- |
| Message text, function-call arguments, custom-tool input, tool outputs | Recursive scanning, including embedded JSON and content keys | `TestSanitizeRequestScansEveryStringLeaf`, `TestEmbeddedJSONEscapes`, `TestSemanticsCommandEffects`, `TestSemanticsPatchEffects` |
| Repeated full history | Stable mappings; already-sanitized history remains stable | `TestSemanticsRepeatedHistory` (64 turns) |
| Delegated context in function/custom-tool output | Scanned like other content; no agent-specific trust exemption | `TestSemanticsCompactionDelegation` |
| Reasoning/compaction encrypted content | Opaque at recognized protocol positions; not decrypted or semantically validated; ciphertext shape checked under blocking policy | `TestStrictDoesNotScanEncryptedContent`, `TestStrictRequiresEncryptedContentToLookLikeCiphertext`, `TestSemanticsCompactionDelegation` |
| Visible summary beside compaction | Scanned and restorable, independently of ciphertext | `TestSemanticsCompactionDelegation` |
| Protocol identifiers | Position-aware opacity; blocking rules still checked where applicable | `TestOpaqueOnlyAtProtocolPositions`, `TestStrictChecksOpaqueProtocolPositions` |
| Unknown textual fields | Recursively scanned; this is not a promise the provider accepts their schema | `TestSanitizeRequestScansEveryStringLeaf`, `TestKeysScanned` |
| User-content image/PDF with extraction policy | Extracted text is scanned; pixels/layout and model vision semantics are not preserved | `TestAttachmentPDFReplacementAndStrict`, `TestImageOCRTextIsSanitized` |
| Validated tool-output image positions | Extraction under the same policy/budget constraints | `TestToolImageExtraction`, `TestToolImagesHonorPolicyAndBudgets` |
| Attachments disabled, wrong placement, ambiguous part, extraction failure | Explicit attachment diagnostic and fail-closed result; no raw binary fallback | `TestDefaultAttachmentDiagnostic`, `TestAttachmentsOnlyInUserContent`, `TestAttachmentAmbiguitiesBlock`, `TestToolImagesRejectAmbiguousPlacement`, `TestToolImageExtractionFailures` |
| Tool schema describing image/file types | Schema text is scanned, not treated as an attachment | `TestNestedToolSchemasNotBlocked` |
| Invalid JSON, excessive nesting, unsupported binary content | Rejected as bad body/depth/attachment error | `TestNonJSONBodyBlocked`, `TestDepthCap`, `TestBinaryChecksOutsideSchema` |
| Clean integer, decimal and exponent forms | Original spelling preserved | `TestSanitizePreservesCleanNumericSpellings`, `TestSemanticsCommandEffects` |
| Sensitive decimal/exponent literal | Rejected rather than changing numeric semantics | `TestSanitizeRefusesSensitiveDecimalAndExponentNumbers` |
| Sensitive integer literal | Can become a synthetic JSON string; arbitrary numeric type preservation is not promised | `TestLargeIntegersScanned`, `TestEncodingNumericAndKeyRestoration` |

## Response events and session boundaries

| Form | Behavior | Executable evidence |
| --- | --- | --- |
| Output text, refusal, reasoning-summary text, custom-tool input deltas | Restored with per-field holdback | `TestSemanticsStreamVariants` |
| Function-call argument deltas | JSON-aware restoration, bounded buffering, validation on completion | `TestEncodingArgumentEverySplit`, `TestEncodingArgumentsFailClosed`, `TestSemanticsCommandEffects` |
| Known delta without a string delta | Stream error, not silent acceptance | `TestMalformedKnownDeltaBlocked` |
| Non-delta item/content completion | Restores content and flushes relevant pending streams | `TestNonDeltaEventsRestored`, `TestTrailingDeltaBeforeDone` |
| Completed, failed, incomplete and error terminal events | Flush pending supported content; terminal status remains meaningful | `TestSemanticsStreamVariants` |
| Concurrent/interleaved items | Separate restoration state | `TestStreamsAreIsolatedPerItem` |
| New response after interruption, reusing item IDs | Fresh rewriter state, same vault mappings | `TestSemanticsReconnect` |
| Arbitrary suffix replay/resume | Not supported: no replay deduplication or transfer of partial JSON buffers between requests | `TestSemanticsReconnect` checks only a new complete stream |
| Unknown delta and non-summary reasoning events | Opaque passthrough; not a restored-content guarantee | `TestUnknownEventPassesThrough`, `TestReasoningEventsPassThrough` |

## Effects and limits

`TestSemanticsCommandEffects` sends arguments through sanitization and chunked
argument restoration, executes a fixed local shell command, and checks resulting
file bytes and parsed configuration values. It covers quotes, backslashes,
Unicode, integers beyond floating-point precision, decimals and exponents.
`TestSemanticsPatchEffects` applies a restored custom-tool diff to an isolated
file and checks its resulting content. Neither executes model-generated code.

These are bounded representative contracts, not proof of all shell languages,
patch dialects, encrypted compaction meaning, client versions or model reasoning.
Task 04 performance qualification remains unsuccessful; semantic test success
does not change those performance findings.

## Paired live corpus (optional, currently unmeasured)

Use five small cases: `argv`, `file_edit`, `config_edit`, `relationship`, and
`tokenization`. The first three use the same independent effects as the tests.
For relationship, ask the model to compare two fabricated ordered values and
check against the original ordering. For tokenization, ask it to count or extract
literal characters and check against the original text. These last two expose
changes that a reversible value mapping cannot prevent.

For each case, repeat at least five paired trials using the same model revision,
settings, task, oracle and comparable initial state. Alternate direct/proxy order;
keep each trial independent. Preserve prompts and detailed evidence privately.
Record every attempted arm, including failures; never drop a failed pair. Classify
transport/policy/restoration failures separately from a successfully delivered
answer that fails the task oracle. A provider error on either arm is a provider
failure; a proxy failure is possible only on the proxy arm. Missing-arm records
are invalid, not successful evidence. Review classification against private logs.

The offline `scripts/workflow/live_semantics.py` accepts a JSON object with
`schema_version: 1` and a `records` list. Each record has exactly:

- `pair_id`: short stable identifier, unique per trial (repeated for its two arms).
- `case`: one of the five case names above.
- `model`: exact model revision identifier, equal within each pair.
- `arm`: `direct` or `proxy`.
- `status`: `ok`, `provider_failure`, or `proxy_failure`.
- `semantic_pass`: boolean for `ok`, otherwise JSON null.

It performs no network calls. Run it against the private outcome file with stdout
and stderr captured. Its output contains aggregate counts and fixed case names
only. It reports all pairs, comparable pairs, both-pass/both-fail, discordant
outcomes, failure-arm counts and pairs with failures. Failure-arm counts can
exceed failed-pair counts. Empty input is explicitly unmeasured. Nonempty input
means observations exist, not that the corpus is complete or qualification passed.

Discordance measures observed outcome differences; it does not attribute them
causally to the proxy. Model sampling variance requires repeated trials, matched
settings and review of raw evidence. No live trials were run for this change,
and no model-quality or statistical equivalence claim is made.
