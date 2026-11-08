# JSON escape dataflow repair

Goal: fix the reproduced tool-output failure and inspect adjacent request,
metadata and streamed-response transformations for the same encoding mistake.
Work is on branch `fix/json-escape-dataflow`; no live policy changes.

Approach: keep JSON structure separate from decoded content. Preserve strict
rules, unsupported-input rejection, numeric inspection, custom rules, vault
sharing, exact restoration and value-free diagnostics. Do not solve failures
by suppressing detector matches or allowing malformed JSON.

- [x] Add failing regressions for escaped separators, nested JSON, object keys,
  tool arguments/output, workspace metadata and resend behavior.
- [x] Repair request processing with decoded-field transformations and safe
  handling of rules that match across JSON structure.
- [x] Audit response deltas/done events and JSON-aware restoration; add tests
  for escaped boundaries, nested strings and chunk splits; fix proven defects.
- [x] Verify full HTTP/SSE round trip and fail-closed/audit behavior.
- [x] Run race suite, all staticcheck checks, build, vulnerability and fuzz
  smoke tests; replay the local failing output offline without storing values.
- [x] Independent review; document coverage and residual limitations; commit.

Review focus: escaped newline/tab/backspace/Unicode boundaries; quotes and
backslashes in restored values; protocol-like keys inside embedded data;
custom patterns spanning JSON tokens; repeated turns using existing synthetics.

Outcome: the original local tool output passes an offline replay under the
unchanged active policy. The final race suite, static analysis, build,
configuration validation, vulnerability scan and four fuzz targets pass.
Independent re-review found no remaining concrete blocker. Coverage and
operating limits are recorded in
[the dataflow report](../../verification-results/2026-11-08-json-escape-dataflow.md).
