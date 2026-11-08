# Manual verification against the real Codex CLI

**Status: live backend smoke passed on 2026-11-07.** Built from this checkout
on macOS arm64 with Go 1.26.10; exercised Codex CLI 0.167.0, ChatGPT login,
model `gpt-6-astra`, and reasoning effort `low`. Only fabricated prompts were
used, from an empty scratch directory, with command-line provider overrides;
the global Codex configuration was unchanged.

## Recorded results

[Recorded run summary](verification-results/2026-11-07-live-smoke.json)
contains versions, case outcomes, metrics, and scope without session credentials.

| Check | Observed result |
|---|---|
| Live routing and authentication | `GET /v1/models` and `POST /v1/responses` returned HTTP 200 |
| Greeting | Exact reply `CUI_PROXY_LIVE_OK` |
| IP and OCID restoration | Exact fabricated originals returned: `10.20.30.15` and `ocid1.instance.oc1.phx.anyhqljrabcdefghijklmnop` |
| Shell tool round trip | Codex executed `printf "host=10.20.30.15 done\n"`; shell output and final reply contained the original fabricated IP |
| Resent history | The second tool-turn request recorded three IPv4 replacements, covering the restored history/tool output |
| Decision log | The baseline live-run chain passed `audit verify`; logs contained no fabricated prompt values |

The baseline run recorded 8 requests, 6 replacements and 16 rewritten events.
It reported zero errors, zero passthroughs, and zero audit-write failures.
`restores` counts rewritten events, including buffered delta re-chunking; it is
not the number of distinct restored values.

Live testing found that the Responses endpoint can omit `Content-Type` on
its SSE response. Header-only classification skipped restoration. The proxy
now probes an unlabelled, unencoded response on a sanitizing route and applies
SSE restoration when its framing matches. Regression tests cover the missing
header, comments, BOM, leading blank lines, and unchanged JSON error bodies.

This smoke covers text and a function tool call over HTTP/SSE. Long-session
compaction, websocket transport, image input, and custom `apply_patch` events
are outside this run's coverage. Existing content restrictions still apply.

## Workspace metadata smoke (2026-11-08)

The [recorded metadata run](verification-results/2026-11-08-metadata-fix.json)
used a synthetic Git repository with an SSH remote and the saved default
routing, without provider overrides. The installed v0.3.1 returned HTTP 422
with reason `header`; the local v0.3.1+metadata.1 patch returned HTTP 200 and
exact reply `PROBE_OK 10.20.30.15` under the same eight-rule policy. Its
temporary audit chain verified. This is evidence for workspace metadata and
fabricated prompt restoration, not every possible request body or transport.
The metadata fix is included in v0.3.2.

To repeat, use a scratch Git repository with a fabricated SSH remote such as
`git@github.com:example/demo.git`, keep email detection enabled, and ask for
an exact fabricated IP reply. Confirm successful restoration and metadata
replacement counts. Strict or explicit block rules should still reject a
matching remote. Never put real restricted values in a probe or its report.

## Repeat the smoke

You run this by hand, once, on your own machine, with your own Codex login.
Use only synthetic prompts. The values in step 6 are made up on purpose; do
not swap in real ones.

## 0. Before you start

- Codex CLI is installed and signed in with ChatGPT (`codex login`).
- Your global `~/.codex/config.toml` is never edited. The provider is passed
  with `-c` flags only, so nothing persists after this test.
- Keep a second terminal free for the proxy's log.

## 1. Build

```sh
cd cui-proxy
make build                      # -> bin/cui-proxy
bin/cui-proxy version
```

## 2. Minimal config

Use a throwaway directory:

```sh
mkdir -p /tmp/cui-verify && cd /tmp/cui-verify
cat > config.yaml <<'YAML'
listen: 127.0.0.1:8787
ui: { listen: 127.0.0.1:8788 }
upstreams:
  codex: https://chatgpt.com/backend-api/codex
policy: { on_error: block }
vault: { mode: memory }
rules:
  - name: builtin
    type: builtin
YAML
/path/to/cui-proxy/bin/cui-proxy check-config -config config.yaml   # expect: ok: 1 rules
```

## 3. Start the proxy

```sh
/path/to/cui-proxy/bin/cui-proxy serve -config config.yaml
```

Expect startup lines like `proxy listening on http://127.0.0.1:8787`,
`dashboard on http://127.0.0.1:8788/` and `upstream: https://chatgpt.com`.
Open <http://127.0.0.1:8788/> in a browser.

## 4. Point a throwaway Codex run at the proxy

In a second terminal, from an empty scratch directory:

