# Codex workspace metadata repair

Goal: allow normal repository sessions without exposing detected workspace values.

Design: an optional adapter header interface handles Codex turn metadata. Only
confirmed content fields `workspaces` and `cwd` are rewritten using the existing
JSON walker and active sanitizer. All other decoded metadata is inspected but
never rewritten. Authentication is untouched. Unknown headers and queries retain
the existing block behavior. Invalid, oversized, duplicate header values and
unsafe metadata fail closed, even under passthrough. Strict/block rules still
block. Share the request vault and record metadata replacements only once the
whole request is eligible for forwarding. No values enter diagnostics.

Alternatives: dropping workspace metadata loses useful context; bypassing email
or all metadata inspection leaks values. Structured sanitization fits the existing
sanitize/strict policy without another configuration switch.

- [x] Add failing HTTP regression tests for SSH remotes and workspace paths.
- [x] Add adapter metadata handling and integrate request audit accounting.
- [x] Test malformed/escaped/duplicate metadata, strict rules, and other headers.
- [x] Update documentation; run race tests, lint, build and relevant fuzz smoke.
- [x] Verify live against synthetic Git repository.
- [x] Install the verified build and validate the saved default routing.

Validation: full race suite passed; lint passed with Go-1.26-built staticcheck
2026.2.2; metadata fuzz passed 200,268 executions. Synthetic Git remote request
failed with 422 on installed v0.3.1 and returned PROBE_OK / HTTP 200 on patch
using the same active eight-rule policy. Audit chain verified. Independent
review found encoded unknown metadata and passthrough audit-count issues; both
were reproduced in tests and fixed, then verified by the reviewer.

Installed locally as v0.3.1+metadata.1. Saved-default routing returned exact
PROBE_OK 10.20.30.15 from a synthetic Git repository; no provider overrides.
Original binary and config backed up outside the repository. No release published.
