# Attachment Text Implementation Plan

**Goal:** Allow explicit local attachment-to-text conversion and explain refusals without disclosing values.

**Architecture:** Configure each request using its immutable policy snapshot. Convert recognized inline parts in the existing duplicate-preserving JSON walker, then sanitize their replacement text normally. Local bounded extraction is isolated in `internal/attachment`.

**Tech Stack:** Go 1.26, Poppler, Tesseract.

**Spec:** `docs/superpowers/specs/2026-11-08-attachment-text-design.md`

## Global Constraints

Default block; no original bytes forwarded; no remote fetches; fail closed on all attachment errors; value-free diagnostics.

## Review Focus

Duplicate or unknown attachment fields; unsupported references; metadata leakage; OCR failure and output limits; passthrough configuration bypass.

### Task 1: Policy and attachment conversion

- [x] Add tests for configuration validation, default refusals, safe image/PDF text substitution, duplicate fields, unsupported references, sanitization and strict blocking.
- [x] Run targeted tests and confirm failure before implementation.
- [x] Implement `policy.attachments`, local extraction with fixed resource limits, and context-aware optional adapter handling using the request snapshot.
- [x] Run targeted tests and verify only sanitized text survives conversion.

### Task 2: Diagnostics and documentation

- [x] Add failing proxy tests for value-free actionable attachment errors, decision-log explanation, and fail-closed behavior under passthrough.
- [x] Implement closed diagnostic details and explanations; expose policy in CLI/dashboard.
- [x] Document settings, local tool requirements, supported wire formats and extraction limitations.
- [x] Run `make test`, `make build`, `make lint`, config validation and focused fuzz smoke; review the resulting diff.

## Verification

Completed on branch `feat/attachment-text`. Full `make test` (race), `make build`,
`make lint` using Go 1.26-compatible staticcheck, `staticcheck -checks=all`,
example config validation, and `git diff --check` passed. Sanitizer fuzz smoke
passed 15,359 executions. Fresh-context review found two diagnostic issues;
both received failing regression tests, fixes, and a green full-suite rerun.

Real Poppler PDF extraction and rasterization were exercised. Tesseract is not
installed on this machine, so OCR integration uses controlled local executables;
actual recognition and deployment compatibility remain unverified. Native tools
have bounded inputs/output/time/concurrency but no hard OS memory containment.
The local binary is rebuilt; the running service and live policy were not changed.
