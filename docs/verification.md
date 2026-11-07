# Manual verification against the real Codex CLI

**Status: NOT executed.** These steps were written during the build but were
not run. The build never contacted chatgpt.com or openai.com and never used
real credentials. All automated tests run against `httptest` fake upstreams
(`cmd/cui-proxy/e2e_test.go`, `internal/proxy`). So **the upstream mapping
`/v1/responses` -> `https://chatgpt.com/backend-api/codex/responses` is
unverified.** It is the open item left by
[the redirect spike](spikes/2026-11-06-codex-redirect-spike.md).

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
| Codex prints a 404 from the upstream, or the feed shows status 404 | The path mapping is wrong. The proxy sends `POST <upstream base> + /responses`, i.e. `https://chatgpt.com/backend-api/codex/responses`. Find the path Codex uses without the proxy (e.g. `RUST_LOG=codex_core=debug codex exec "say hi" </dev/null` against the default provider, and look for the request URL; debug logs can contain auth headers, so do not share them and delete them afterwards). Then change either `upstreams.codex` in the config (the base) or the route's `UpstreamPath` in `internal/adapter/codex/codex.go` (`Match`), and the README's Codex section to match. `GET /v1/models` maps to `<base>/models` the same way. |
| 401 / 403 from the upstream | The auth headers did not arrive. Check that `requires_openai_auth=true` is in the `-c` flag. The proxy forwards `Authorization` and `Chatgpt-Account-Id` untouched. |
| 422 `header` block | A non-credential request header (e.g. `X-Codex-Turn-Metadata`, which carries the cwd) matched a rule. Headers are never rewritten, so this always blocks. Run from a directory whose path holds nothing sensitive. |
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

Stop the proxy with Ctrl-C. Memory mode discards every mapping.

## 7. Record the result

Write down, for this repo:

1. Whether `https://chatgpt.com/backend-api/codex` + `/responses` worked. If
   not, the base and path that did, plus the code/README change made.
2. Codex CLI version (`codex --version`).
3. Answers to the open items below.

## Open items to confirm against a real session

These are assumptions in the adapter that only a real session can confirm:

- **Upstream mapping**: `/v1/responses` -> `/backend-api/codex/responses` and
  `/v1/models` -> `/backend-api/codex/models` (query string kept).
- **Trailing-delta fields**: real delta events carry `sequence_number` and
  `obfuscation`. When the restorer re-chunks a held-back delta, it reuses the
  last delta event's fields. Check that Codex accepts this (no error about
  sequence numbers).
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
  scanned and can cause a `header` block.
- **Non-streaming / websocket / compaction / image requests**: not observed in
  the spike. Image parts are blocked by design.
