# Configuration and CLI reference

This page is a lookup reference for the checked-out implementation. For a
walkthrough, use [configuration](configuration.md); for complete starting files,
use [recipes](recipes.md). [Rules](rules.md) explains detection and overlap
semantics, and [policy](policy.md) explains blocking versus masking.

## Loading and paths

Configuration is one YAML document. Unknown fields and additional YAML
documents are errors. `check-config` validates the YAML, synth classes,
validators, dictionary files, selected adapter and all configured upstream URLs.
It does not contact the upstream or prove a listener can bind, the vault can be
opened, or the decision log can be written; those are startup checks.

Relative dictionary, vault, key and audit file paths resolve against the YAML
file's directory, not your shell's current directory. `~` is not expanded inside
YAML paths. Use a relative path or an actual absolute path there; shell commands
in this manual use `~` where the shell expands it.

Defaults are **loader defaults**, not the content of `init`'s starter file.
In particular, the loader supplies neither an upstream nor rules. `init` writes
a ChatGPT upstream and three rules: the built-in bundle, a dictionary, and an
AWS-key regex. A sanitize configuration with no rules can validate and detects
nothing; strict requires at least one rule.

## Fields and defaults

**Reload** means save the file, then use the dashboard button or SIGHUP.
**Restart** means stop and start the process. A successful reload keeps the
running values of restart-only fields and logs a warning about changed fields.
The dashboard shows the effective configuration.

### Listener and upstream fields

| YAML field | Loader default | Accepted value and behavior | Apply |
| --- | --- | --- | --- |
| `listen` | `127.0.0.1:8787` | Proxy TCP address, normally loopback. | Restart |
| `ui.listen` | `127.0.0.1:8788` | Dashboard TCP address. Empty uses the default; it does not disable the UI. | Restart |
| `allow_non_loopback` | `false` | Allows non-loopback listener addresses. Adds neither TLS nor authentication. The macOS helper requires loopback. | Restart |
| `upstreams` | Empty mapping | Adapter name to base URL. The selected adapter must have an entry. Currently the registered adapter is `codex`. | Restart |
| `upstreams.codex` | None | Absolute HTTPS URL. Plain HTTP is accepted only for a loopback host for local testing. Credentials, query strings and fragments are rejected. Starter: `https://chatgpt.com/backend-api/codex`. | Restart |

Loopback checks cover both the configured address and the address actually
bound. `localhost` is accepted. Do not point Codex at the dashboard port; see
[Codex setup](codex.md).

### Policy fields

| YAML field | Loader default | Accepted value and behavior | Apply |
| --- | --- | --- | --- |
| `policy.profile` | `sanitize` | `sanitize` masks detections; `strict` blocks them. Rule actions may strengthen sanitize, but cannot weaken strict. | Reload |
| `policy.on_error` | `block` | `block` or `passthrough`. Passthrough can forward the original body on eligible sanitizer failures and is invalid with strict or any explicit blocking rule. It does not permit unsupported content or override policy matches. | Reload |
| `policy.attachments` | `block` | `block` or `extract_text`. Unreleased after v0.3.3: supported inline PDF/PNG/JPEG becomes locally extracted text before scanning; original files never forwarded. All body failures block in extraction mode. See [attachments](attachments.md). | Reload |
| `policy.unsupported` | `block` | Only `block` is accepted. Unsupported content is refused. | Fixed behavior |
| `policy.max_body_bytes` | `20971520` (20 MiB) | Maximum request body bytes. Negative is invalid; zero takes the default, not unlimited. | Reload |

No setting adds an allowlist, strict exception, approval workflow or
`action: allow`. See [limitations](limitations.md) for supported content and
credential exceptions.

### Vault fields

| YAML field | Loader default | Accepted value and behavior | Apply |
| --- | --- | --- | --- |
| `vault.mode` | `memory` | `memory` or `file`. Memory mappings and their random key last only for the process. | Restart |
| `vault.file` | Empty | Required in file mode: encrypted mapping file. A damaged or unreadable existing vault stops startup rather than being overwritten. | Restart |
| `vault.key_file` | Empty | Required in file mode: 32-byte key file. Created with mode 0600 if no existing vault requires an existing key. Keys with broader permissions are refused. | Restart |

File mode uses AES-256-GCM and format `CUIV2`. It saves changed mappings every
30 seconds and on orderly shutdown; a crash can lose newer changes. One process
holds a lifetime lock on the file vault. Keep the key, encrypted vault and parent
directory private. Reload preserves the live mappings in either mode.

After a memory-mode restart, old synthetics in client history are unknown to
the new vault and cannot be restored. Read [architecture](architecture.md) for
mapping lifecycle and [configuration](configuration.md) for pool behavior.

### Audit fields

These configure the persistent decision log, distinct from the dashboard's
in-memory request feed. All explicit audit field changes require restart.

