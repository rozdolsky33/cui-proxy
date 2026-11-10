# Tool-result image extraction implementation plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task by task.

**Goal:** Convert supported images in function/custom tool output into sanitized text under `extract_text`.

**Architecture:** Extend the Codex walker with explicit attachment context derived from validated top-level tool-output envelopes. Reuse the request-scoped extractor and existing replacement/sanitization path.

**Tech Stack:** Go 1.26, Go testing/httptest, local Tesseract.

**Spec:** `docs/adr/0003-tool-result-image-extraction.md` (approved approach).

## Global constraints

- Fail closed; never forward raw attachments or bypass blocking rules.
- Retain extraction budgets, user-attachment behavior, and metadata-only logging.
- Capture test output locally; print only statuses and counts.
- Work on `fix/tool-result-image-extraction`; retain the existing ADR edits.

## Review focus

- Tool-output context must not propagate to nested objects or encoded JSON.
- Duplicate envelope fields and empty/non-string call IDs must not grant eligibility.
- Text extracted from images must still be sanitized, blocked when required, and restored in responses.
- User and tool attachments share count, byte, text, and timeout budgets.
- Default block mode and unsupported representations must still refuse before forwarding.

## Task 1: Adapter support and regression coverage

Files: `internal/adapter/codex/request.go`, `attachments.go`, new `tool_attachments_test.go`.

- [x] Add deterministic subprocess-backed extraction tests for both tool-output types, mixed text/images, sanitization/restoration, block rules, shared count limits, malformed envelopes, unsupported placements, and failure modes.
- [x] Run the new acceptance test and observe the existing shape refusal.
- [x] Add `attachmentContext` to walker scope and `toolOutput(n *node) bool` envelope validation. Grant eligibility only to direct image parts of validated output arrays; leave nested/embedded JSON ineligible.
- [x] Reuse existing image validation, request extractor, replacement, and text walking; preserve call identity and ordering.
- [x] Run adapter and attachment tests with captured output.

## Task 2: Proxy integration and operator documentation

Files: new `internal/proxy/tool_attachments_test.go`, `internal/attachment/errors.go`, `docs/attachments.md`, ADR 0003 and ADR index.

- [x] Add httptest coverage proving sanitized text reaches upstream, streamed values restore, and refusal paths make zero upstream calls without leaking values to audit/log output.
- [x] Update attachment documentation and fixed diagnostic guidance for supported tool-output positions.
- [x] Record protocol evidence from the official Responses reference and observed session shape; mark the approved ADR Accepted with a status-log entry.
- [x] Run race suite, vet/staticcheck, build, request-walker fuzz smoke, and captured local OCR smoke when available.
- [x] Review final diff and report validation and remaining operational limits. Leave changes reviewable without committing or publishing.

## Execution record

- Plan and approved ADR reviewed before implementation. Running on a feature branch in the current workspace, preserving the prior documentation changes.
- Protocol evidence: official Responses API documents array-valued output with `input_text` and `input_image` for function/custom call output. The observed session has array-valued custom tool output and a nonempty call ID. No raw request capture or real upstream credentials are required for deterministic local verification.

- Verification: baseline packages passed; new acceptance regression failed with image-shape refusal before implementation, then passed for both output types. Final race suite passed all 17 packages, vet/staticcheck and build passed, request sanitizer fuzz smoke passed (20 seconds).
- Real OCR: installed Tesseract and ran the built proxy with a synthetic PNG and local fake upstream. Both output types returned HTTP 200, original image/OCR value were absent upstream, and response text restored.
- Review: independent reviewer found no concrete production correctness or security issue.
- Required follow-up resolved: refreshed embedded manual after the initial full suite detected stale source hashes.
