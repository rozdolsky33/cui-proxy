# Assurance and operations

## Intended use and trust boundary

cui-proxy is a local inspection and reversible masking layer for supported
Codex text requests and HTTP/SSE responses. It reduces accidental disclosure
of values its configured rules recognize. Use it on a trusted, single-user
workstation for workflows that are already authorized to use the selected
upstream. Each environment supplies its own private configuration and
literal dictionaries outside this source repository.

The native client, operating-system account, configuration, vault key, and
configured upstream are trusted. Browser requests and DNS-rebound Host names
are refused by the proxy API. This is not authentication against another
native process running as the same user. Do not expose the listener as a
shared network service without separate authentication, TLS, and resource
controls; `allow_non_loopback` does not provide those controls.

## Review findings addressed

| Failure | Protection and regression evidence |
|---|---|
| Sensitive numbers forwarded in decimal/exponent form; short custom numeric IDs ignored | Every number is checked; sensitive decimal/exponent forms fail closed; `internal/adapter/codex/numeric_test.go` |
| Quadratic IPv6 candidate scanning on long punctuation input | Bound parsing work to representable address suffixes; `internal/detect/cost_test.go` |
| Unsupported successful model responses bypass restoration | Require recognized, unencoded SSE; otherwise HTTP 502; `internal/proxy/response_guard_test.go` |
| Browser/rebinding requests reach the restoration vault | Native-client and loopback Host checks; persisted block decisions; `internal/proxy/client_guard_test.go` |
| Extra YAML documents silently ignored; invalid ports accepted | Single-document parsing and port validation; `internal/config/hardening_test.go` |
| Large retention values overflow and delete fresh audit history | Bound retention to 0–106751 days at configuration and log API boundaries; `internal/decisionlog/hardening_test.go` |
| Parser errors expose configuration values through service logs/reload responses | Service-facing errors suppress values; explicit `check-config` retains diagnostics; CLI regression tests |
| Concurrent processes overwrite a shared file vault | Hold an exclusive vault lock from load through final save; `cmd/cui-proxy/vault_lock_test.go` |

The minimum toolchain is Go 1.26.10. An initial `govulncheck` scan of Go 1.26.0
reported standard-library vulnerabilities; a scan using the patched toolchain
reported none. For example, the Go advisory for
[GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809) identifies 1.26.10 as fixed.
CI and release jobs run `make vuln` against the current advisory database;
a clean scan is time-specific evidence, not a guarantee of absence of bugs.

## Workspace metadata

From v0.3.2, Codex `workspaces` and `cwd` header fields use the request
sanitizer and vault (including workspace keys and Git remote URLs). Other
metadata fields are inspected after decoding and remain unchanged. Strict or
explicit block rules still reject detections. Invalid metadata fails closed
even under passthrough. Regression and fuzz coverage lives in
`internal/adapter/codex/metadata_test.go` and `internal/proxy/metadata_test.go`;
[verification](verification.md#workspace-metadata-smoke-2026-11-08) records
the synthetic live probe. Credentials remain an inspection exception.

## Deployment and routine checks

1. Build with the required patched Go toolchain. Run `make test`, `make lint`,
   `make fuzz`, and `make vuln` before distributing a modified binary.
2. Keep configuration, dictionaries, vault/key files and audit storage in a
   private directory. Avoid sensitive identifiers in rule names: names are
   deliberately retained in diagnostics and decision records.
3. Run `cui-proxy check-config -config /private/path/config.yaml` and
   `cui-proxy policy check -config /private/path/config.yaml`. For a custom
   compiled adapter, use the same `-adapter` with `check-config` and `serve`.
4. Use `policy.on_error: block`. Set `audit.on_error: block` when delivery
   must stop if recording fails; sanitize mode otherwise defaults to `warn`.
   Choose `strict` when detected values must be refused instead of masked.
5. Run `test-rules` with fabricated samples covering your own rules, then
   repeat the [live smoke](verification.md) when changing clients or upstreams.
6. Validate edits before reloading. A failed reload retains the current
   policy; startup/reload log errors direct you to explicit validation.
   Restart-only settings require a restart, as documented in the README.
7. Verify audit chains regularly with `audit verify`, and monitor disk space,
   blocked requests, errors, audit-write failures, and vault-save warnings.

## Demonstrated scope and remaining limits

The [live evidence](verification-results/2026-11-07-live-smoke.json) records
ChatGPT-authenticated Codex CLI 0.167.0 with `gpt-6-astra`, exact IP/OCID
restoration, and a shell tool round trip using fabricated data. Local tests
cover policy blocks, error handling, reloads, framing, concurrency and storage.
These checks do not establish detection accuracy for arbitrary customer data.

- Patterns and dictionaries can miss information, including semantic meaning,
  unfamiliar encodings, names and local paths absent from configured rules.
  `strict` blocks detections; it cannot classify every restricted fact.
- Credential headers and vendor encrypted reasoning content are documented
  inspection exceptions. A malicious client can bypass the proxy entirely.
- Restoration does not validate a model's reasoning or make generated commands
  safe. Keep Codex tool permissions and normal human review in place.
- Memory-mode restarts lose mappings. File mode checkpoints every 30 seconds
  and on graceful shutdown; abrupt termination or storage failure can lose
  recent mappings. Preserve the key and vault together, and begin a fresh
  client conversation after losing state.
- Vault growth and aggregate concurrency are not globally bounded. Per-request
  limits and bounded parsing do not replace OS resource controls.
- Audit chains are tamper-evident, not externally anchored. A writer who owns
  the files can rewrite an entire chain or remove its newest tail.
- Long-session compaction, websocket transport and arbitrary future response
  event types are outside the recorded live smoke. Unknown request routes
  are refused. Stream restoration covers output text, refusals, reasoning
  summaries, function-call arguments and custom-tool input deltas. Unknown
  delta events, non-summary reasoning events and non-JSON event data pass
  through unchanged. Other JSON events restore eligible string fields while
  preserving opaque fields; this does not guarantee future event compatibility.

This review supports use as an additional local safeguard within those
boundaries. It is not a compliance certification or permission to transmit
restricted data. Organizational approvals, approved-account enforcement and
network controls remain separate responsibilities; deferred approval formats
must not be represented as implemented controls.
