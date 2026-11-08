# JSON escape dataflow repair — 2026-11-08

Branch: `fix/json-escape-dataflow`, based on `cc01fa7`.

## Failure and correction

After a GitHub CLI command completed, its returned output was carried as JSON
inside a request string. The adapter sanitized decoded fields, then scanned
and rewrote their serialized JSON a second time. An email match could consume
the `n` from a preceding `\n` escape, leaving an invalid escape. The resulting
HTTP 422 was a body sanitization error; it was not an SSH connection denial.

The adapter now transforms decoded keys, strings and supported numbers once
per JSON level. It still checks raw and canonical JSON for custom matches
spanning structure; those cannot be safely rewritten and fail closed. Keys
inside embedded JSON are content even when their names resemble protocol
fields. Outer opaque protocol fields retain their established handling.

The adjacent-path review found and corrected these related problems:

- Already-inspected metadata was rescanned as raw JSON, causing false matches
  at escaped boundaries. Only headers successfully inspected by the adapter
  are exempt from the generic raw-value check. Header names, other headers,
  queries, immutable metadata fields and explicit block rules remain checked.
- Function argument restoration used plain matching over serialized JSON.
  JSON string and number tokens are now buffered and restored in decoded form,
  including nested JSON and Unicode escapes. Streamed and completed forms use
  the same content semantics; object keys stay plain decoded keys.
- Equivalent escaped item IDs or numeric index spellings could split a logical
  stream. Stream identity now uses bounded canonical values.
- Unfinished arguments could reach a clean EOF. Finalization now returns an
  error through the adapter and proxy, so the client sees an interrupted stream
  and the audit records `stream_rewrite` without logging argument values.

## Regression coverage

All committed examples use fabricated values.

- Newline, carriage return, tab, backspace, form feed, quote, backslash, control
  and Unicode separators before email/IP/SSN matches, at multiple JSON levels.
- Exact decoded restoration and idempotent sanitization on a second turn.
- Embedded protocol-looking keys and custom rules spanning JSON tokens.
- Metadata escapes, nested JSON, case-insensitive header ownership, duplicate
  headers, malformed metadata, and strict/explicit blocking.
- Every byte split in representative argument strings, nested JSON, escaped
  synthetics, numeric card placeholders and JSON-looking object keys.
- Delta/done consistency, equivalent stream IDs/indices, scalar arguments,
  invalid JSON, incomplete EOF, nesting bounds and cumulative argument limits.
- End-to-end HTTP/SSE tool-output restoration and metadata-only audit/log checks.
- Existing query percent-decoding, body restrictions, routing, vault and plain
  text restoration tests remain part of the full suite.

`FuzzArgumentJSON` compares independently implemented token streaming and
complete-document restoration after decoding their results. It runs in local
`make fuzz` and both CI operating systems alongside the existing fuzz targets.

## Validation

Passed on macOS with Go 1.26.10:

- `make test` — complete Go race suite.
- `go vet ./...` and Go 1.26-compatible `staticcheck -checks=all ./...`.
- `make fuzz` — all four targets, 20 seconds each: request sanitization
  (44,635 executions), SSE rewriting (1,487,628), argument JSON (474,687),
  and transform round trips (76,260).
- `make build` and example `check-config` validation.
- `govulncheck ./...` — no vulnerabilities found.
- `git diff --check` — no whitespace errors.

The independent reviewer also ran adapter/proxy tests and a separate 10-second
argument JSON fuzz session (131,774 executions).

The original local tool output was replayed offline with the existing eight-rule
policy, without contacting an upstream. Both captured tool outputs and their
embedded JSON passed; no real output values were copied into this repository.
Independent review identified the numeric and key-context regressions above;
they were corrected, and the second review found no remaining concrete blocker.

## Scope and operating limits

Function arguments buffer a complete JSON token, which can delay a large string
until its closing quote arrives. Cumulative argument delta input per response
and individual completed argument documents are limited to 20 MiB; JSON nesting
is bounded at 64 containers. These are fixed limits, not policy bypasses.

This audit covers the supported HTTP/SSE dataflow and the demonstrated encoding
failure family. Arbitrary prose/custom-tool input remains plain text; it is not
parsed as a programming language. Unknown delta types and opaque vendor fields
retain their documented behavior. Tests cannot establish that every possible
future payload is defect-free. No live backend smoke, service restart, installed
binary replacement or configuration change was performed for this repair.
