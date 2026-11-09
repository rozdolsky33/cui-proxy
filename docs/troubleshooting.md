# Troubleshooting

First identify the failing layer: configuration/startup, the local dashboard,
a proxy policy decision, the model upstream, or a local tool such as Git.
A policy block and a broken SSH login need different fixes.

Use fabricated input when reproducing a problem. Keep raw prompts, credential
headers, dictionary entries, vault files and keys out of reports. Rule names,
regex patterns and paths can also reveal information; inspect diagnostics and
screenshots before sharing them.

## Establish the active setup

Run these local commands with the config used by your service:

```sh
cui-proxy version
cui-proxy check-config -config ~/.config/cui-proxy/config.yaml
cui-proxy policy check -config ~/.config/cui-proxy/config.yaml
cui-proxy info -config ~/.config/cui-proxy/config.yaml
```

`check-config` is deliberately more detailed than daemon errors and can quote
configuration text. `info` reports configured URLs; it does not prove a process
is listening there. Open the [dashboard](dashboard.md) to inspect the running
version, profile, rule actions and effective settings. An installed new binary
does not replace an old running process: restart after upgrades. On macOS,
check the [menu-bar helper](menubar.md)'s binary and config selection too.

If a proxy response contains a request ID, look up its decision:

```sh
cui-proxy explain -config ~/.config/cui-proxy/config.yaml REQUEST-ID
```

Use the 16-character hexadecimal ID from the error, response header or dashboard
row tooltip. See [decision log](decision-log.md) and
[block diagnostics](block-diagnostics.md) for the evidence and reason codes.

## Startup and configuration

| Symptom | Diagnosis | Fix |
| --- | --- | --- |
| `configuration or rule files could not be loaded` | The daemon hides parser details to avoid logging values. YAML, a regex, a synth/validator, or a dictionary may be invalid. | Run `check-config` locally on the same file, correct its detailed error, and retry. Use [reference](reference.md) for accepted fields. |
| Unknown field or unexpected YAML document | Misspelled key, unsupported option, or more than one YAML document. | Keep one document and use only supported fields. There is no `allow`, `exclude` or `action: allow`. |
| Dictionary not found, or `has no entries` | Relative paths are based on the YAML directory; blank and comment-only dictionaries are invalid. | Put the literal file beside the config or use its actual absolute path. Add fabricated entries for tests. YAML does not expand `~`. |
| `no upstream configured for adapter "codex"` | Loader defaults do not supply an upstream. | Use a complete [recipe](recipes.md), or add the appropriate `upstreams.codex` to your existing config. |
| Upstream URL validation error | Non-absolute URL, embedded credentials, query/fragment, unsupported scheme, or non-loopback plain HTTP. | Use an absolute HTTPS base URL. Plain HTTP is for loopback fake upstreams only. Authentication belongs in client headers. |
| Bind/listen failure or address already in use | Another process owns the proxy or UI port, or the address cannot bind. | Stop the duplicate instance, or choose two unused loopback ports and update Codex routing. Restart for listener changes. Avoid running a manual server alongside the menu-bar managed server. |
| Non-loopback address refused | Listener validation or the actual bound-address check rejected it. | Use `localhost` or a loopback literal. `allow_non_loopback` is an explicit exposure override, not a routine fix or authentication layer. |
| Strict configuration rejected | It has no rules, explicit `action: sanitize`, passthrough, disabled audit, or audit warning mode. | Keep at least one rule, remove weakening settings, and use enabled audit with block-on-error. See [policy](policy.md). |
| File vault is locked | Another proxy owns that file. | Stop the other owner or use a separate vault for a separate instance. Do not share one writable vault across processes. |
| Existing vault cannot open | Missing/wrong key, broad key permissions, corruption, or incompatible format. | Restore the matching key/vault pair and private permissions. Do not generate a replacement key for an existing vault. See [configuration](configuration.md). |

Start from the [quick start](quickstart.md) or [tutorial](tutorial.md) if a custom
file is difficult to reason about. Compare against a complete recipe rather
than pasting a partial YAML block over an entire configuration.

