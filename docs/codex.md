# Connect Codex

There are two configuration files in this setup. **The proxy YAML controls
inspection. Codex's TOML controls routing.** A Codex `-c` option cannot change
the proxy's policy or turn a blocking rule into a sanitizing rule.

| Setting | Example | Meaning |
|---|---|---|
| Proxy `listen` | `localhost:8787` | Local address accepting model requests |
| Codex provider `base_url` | `http://localhost:8787/v1` | Where Codex sends those requests |
| Proxy `upstreams.codex` | `https://chatgpt.com/backend-api/codex` | Where inspected requests go next |
| Dashboard `ui.listen` | `localhost:8788` | Browser UI; never use this as Codex's model URL |

Start the proxy first. Keep the `/v1` suffix in the Codex URL; the adapter maps
`POST /v1/responses` to `/responses` beneath the configured upstream base.
It also supports `GET /v1/models`. Other routes are refused.

## Try it for a single session

This uses your existing ChatGPT login and model selection:

```sh
codex -c 'model_provider="cui-proxy"' \
  -c 'model_providers.cui-proxy={name="cui-proxy",base_url="http://localhost:8787/v1",wire_api="responses",requires_openai_auth=true,supports_websockets=false}'
```

The macOS menu-bar helper also provides **Copy Codex command** for its configured
listener. Command-line overrides apply to that invocation; they do not rewrite
your TOML. To use the tutorial instance, substitute port `18787`.

## Make the proxy your default

Back up `~/.codex/config.toml` if it exists, then merge this snippet into that
**user-level** file. With a custom `CODEX_HOME`, use its `config.toml` instead.
Do not replace unrelated settings or create a second copy of an existing table.

```toml title="Merge into config.toml"
--8<-- "examples/codex-chatgpt.toml"
```

[Download the TOML snippet](examples/codex-chatgpt.toml).

!!! tip "TOML table placement matters"
    Put `model_provider = "cui-proxy"` at the top level, before any `[table]`
    header. If you put it below `[model_providers.cui-proxy]`, it becomes a
    provider-table field instead of selecting your provider.

Keep any existing `model` setting that works with your account. Provider
selection does not grant access to a model. Stop and reopen Codex after editing,
then [verify a fabricated request](testing.md).

## Use a named Codex profile

If you want an explicitly selected profile, define the provider at user level
and put its selection in a profile. This is a **Codex profile**, separate from
the proxy's `policy.profile`.

```toml title="Merge at table boundaries; keep your current top-level settings"
[model_providers.cui-proxy]
name = "cui-proxy"
base_url = "http://localhost:8787/v1"
wire_api = "responses"
requires_openai_auth = true
supports_websockets = false

[profiles.cui]
model_provider = "cui-proxy"
```

```sh
codex --profile cui
```

With only this profile setup, an invocation that does not select the profile
can use your other default provider and bypass the proxy. Check the active
route rather than assuming every Codex session is covered.

## What the provider fields mean

| Field | Why it is here |
|---|---|
| `name` | Human-readable provider label |
| `base_url` | Local proxy endpoint, including `/v1` |
| `wire_api = "responses"` | Selects the protocol implemented by the adapter |
| `requires_openai_auth = true` | Uses OpenAI authentication for this ChatGPT-login setup |
| `supports_websockets = false` | Uses the HTTP/SSE transport supported by cui-proxy |

These Codex fields are documented in the
[official configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference).
Managed client settings may constrain provider selection. The repository's
[live verification](verification.md) names the exact client and backend tested;
it does not establish compatibility with every Codex release or desktop host.

## API-key backend (separate configuration)

For an API-key setup, both ends must agree. In the **proxy YAML**, replace the
existing upstream block with:

```yaml title="Partial proxy YAML edit"
upstreams:
  codex: https://api.openai.com/v1
```

Restart the proxy: upstream changes are not hot-reloadable. In the **Codex user
TOML**, use an environment-backed provider instead of the ChatGPT-login provider:

```toml title="Alternative provider; do not combine authentication modes"
[model_providers.cui-api]
name = "cui-proxy API"
base_url = "http://localhost:8787/v1"
wire_api = "responses"
env_key = "OPENAI_API_KEY"
requires_openai_auth = false
supports_websockets = false

[profiles.cui-api]
model_provider = "cui-api"
```

Supply `OPENAI_API_KEY` through your existing secret-management mechanism, choose
a model available to that API account, then run `codex --profile cui-api`.
Never put the key in proxy YAML, a dictionary, a screenshot, or a public issue.
This is an alternate configuration based on the Responses protocol; the dated
ChatGPT-login smoke test is **not** evidence of an API-key live test. Run your
own fabricated end-to-end check before relying on it.

## Things routing does not change

The proxy passes credential headers to the upstream unchanged. It does not sign
you in, store credentials, grant tool execution permissions, configure SSH, or
intercept every network connection from your computer. Shell tools, Git, MCP
servers, browser traffic, and other clients have their own network paths.
Text they return to Codex is inspected when it enters a supported model request.

If the proxy is stopped, requests routed to it fail. There is no proxy-side
direct fallback. To deliberately stop using the proxy, restore your previous
provider selection or launch without the temporary override; that traffic
will no longer be inspected by cui-proxy.

See [Life of a request](architecture.md), [Git/SSH troubleshooting](troubleshooting.md),
and [supported overrides](configuration.md#overrides-that-exist-and-ones-that-do-not).
