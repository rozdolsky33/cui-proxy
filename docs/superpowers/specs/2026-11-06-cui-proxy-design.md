# cui-proxy — Design

Date: 2026-11-06
Status: DRAFT, awaiting builder check and user approval

## 1. Intent and why

The user is a federal SRE. Their employer's AI policy forbids sending CUI, customer identifiers (IPs, hostnames, OCIDs, tenancy IDs), credentials, PHI and similar data to low-side (cloud-hosted) AI tools. Agentic CLIs (Codex CLI, Claude Code) read files, logs and command output and send them to a hosted model, so restricted data can leak without the user typing it.

cui-proxy is a single Go binary that runs as a local reverse proxy between an agent harness and its model API. It detects restricted values in outbound traffic and replaces them with realistic synthetic values, so the model keeps working. It maps synthetic values back to real ones in responses, so commands and edits the agent produces still run against real systems.

Why a proxy rather than a skill/hook: a skill is advice to the model, and enforces nothing; hooks are harness-specific and cannot cover every outbound byte. A proxy covers all traffic from any harness that allows a base-URL override.

## 2. Decisions made by the user

- Mechanism: local HTTP proxy, harness pointed at it via base-URL override (not MITM, no custom CA).
- Behavior on a match: swap in synthetic values and keep working (not block-and-stop).
- First harness: Codex CLI. Design must be extensible to Claude Code and others via adapters.
- Quality bar: extensible, maintainable, testable, best practices.
- Include a UI to watch the proxy at work: current configuration, useful metrics and details.

## 3. Goals

- G1. Transparent to Codex CLI: after one config change, normal use works, including streaming responses and tool calls.
- G2. Outbound sanitization of the full request body (prompt, history, file contents, tool results), not just the latest message.
- G3. Inbound restoration so tool calls and text the harness receives contain real values.
- G4. Deterministic, stable mapping: same real value -> same synthetic value across turns and restarts (harnesses resend full history every turn).
- G5. Synthetic values are format-preserving (valid-looking IP, OCID, SSN, etc.) and can never collide with a real value in the vault.
- G6. Rules defined in a YAML file; adding a detector or harness adapter does not require touching the core.
- G7. Local web UI: live request feed, config view, metrics.
- G8. High test coverage on the core, with a fake upstream for end-to-end tests.

## 4. Non-goals

- Not a compliance guarantee. Detection is imperfect; the user must still obtain ISSM/GOVSEC approval for this approach.
- No cloud components, no telemetry leaving the machine.
- No semantic/ML detection of CUI in v1 (pluggable interface only).
- No multi-user or remote deployment; localhost only.
- No MITM of arbitrary HTTPS.

## 5. Anti-goals (failure even if it "works")

- Leaking a real value upstream because of a parse error or unsupported payload shape. Default is fail-closed.
- Logging real values anywhere (logs, UI, metrics, crash output). Only rule names, counts, and hashed/truncated fingerprints.
- Breaking tool calls by restoring values incorrectly, or restoring values into text the model was never meant to see real.
- The UI becoming an exfiltration path: it must bind to localhost only and never display real values by default.

## 6. Architecture (builder may refine)

Packages (Go, standard library first, minimal dependencies):

- `cmd/cui-proxy` — CLI entry: `serve`, `check-config`, `test-rules` (run detectors over stdin), `version`.
- `internal/config` — load/validate YAML, defaults, hot-reload.
- `internal/detect` — `Detector` interface; built-ins: regex, exact-match dictionary, checksum-validated (e.g. SSN/Luhn), OCID. Registry for adding more.
- `internal/synth` — `Generator` per value class producing format-preserving synthetic values from a keyed HMAC of the real value (deterministic, collision-checked).
- `internal/vault` — bidirectional real<->synthetic map. In-memory with optional encrypted-at-rest file. Never leaves the machine.
- `internal/transform` — scan text, replace spans (outbound), restore (inbound). Includes a streaming restorer that buffers across chunk boundaries.
- `internal/adapter` — `Adapter` interface: identify routes, walk request JSON for text fields, walk response/stream events for text and tool-call arguments. Implementations: `codex` (first), later `claude`, `openai-chat`.
- `internal/proxy` — reverse proxy, TLS to upstream, header passthrough (auth untouched), SSE streaming, fail-closed policy.
- `internal/metrics` — counters/histograms, in-process.
- `internal/ui` — embedded static web UI + JSON/SSE API served on a separate localhost port.
- `internal/audit` — redacted event log.

Request flow: harness -> proxy -> adapter finds text fields -> detectors find spans -> synth/vault replace -> forward upstream. Response flow: upstream stream -> adapter extracts text/tool-call fields -> streaming restorer swaps synthetic back to real -> harness.

## 7. Codex CLI specifics (verify first)

Installed: codex-cli 0.166.0. The user's `~/.codex/config.toml` has no custom provider; `~/.codex/auth.json` exists, so they are probably signed in with ChatGPT rather than an API key. These use different upstreams and wire formats (the Responses API at the OpenAI API vs the ChatGPT backend). The builder's first task is a short spike: confirm how to point Codex at a local base URL for the user's auth mode (custom `model_providers` entry with `base_url`, or an env var), capture the actual request/response shapes (including streaming event types and tool-call encoding), and record findings in the repo. If ChatGPT-login traffic cannot be redirected to a local base URL, say so plainly and fall back to API-key mode; do not work around it with MITM.