| YAML field | Loader default | Accepted value and behavior | Apply |
| --- | --- | --- | --- |
| `audit.enabled` | `true` | Boolean. `false` is rejected under strict. | Restart |
| `audit.file` | `$XDG_STATE_HOME/cui-proxy/decisions.jsonl` when that environment variable is absolute; otherwise `~/.local/state/cui-proxy/decisions.jsonl` | Value-free decision-log path. An explicit relative path resolves beside the config. | Restart |
| `audit.on_error` | `warn` under sanitize; `block` under strict | `warn` continues when a decision cannot be written; `block` refuses the request. Strict rejects explicit `warn`. | Restart for explicit changes |
| `audit.max_size_mb` | `50` | Integer 1–1,048,576; rotation threshold in binary MiB despite the field name. | Restart |
| `audit.retention_days` | `90` | Integer 0–106,751. Zero retains rotated files indefinitely. The newest rotated file is kept. | Restart |

When `audit.on_error` is omitted, a profile-only reload does not count as an
explicit audit edit; strict still forces failed writes to block. A reload to
strict is refused if audit was disabled at startup. Enable logging and restart
first. The hash chain detects damage under its documented limits; it is not
proof against every form of tampering. See [decision log](decision-log.md).

### Rule fields

`rules` defaults to an empty list and is replaced as a whole when loaded.
Every field below is part of the reloadable rule set. Dictionary contents are
also reread on reload.

| Field | Required for | Default / meaning |
| --- | --- | --- |
| `name` | All rules | No default. Nonempty, unique configuration name. Names appear in diagnostics; use category names, never sensitive values. |
| `type` | All rules | No default. `builtin`, `regex`, or `dictionary`. |
| `action` | None | Omitted follows profile. Explicit `sanitize` or `block`; `sanitize` is invalid under strict. |
| `pattern` | Regex | Go regexp syntax. The entire match is masked, not a selected capture group. No lookaround or backreferences. |
| `validate` | None | Regex candidate validator: `ipv4`, `ssn`, `luhn`, or `ocid`. Omitted means no additional validation. |
| `file` | Dictionary | Literal dictionary file path. One entry per line; blank and `#` comment lines ignored. Empty usable dictionaries are rejected. |
| `synth` | Regex and dictionary | Replacement class: `ipv4`, `ipv6`, `cidr`, `hostname`, `email`, `ocid`, `ssn`, `card`, or `secret`. No default. |

A built-in bundle chooses its own patterns and classes. Its optional pattern,
file, validator and synth fields do not customize that bundle; omit them.
There is no selector to disable a single built-in detector. Dictionary matching
is case-insensitive at alphanumeric boundaries. A synth class must suit the
matched data; `secret` is appropriate for opaque identifiers.

All rules run. An overlapping block cannot be canceled by a sanitizing rule or
by rule order. See [rules](rules.md) for the built-in classes, span resolution,
and examples.

## Reload, restart and signals

| Operation | Behavior |
| --- | --- |
| Dashboard **Reload config** | Reread the existing config path and rule files; swap policy/rules only after successful validation/build. |
| `kill -HUP PID` | Same reload operation. Replace `PID` with the running proxy process ID. |
| Invalid reload | Keep the old active rules and policy; daemon/dashboard diagnostics suppress parser details. Run `check-config` locally for details. |
| Valid reload with restart-only changes | Apply reloadable settings, preserve running restart-only settings, log `WARN restart required for: …`. |
| In-flight request during reload | Finish with its original policy/rule snapshot. Vault remains shared and retained. |
| SIGINT or SIGTERM | Stop with up to 10 seconds for in-flight requests to drain and save a file vault. |
| Second stop signal during drain | Cancel the drain and close promptly. |

After upgrading a binary, restart the running service. Reload cannot load new
program code. The [menu-bar guide](menubar.md) covers its managed process and
binary selection.

## Commands

Run `cui-proxy help` for the command list or `cui-proxy COMMAND -h` for command
flags. Put flags before positional arguments. `PATH`, `NAME`, `PID` and
`REQUEST-ID` below are placeholders.