```sh
mkdir -p /tmp/cui-verify/work && cd /tmp/cui-verify/work
codex exec \
  -c 'model_provider="cui-proxy"' \
  -c 'model_providers.cui-proxy={name="cui-proxy",base_url="http://127.0.0.1:8787/v1",wire_api="responses",requires_openai_auth=true}' \
  "say hi" </dev/null
```

`</dev/null` matters: without it `codex exec` waits for stdin.

## 5. What to look for (first prompt)

On success, Codex prints a short greeting. Then check:

- The dashboard's request feed has a `POST /v1/responses` with status 200 and
  no `blocked` reason. One or two `GET /v1/models` may appear before it (they
  are passed through).
- The proxy log has no `request blocked` or `upstream request failed` line,
  and contains no token, account id, prompt text or URL query.
- `replacements` stays near 0 for this prompt. The Codex system prompt can
  still match a rule; if so, the feed shows the rule names and counts only.

If something fails, check these in order:

| Symptom | Meaning / what to change |
|---|---|
| Codex prints a 404 from the upstream, or the feed shows status 404 | The path mapping is wrong. The proxy sends `POST <upstream base> + /responses`, i.e. `https://chatgpt.com/backend-api/codex/responses`. Find the path Codex uses without the proxy (e.g. `RUST_LOG=codex_core=debug codex exec "say hi" </dev/null` against the default provider, and look for the request URL; debug logs can contain auth headers, so do not share them and delete them afterwards). Then change either `upstreams.codex` in the config (the base) or the route's `UpstreamPath` in `internal/adapter/codex/codex.go` (`Match`), and the "Point Codex at the proxy" section of [configuration.md](configuration.md) to match. `GET /v1/models` maps to `<base>/models` the same way. |
| 401 / 403 from the upstream | The auth headers did not arrive. Check that `requires_openai_auth=true` is in the `-c` flag. The proxy forwards `Authorization` and `Chatgpt-Account-Id` untouched. |
| 422 `header` block | Codex `workspaces` and `cwd` metadata is sanitized using the active rules. Strict/block rules, malformed or oversized metadata, and detected values in unchanged metadata fields, other headers or queries still block. Inspect the reported kind and rule; do not disable email detection to allow SSH Git remotes. |
| 422 `sanitize` / `unrestorable` block | The body could not be sanitized. The error message and the log line name a kind and the rule, e.g. `reason=sanitize kind=pool_exhausted rule=builtin.ipv4`; see [Block diagnostics](block-diagnostics.md). No value is ever printed. |
| 403 `route` on an unexpected POST path, typically after a long session | Codex called an endpoint the adapter does not know, most likely remote compaction (it advertises `X-Codex-Beta-Features: remote_compaction_v2`; the path may be e.g. `/v1/responses/compact`). The feed shows the path as `(unmatched)`; find the real path with the debug log as in the 404 row. Add it as a `KindSanitize` route in `Match` in `internal/adapter/codex/codex.go` (its body is the same kind of history, so the request sanitizer applies). Note that a successful non-SSE response is refused until the adapter supports safe restoration for that format. |
| 502 `upstream_redirect` | The upstream answered with a 3xx. The proxy never relays redirects, so the base URL is probably wrong (see the 404 row). |
| 502 `stream_rewrite` or a stream cut mid-answer | An SSE event the rewriter could not handle. Note the event names from the open items below and report them. |

## 6. Second prompt: made-up IP and OCID

```sh
codex exec \
  -c 'model_provider="cui-proxy"' \
  -c 'model_providers.cui-proxy={name="cui-proxy",base_url="http://127.0.0.1:8787/v1",wire_api="responses",requires_openai_auth=true}' \
  "Reply with exactly this text and nothing else: host 10.20.30.15 instance ocid1.instance.oc1.phx.anyhqljrabcdefghijklmnop" </dev/null
```

Expect:

- The dashboard counts at least `builtin.ipv4: 1` and `builtin.ocid: 1`
  (metrics and the feed entry). `vault_size` grows by 2 or more.
- Codex's answer shows the ORIGINAL made-up values (`10.20.30.15` and the
  ocid). The model only ever saw synthetics (`192.0.2.x` / `198.51.100.x` /
  `203.0.113.x` and an `ocid1.…ocsynth…` value), and the proxy restored them.
  If the answer shows a synthetic instead, restoration missed an event type.
  Note which one (see open items).
- The proxy log shows no IP or OCID, only rule names and counts.

Optional tool-call check (the main restore path): ask
`"Run: echo 10.20.30.15"` in a sandboxed run. The command Codex executes,
and the `function_call_output` it sends back next turn, must show the
original value locally, while the dashboard counts the value again on the
second request (the resent history is sanitized again).

### 6a. Tool output with addresses (downgrade and addr.port)