## 8. Configuration (YAML sketch)

```yaml
listen: 127.0.0.1:8787
ui: { listen: 127.0.0.1:8788 }
upstreams:
  codex: https://api.openai.com   # per-adapter; builder confirms
policy:
  on_error: block                # block | passthrough (default block)
  max_body_bytes: 20MiB
vault: { mode: memory }          # memory | file (encrypted)
rules:
  - name: oci-ocid
    type: regex
    pattern: 'ocid1\.[a-z0-9]+\.oc[0-9]\.[a-z0-9-]*\.[a-z0-9]+'
    synth: ocid
  - name: ipv4
    type: regex
    pattern: '\b(?:\d{1,3}\.){3}\d{1,3}\b'
    validate: ipv4
    synth: ipv4
  - name: customer-hosts
    type: dictionary
    file: ./hosts.txt
    synth: hostname
```

The HMAC key is generated on first run and stored with user-only permissions.

## 9. UI

Embedded in the binary, served on its own localhost port, no external assets or CDN. Shows:

- Live feed of requests: time, adapter, direction, bytes, rules hit and counts, latency, status. No real values; synthetic values and short fingerprints only, with an explicit opt-in toggle in config for showing real values locally.
- Current configuration: effective config, loaded rules, adapter and upstream health, config validity, reload button.
- Metrics: requests/min, replacements by rule, restore counts, blocked requests, error count, p50/p95 proxy overhead latency, vault size, stream buffer stats.
- A "test a string" box that shows what would be replaced.

Look and feel is left to the builder: simple, dense, readable.

## 10. Testing

- Unit tests per detector, generator (format validity, determinism, no collisions), vault, and streaming restorer (split-chunk cases are the key risk).
- Property/fuzz tests: sanitize then restore round-trips to the original; no real value survives in outbound bytes.
- Golden-file tests for Codex adapter using captured (scrubbed) request/response fixtures.
- End-to-end test with a fake upstream asserting the upstream never sees a seeded real value and the client receives restored values.
- CI-friendly: `go test ./...`, `go vet`, `staticcheck`/`golangci-lint`, race detector.

## 11. Constraints

- Go; single static binary; macOS arm64 primary, Linux amd64 secondary.
- Standard library preferred; every dependency justified.
- Localhost-bind only by default; refuse non-loopback bind unless explicitly overridden.
- Do not modify or store the user's upstream auth tokens; pass headers through.

## 12. Left to the builder

Exact package boundaries, YAML schema details, synthetic-value formats per class, UI framework (plain HTML/JS preferred, embedded via `embed`), metrics representation, file layout for fixtures, logging library.

## 13. Calls made by the author (after the builder check)

Spike result (see `docs/spikes/2026-11-06-codex-redirect-spike.md`): Codex with ChatGPT login CAN be pointed at a local base URL via a custom `model_providers` entry with `requires_openai_auth=true`. Section 7's fallback is not needed. Follow-up on 2026-11-07: `https://chatgpt.com/backend-api/codex` was confirmed as the live base with Codex CLI 0.167.0 and `gpt-6-astra`; routing, streamed restoration, and the tool round trip passed (see `docs/verification.md`).

Author's calls, to be checked by the user:

1. Project at `~/tmp/cui-proxy`, module `cui-proxy`. v1 supports Responses-API-style Codex traffic only; Claude Code is a later adapter.
2. Fail-closed means: block when the route is unknown, the body is not JSON, the body exceeds `max_body_bytes`, or the body contains an image/file/binary part. Within a known JSON request, scan every string leaf EXCEPT an explicit list of opaque keys (ids, `encrypted_content`, signatures, base64 data) which pass untouched. A new unknown field is therefore scanned, not blocked.
3. Restoration applies to assistant text, function-call arguments, and reasoning summaries. It never applies to opaque fields. Every outbound request is re-sanitized in full, including history the harness holds in restored form.
4. Vault is in-memory by default. Restart stability (G4) applies only in file mode (encrypted with a key file, user-only permissions). In memory mode after a restart, synthetic values the vault has never seen pass through unchanged and are counted in a metric.
5. Synthetic values come from reserved ranges (RFC 5737/3849 IPs, `.example`/`.invalid` hostnames, clearly fake-format OCIDs) so they cannot be real external values. Collision check is against the vault and the configured dictionary.
6. v1 detector set: OCID, IPv4/IPv6/CIDR, emails, dictionary file (the user's own list of sensitive values), SSN (validated), card numbers (Luhn), and common secrets (bearer tokens, JWTs, private-key blocks). On overlap the longest span wins. Validators reduce false positives (for example version numbers that look like IPs).
7. Hot reload: new rules apply to new requests; in-flight streams finish with the rules they started with; the vault keeps old mappings.
8. The "show real values in the UI" toggle from section 9 is REMOVED. The UI and audit log never contain real values. Audit log is a bounded in-memory ring buffer.