| Command and flags | Purpose |
| --- | --- |
| `init [-dir PATH] [-force] [-profile sanitize\|strict]` | Write `config.yaml` and `hosts.txt`. Default profile is sanitize. Default directory is `$XDG_CONFIG_HOME/cui-proxy`, otherwise `~/.config/cui-proxy`. Does not overwrite without `-force`. |
| `policy init [-dir PATH] [-force] [-profile sanitize\|strict]` | Same starter generation as `init`. |
| `policy check -config PATH [-require-strict]` | Validate and summarize policy and its limits. `-require-strict` makes a non-strict config fail for scripts/audits. |
| `check-config -config PATH [-adapter NAME]` | Validate config, rule files and adapter/upstreams without forwarding traffic. Adapter default: `codex`. Prints `ok: N rules` on success. |
| `serve -config PATH [-adapter NAME]` | Run proxy and dashboard in the foreground. Adapter default: `codex`. Config is required. |
| `info -config PATH` | Print proxy and dashboard URLs from configuration; does not probe the running service. |
| `test-rules -config PATH` | Read at most 8 MiB from stdin. Write sanitized text to stdout and a rule/class/count table to stderr. Uses a throwaway vault and makes no upstream request. A policy match fails under strict or an applicable blocking rule. |
| `audit verify [-file PATH \| -config PATH]` | Verify the decision-log hash chain across active and rotated files. With neither flag, use the default log path. A broken chain exits 1. |
| `explain [-file PATH \| -config PATH] REQUEST-ID` | Read the decision log and explain one request. Uses the default log path if neither flag is given. The ID comes from errors, `X-Cui-Proxy-Request-Id`, or a dashboard row tooltip. |
| `menubar install [-config PATH] [-binary PATH] [-dir PATH] [-no-launch]` | macOS: compile/install `CuiProxy.app` and normally launch it. Default app directory: `~/Applications`; default config: `~/.config/cui-proxy/config.yaml`; default binary: the one found on PATH. |
| `menubar status [-dir PATH]` | macOS: inspect the installed helper. |
| `menubar uninstall [-dir PATH]` | macOS: quit/remove the helper. Config, dictionary and logs remain. |
| `version` | Print the binary version. |
| `help`, `-h`, `-help`, `--help` | Print top-level usage. |

Exit codes are **0** for success, **1** for failure, and **2** for usage errors.
The `-file` and `-config` log-selection flags are mutually exclusive. Use the
same config as the running service when explaining requests.

Example local checks using only fabricated data:

```sh
cui-proxy check-config -config ~/.config/cui-proxy/config.yaml
cui-proxy policy check -config ~/.config/cui-proxy/config.yaml
printf '%s\n' 'Email demo.operator@example.com for the demo.' | cui-proxy test-rules -config ~/.config/cui-proxy/config.yaml
```

The last command can fail intentionally under strict. Read [testing](testing.md)
for expected outcomes and [verification](verification.md) for broader backend
checks. The [tutorial](tutorial.md) walks through the complete workflow.

## Local dashboard endpoints

These endpoints are on the **dashboard listener**, normally
`http://localhost:8788`, not the proxy API. They are useful for local diagnosis;
the dashboard is their primary client.

| Method and path | Response or effect |
| --- | --- |
| `GET /` | Embedded dashboard HTML. |
| `GET /healthz` | Plain text `ok`; dashboard reachability only, not upstream health. |
| `GET /api/config` | Effective configuration, version, rule definitions/actions and paths. No dictionary entries or vault values. |
| `GET /api/metrics` | Process counters, latency percentiles, vault size, uptime and live-feed drops as JSON. |
| `GET /api/events` | Recent in-memory request events, oldest first, up to 500. |
| `GET /api/stream` | Server-sent request events. A keepalive comment is sent every 15 seconds. |
| `POST /api/test` | JSON `{"text":"…"}`; returns sanitized text and rule/class/count entries using a throwaway vault. Limit: 1 MiB JSON request body. |
| `POST /api/reload` | Reload existing config; send JSON `{}`. Success: `{"ok":true}`. Validation failure: HTTP 422 with `ok: false` and an error. |

For example, this read-only command checks the dashboard listener:

```sh
curl --fail http://localhost:8788/healthz
```

Non-loopback Host headers receive HTTP 421. Writes require
`Content-Type: application/json` (optionally UTF-8 charset); wrong types receive
415. A supplied Origin must be HTTP with the same host and port, and supplied
`Sec-Fetch-Site` must be `same-origin` or `none`; rejected cross-origin/site writes
receive 403. There is no remote login or general CORS access.

The UI test returns HTTP 422 for policy blocks or transformation failures,
413 for an oversized request, and 400 for malformed JSON. Its exact UI messages
and privacy boundaries are covered in [dashboard](dashboard.md). Do not expose
these configuration and control endpoints publicly.

## Fixed limits

| Limit | Value |
| --- | --- |
| Default request body limit | 20 MiB; configurable with `policy.max_body_bytes` |
| Attachment extraction per request (unreleased) | 8 attachments, 10 MiB decoded input, 20 total PDF pages, 2 MiB extracted text, 30 seconds; [details](attachments.md#limits-and-diagnostics) |
| Attachment images and processes (unreleased) | 16 million pixels per input image; PDF raster longest side 4000 pixels; 2 extraction processes globally |
| Structured turn-metadata header | 64 KiB; not a YAML option |
| JSON nesting depth | 64 levels; not a YAML option |
| Function-call argument restoration | 20 MiB cumulative argument-delta input per response and 20 MiB per completed argument document |
| Dashboard test | 1 MiB entire JSON body |
| CLI `test-rules` | 8 MiB stdin |
| Dashboard browser rows / server recent events | 200 / 500 |
| Overhead samples | Most recent 4,096 |

Increasing the body limit does not increase the other limits. Invalid or
unfinished function-call argument documents terminate the response stream.
See [architecture](architecture.md), [limitations](limitations.md) and
[glossary](glossary.md) for context.