In a sandboxed run, ask Codex to run a command whose output holds an IPv6
address, a `label:fe80::1` form and tcpdump-style text (all made up):

```sh
codex exec \
  -c 'model_provider="cui-proxy"' \
  -c 'model_providers.cui-proxy={name="cui-proxy",base_url="http://127.0.0.1:8787/v1",wire_api="responses",requires_openai_auth=true}' \
  "Run: printf 'addr fd12:3456:789a::5 id:fe80::1\nIP 10.20.30.15.443 > 10.20.30.16.51234: Flags [S]\n' and then tell me which addresses appear" </dev/null
```

Expect:

- No `unrestorable` or `sanitize` block in the feed or the log, on this
  request or on the following ones (the history with the tool output is
  resent every turn).
- The `Downgraded` card counts at least 1 (`id:fe80::1` is masked as a
  `REDACTED-<8hex>` token because an address synthetic glued to `id:` would
  not restore) and grows again on each later request of the same session.
- The tcpdump endpoints are counted under `builtin.secret` (each
  `addr.port` token is masked whole); `fd12:3456:789a::5` under `builtin.ipv6`.
- Codex's answer and the local command output show the original values.

### 6b. Long session (remote compaction)

Run one interactive session (plain `codex` with the same `-c` flags) long
enough for Codex to compact its context, for example by asking it to read
and summarize several large files in the scratch directory. Watch the feed
for any request other than `POST /v1/responses` and `GET /v1/models`:

- A 403 `route` with path `(unmatched)` means a compaction (or other)
  endpoint is not routed; see the 403 row in the table above.
- If compaction goes through `POST /v1/responses`, note it under the open
  items.

Stop the proxy with Ctrl-C. Memory mode discards every mapping.

### 6c. Decision log

With the proxy stopped (Ctrl-C), check that every request of the session was
recorded and that the chain verifies. Use the same `-config` as `serve`:

```sh
/path/to/cui-proxy/bin/cui-proxy audit verify -config config.yaml   # expect: Result: OK, exit 0
```

Expect `Records checked` to be at least the number of requests in the feed, and
the decision log (default `~/.local/state/cui-proxy/decisions.jsonl`, mode
`0600`) to contain no IP, OCID, prompt text, header or path, only rule names
and counts. Copy a `request_id` from a blocked request's error message
(or from the `X-Cui-Proxy-Request-Id` header) and run
`cui-proxy explain -config config.yaml <id>`: it should name the reason and
the rule. Then make a copy of the log, delete one middle line from the copy
and run `cui-proxy audit verify -file <copy>`: it must report the first
break at that line and exit 1. See [decision-log.md](decision-log.md).

## 7. Record the result

Write down, for this repo:

1. Whether `https://chatgpt.com/backend-api/codex` + `/responses` worked. If
   not, the base and path that did, plus the code/docs change made.
2. Codex CLI version (`codex --version`).
3. Answers to the open items below, including the compaction path (6b) and
   the `X-Codex-Turn-Metadata` shape.

## Coverage notes and follow-up checks

The live smoke established the core route and restoration path; broader
sessions can exercise the additional cases below:

- **Confirmed routing**: `/v1/responses` -> `/backend-api/codex/responses` and
  `/v1/models` -> `/backend-api/codex/models` returned HTTP 200 in the live smoke.
- **Trailing-delta fields**: real delta events carry `sequence_number` and
  `obfuscation`. When the restorer re-chunks a held-back delta, it reuses the
  last delta event's fields. Codex accepted the text and function-tool streams
  in the live smoke; continue watching sequence handling in longer sessions.
- **Event names**: reasoning-summary events
  (`response.reasoning_summary_text.delta`,
  `response.reasoning_summary_part.done`) and the custom-tool input event
  (`response.custom_tool_call_input.delta`, used by `apply_patch`). Any other
  `*.delta` event passes through unrestored, so a synthetic in it would reach
  the user as is.
- **Redirects**: whether Codex follows redirects (the proxy turns every 3xx
  except 304 into a 502 anyway).
- **Header shapes**: the exact names and contents of `Session-Id`,
  `Thread-Id`, `X-Codex-Turn-Metadata` and `X-Codex-Beta-Features`. These are
  inspected and can cause a `header` block; `workspaces` and `cwd` in turn
  metadata are sanitized. Additional client shapes need synthetic verification.
- **Additional transports and content**: non-streaming, websocket, compaction
  and image requests are outside the recorded smoke coverage. Image parts are blocked by design. Remote compaction
  (`remote_compaction_v2`) probably uses its own endpoint, which today
  returns 403 `route` (step 6b); a route is added only once the real path
  is confirmed.