## Dashboard and reload

| Symptom | Diagnosis | Fix |
| --- | --- | --- |
| Browser cannot connect | Wrong port, proxy process stopped, or changed `ui.listen`. | Use the startup dashboard URL. Default UI port is 8788; proxy port is 8787. Test `curl --fail http://localhost:8788/healthz` for the default listener. |
| `config ok` but no traffic | Config fetch succeeded, but Codex might bypass the proxy, no request has completed, or the feed disconnected. | Check [Codex provider setup](codex.md), effective proxy URL and feed status. Streaming requests appear only when they finish. |
| `config unavailable` | Browser cannot fetch the dashboard config endpoint. | Verify the local listener and reload the page. Use a localhost URL, without a remote dashboard wrapper or rewritten Host. |
| `reconnecting...` or `live feed failed (reload the page)` | The event connection was interrupted. | Refresh the page; verify the process remains running. Feed drops mean slow event consumers, not failed durable writes. |
| `reload failed` with generic rule-file error | New YAML or detector build failed; old rules remain active. | Run `check-config`, fix the file and reload again. |
| Reload says success but port/upstream/vault/audit did not change | Those fields are restart-only and pinned to their running values. | Restart, then inspect the effective panel. Check daemon warnings listing fields requiring restart. |
| Reload to strict refused because audit was disabled | Reload cannot open a log disabled at startup. | Set `audit.enabled: true` and restart with a valid strict configuration. |
| Dictionary or regex edit has no effect | Disk edits are not automatically watched, or you edited another file. | Validate the actual service config, reload and inspect the displayed rule/path/action. |
| Built-in row has empty Synth and Pattern / file | Expected: the bundle supplies its own patterns/classes. | Test a fabricated built-in match; do not fill those cells to customize the bundle. See [rules](rules.md). |
| HTTP 421, 403 or 415 from a dashboard endpoint | Host, cross-origin/site, or JSON-content-type guard rejected the request. | Open the dashboard directly through localhost. Local API writes need JSON and same-origin conditions; see [reference](reference.md#local-dashboard-endpoints). |

The dashboard is local and has no login. Keep it on loopback. It displays
configuration paths and patterns, so screenshot privacy still matters even
though the live request feed contains metadata rather than prompt values.

## Unexpected rule-test results

Use [Test a string](dashboard.md#test-a-string) or the local CLI before involving
a model backend:

```sh
printf '%s\n' 'Send the demo report to demo.operator@example.com.' | cui-proxy test-rules -config ~/.config/cui-proxy/config.yaml
```

| Symptom | Diagnosis | Fix |
| --- | --- | --- |
| `no matches` or unchanged text | No loaded detector recognizes that value, a regex boundary differs, or the test uses old rules. | Confirm the effective rule, reload edits, and test positive/negative fabricated examples. Built-ins are not general username/hostname/path detectors. |
| UI says `error: blocked by policy (rule=…, class=…)` | Strict or explicit `action: block` matched. | This is expected behavior. Remove the sensitive value from the request or adjust a custom policy only if masking is acceptable. No strict per-value override exists. |
| Broad match or unexpected count | Full regex matches and overlap resolution differ from intended capture groups; multiple rules ran. | Test punctuation/adjacency/repeats, narrow the custom regex, and read [rule semantics](rules.md). A blocking overlap always wins. |
| `text could not be sanitized reversibly` | A match cannot be transformed safely, including an unsuitable synth class or ambiguous input. | Reproduce with fabricated data and inspect pattern/class. Use `secret` for opaque identifiers. Do not bypass the failure by enabling passthrough. |
| UI `text too large` | The JSON body exceeded 1 MiB, including escaping overhead. | Use smaller samples or `test-rules` for at most 8 MiB stdin. These are separate from the proxy request-body limit. |
| Synthetics differ between tests | Each test uses a new random-key throwaway vault. | Assert masking, class and count rather than a fixed synthetic spelling. |
| Test passes but a real request blocks | Plain-text tests do not exercise request structure, metadata, headers, routing or response streams. | Follow [testing](testing.md) and [verification](verification.md) with a fabricated full request. |

Neither a successful rule test nor `no matches` guarantees that a request has
no sensitive content. Detection is limited to configured rules and supported
representations; read [limitations](limitations.md).

## Proxy refusals and upstream failures

The error's `reason`, `kind`, `rule`, optional `detail`, and request ID narrow the diagnosis. A
metadata `header` error does not identify a particular header by itself.

| Symptom / code | Diagnosis | Fix |
| --- | --- | --- |
| `policy` / `rule_match` | A body detection has effective action block. | Check active profile and named rule. Remove the value or change policy only when your requirements permit masking. |
| `header` / `rule_match` or `bad_body` | Strict metadata detection, a match in an unchanged field/header/query, or malformed structured turn metadata. | Reproduce with fabricated workspace metadata and verify the current binary. Supported `workspaces`/`cwd` masking does not make every header rewritable. |
| `route` | Unsupported method/path. | Use the documented Codex Responses provider setup and supported routes; arbitrary API traffic is not automatically supported. |
| `compressed` | Request body encoding cannot be inspected. | Send uncompressed supported JSON. There is no unsupported-content bypass. |
| `body_too_large` | Request body exceeds configured limit. | Reduce context/request size, or deliberately adjust `policy.max_body_bytes` and reload. Other fixed limits remain. |
| `sanitize` / `bad_body` | Invalid JSON/UTF-8, unsupported content, or unsupported structure. | Remove images/files/audio or other unsupported input and reproduce with synthetic text-only content. Check [limitations](limitations.md); do not use passthrough as a fix. |
| `sanitize` / `depth` | JSON nesting exceeds 64 levels. | Simplify nested data. Increasing the byte limit does not change this bound. |
| `sanitize` / `pseudonym` | A detected value does not suit its synth class. | Correct the custom rule or validator and test fabricated cases. |
| `sanitize` / `pool_exhausted` | A synthetic pool is exhausted. | Review [configuration](configuration.md)'s pool limits and vault lifecycle. A planned fresh vault loses old restoration mappings; do not delete a live vault casually. |
| `sanitize` / `ambiguous` | A value conflicts with an issued overflow synthetic. | Review the documented overflow-pool restriction. Preserve fail-closed behavior; capture only fabricated reproductions. |
| `unrestorable` | A safe reversible transformation could not be achieved. | Keep the refusal, record rule/kind/version and reproduce with fabricated input for a private security report if appropriate. |
| `audit` with HTTP 503 | Required decision record could not be written. | Inspect log directory permissions, available disk space and decision-log health. Restore writeability; keep strict logging enabled. |
| HTTP 502 `upstream` | The proxy could not complete the upstream request. | Check intended HTTPS upstream, network/VPN and authentication environment. A local rule test cannot validate connectivity. |
| HTTP 502 `upstream_redirect` | Upstream returned a redirect which the proxy refuses to relay. | Correct the upstream base URL rather than relying on redirects. |
| `stream_rewrite`, cut stream, or apparent 200 followed by failure | Unsupported/encoded response format or restoration failed after streaming began. | Use a supported SSE Responses backend. Check value-free daemon diagnostics and fabricate a reproduction; initial HTTP status alone cannot prove stream success. |
| Upstream 401/403 | Client credentials or backend permissions may be wrong. | Confirm the chosen provider and login configuration; the proxy passes designated authentication headers unchanged. Do not paste those headers into reports. |

`policy.on_error: passthrough` is unsafe for original bodies and is not a
universal unblock switch. It cannot coexist with strict or explicit blocking
rules, and does not override metadata failures, policy detections, unsupported
content or size/depth limits. See [policy](policy.md) and
[block diagnostics](block-diagnostics.md).

## Git and SSH are separate from model routing

Codex sends model requests through cui-proxy. A local `git` command opens its
own network connection. The proxy does not authorize shell commands or open,
filter, or approve the SSH socket. It can still block the **model request**
containing a remote URL, proposed command, or earlier tool output.

| What you see | Investigate | Appropriate fix |
| --- | --- | --- |
| Proxy HTTP 422 with `rule=builtin.email` around a fabricated SSH remote such as `demo.operator@example.com:demo/repo.git` | Model-request content and effective action. An email-like SSH user/host can match the email detector. | Supported text and workspace fields can be masked under sanitize; strict and blocking rules still refuse matches. Test the fabricated remote locally and use `explain`. |
| `Permission denied (publickey)` | SSH key, agent environment, GitHub identity and repository access. | Repair SSH authentication/authorization. Proxy YAML has no SSH allowlist. |
| DNS failure, connection timeout/refused | Network, VPN, firewall and SSH endpoint. | Diagnose the local Git/SSH network path. |
| Agent command approval or sandbox denial | Execution and network permissions of the agent. | Use the supported approval flow for the intended command; a proxy profile cannot grant it. |
| `non-fast-forward` | Git branch history. | Fetch and reconcile branch state according to the repository workflow. |

A read-only `git ls-remote origin` tests remote access from the same repository;
it contacts the remote and may require agent network permission. If it works
in your terminal but fails in an agent, compare SSH-agent environment and
execution permissions. Do not change transport or disable detectors merely
to fix a different layer.

For the escaped-newline-before-match `sanitize:bad_body` defect in v0.3.2 and
earlier, use v0.3.3 or later and restart the running proxy. SSH key changes,
disabling email detection and enabling passthrough do not repair that parser
issue. See [configuration](configuration.md#git-and-ssh-troubleshooting) for
the historical context and [verification](verification.md) for regression scope.

## PDF, image and file refusals

`422 reason=sanitize kind=bad_body` is a body validation failure, independent of
the number of blocking rules. Attachments are blocked by default. The
[text extraction mode](attachments.md) converts supported inline PDF/PNG/JPEG
content locally; it never forwards originals.

New errors add fixed details such as `attachment_file_disabled`,
`attachment_file_reference`, or `attachment_image_missing_tool`, with a remedy
in the response and server log. `explain REQUEST-ID` reads the persisted detail.
An old record without a detail cannot reveal the exact cause retroactively.
For tool failures, check the service PATH and English Tesseract data; a terminal
installation alone does not prove a menu-bar process can find the executable.
A v0.3.3 binary rejects the new setting: upgrade/build and restart first.

## Decision-log and restoration questions

If `explain` reports no record, confirm the log path, whether audit was enabled,
and whether retention removed the relevant segment. The UI's recent events are
not a permanent substitute. A failed log write can also leave no persistent
record. Run:

```sh
cui-proxy audit verify -config ~/.config/cui-proxy/config.yaml
```

A broken chain is evidence to investigate, not an invitation to edit the log.
Strict/block-on-error startup refuses damaged logs; sanitize warning mode can
start a new segment. See [decision log](decision-log.md) for recovery and
verification limits.

If a model echoes synthetics after a restart, check whether you used memory
mode or switched vaults. Old mappings are gone in memory mode. File mode needs
the same readable vault and key. If response text invents or changes a token,
exact restoration may also be impossible; see [architecture](architecture.md)
and [glossary](glossary.md).

For a useful report, include the binary version, fabricated minimal input,
profile/effective rule action, error reason/kind, and which local or end-to-end
check reproduced it. Share no real sensitive values. Report suspected bypasses
through the repository's private vulnerability-reporting process.

### Repeated policy blocks after reading a fixture

A fabricated identifier or credential example can match an explicit blocking
rule. If a tool prints it into a proxied assistant session, the next request
can fail with HTTP 422, `reason=policy kind=rule_match`. Retrying can repeat the
failure because conversation history includes the same tool output.

Reproduce with `cui-proxy test-rules` locally, capture stdout and stderr, and
report only the status and rule name. Keep matching fixtures and full config
responses out of the session. Start a fresh session if the existing history
contains the trigger, and avoid reading that fixture back into it. Preserve
intentional blocking rules. See the [active-policy verification check](verification.md#check-the-active-policy-before-repeating-a-smoke).
