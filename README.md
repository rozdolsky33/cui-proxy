# cui-proxy

A local, fail-closed proxy for Codex CLI. It sits between Codex and the model
backend, replaces sensitive values in each request (IP addresses, OCIDs,
emails, SSNs, card numbers, tokens, and your own dictionary of host names)
with synthetic values from reserved ranges (RFC 5737 / RFC 3849 addresses,
`.example` host and mail names), and swaps the real values back into the
streamed response so tool calls run against your real systems.

> **Important: this tool reduces risk; it is not a compliance control.**
> It does not make it permissible to use a cloud model with CUI or other
> sensitive government data. Detection is pattern- and dictionary-based and
> will miss things. **You still need GOVSEC / ISSM approval** for your use
> case before sending any such data through it.

What it is not: not a DLP product, not a guarantee that nothing sensitive
leaves the machine, not a MITM proxy (Codex is pointed at it explicitly),
and it never stores or alters your upstream credentials.

## Install

### Homebrew (macOS and Linux)

```sh
brew tap rozdolsky33/tap
brew install cui-proxy
```

(One line: `brew install rozdolsky33/tap/cui-proxy`.) Upgrade with
`brew update && brew upgrade cui-proxy`.

### curl

```sh
curl -fsSL https://raw.githubusercontent.com/rozdolsky33/cui-proxy/main/install.sh | sh
```

The installer downloads the release archive for your OS and CPU, **verifies
its sha256 against the release's `checksums.txt`**, and installs the single
`cui-proxy` binary into `/usr/local/bin` if you can write there, otherwise
`~/.local/bin`. It never uses `sudo` and touches nothing else. Options:
`INSTALL_DIR=/some/dir` and `CUI_PROXY_VERSION=v0.1.0`.

Prefer to read it first?

```sh
curl -fsSL https://raw.githubusercontent.com/rozdolsky33/cui-proxy/main/install.sh -o install.sh
less install.sh && sh install.sh
```

### Manual download or from source

Download an archive and `checksums.txt` from the
[releases page](https://github.com/rozdolsky33/cui-proxy/releases), check it
with `shasum -a 256 -c --ignore-missing checksums.txt`, and put `cui-proxy` on
your `PATH`. To build from source, see [Build](#build).

Archives exist for macOS and Linux on amd64 and arm64. Check the install with
`cui-proxy version`.

## Quick start

```sh
cui-proxy init                                   # writes ~/.config/cui-proxy/{config.yaml,hosts.txt}
$EDITOR ~/.config/cui-proxy/hosts.txt            # your own sensitive host names, one per line
cui-proxy check-config -config ~/.config/cui-proxy/config.yaml
cui-proxy serve -config ~/.config/cui-proxy/config.yaml
```

Then, in another terminal, point Codex at the proxy for one run (a permanent
setup is in [Point Codex at the proxy](#point-codex-at-the-proxy)). Use a
**synthetic prompt** the first time:

```sh
codex -c 'model_provider="cui-proxy"' \
      -c 'model_providers.cui-proxy={name="cui-proxy",base_url="http://127.0.0.1:8787/v1",wire_api="responses",requires_openai_auth=true}'
```

- Dashboard: <http://127.0.0.1:8788/> (live feed, metrics, config; never shows
  real values).
- See what the rules do to some text without running Codex:
  `echo 'ssh admin@10.20.30.15' | cui-proxy test-rules -config ~/.config/cui-proxy/config.yaml`
- Stop the proxy with Ctrl-C.

**Before you rely on it:** the upstream mapping has not been tested against the
real ChatGPT backend yet. Run the manual smoke in
[docs/verification.md](docs/verification.md), and read
[Known limitations](#known-limitations--behavior).

## Uninstall

```sh
brew uninstall cui-proxy && brew untap rozdolsky33/tap    # Homebrew
rm "$(command -v cui-proxy)"                              # curl install
rm -r ~/.config/cui-proxy                                 # config and dictionary, if you want them gone
```

## Build

Requires Go 1.26.

```sh
make build            # -> bin/cui-proxy
make test lint        # go test -race, go vet, staticcheck
go build -ldflags "-X main.version=v0.1.0" -o bin/cui-proxy ./cmd/cui-proxy
```

## Run

```sh
bin/cui-proxy init            # or: cui-proxy init, when installed
# then edit ~/.config/cui-proxy/config.yaml and hosts.txt
bin/cui-proxy check-config -config ~/.config/cui-proxy/config.yaml
bin/cui-proxy serve -config ~/.config/cui-proxy/config.yaml
```

Commands:

| Command | Purpose |
|---|---|
| `init [-dir PATH] [-force]` | Write a starter `config.yaml` and `hosts.txt` (mode 0600) into `$XDG_CONFIG_HOME/cui-proxy`, else `~/.config/cui-proxy`. Never overwrites without `-force`. |
| `serve -config PATH [-adapter codex]` | Run the proxy (default `127.0.0.1:8787`) and the dashboard (default `127.0.0.1:8788`). |
| `check-config -config PATH` | Validate the config, rule files and upstream URLs; prints `ok: <N> rules`, exits 1 on error. |
| `test-rules -config PATH` | Read stdin (max 8 MiB), print the sanitized text to stdout and a rule/class/count table to stderr. Uses a throwaway vault. |
| `version` | Print the version. |

Exit codes: 0 ok, 1 failure, 2 usage error.

The upstream for ChatGPT-login Codex is configured in the proxy config:

```yaml
upstreams:
  codex: https://chatgpt.com/backend-api/codex
```

The upstream must be `https` (plain `http` is accepted only for a loopback
host, for testing). See `examples/config.yaml` for the full, commented
config. Relative dictionary and vault paths resolve against the config
file's directory.

## Point Codex at the proxy

In `~/.codex/config.toml` (or a profile):

```toml
model_provider = "cui-proxy"

[model_providers.cui-proxy]
name = "cui-proxy"
base_url = "http://127.0.0.1:8787/v1"
wire_api = "responses"
requires_openai_auth = true
```

Or for a single run:

```sh
codex -c 'model_provider="cui-proxy"' \
      -c 'model_providers.cui-proxy={name="cui-proxy",base_url="http://127.0.0.1:8787/v1",wire_api="responses",requires_openai_auth=true}'
```

`requires_openai_auth = true` makes Codex send its ChatGPT login headers to
the proxy, which forwards them unchanged to the upstream.

**Unverified:** the upstream mapping (`/v1/responses` ->
`https://chatgpt.com/backend-api/codex/responses`) has not been tested
against the real service yet. Run the manual smoke in
[docs/verification.md](docs/verification.md) first. It also says what to
change if the mapping is wrong.

## Dashboard

<http://127.0.0.1:8788/> shows the live request feed (rule names and counts,
never values), metrics, the effective config, a "test a string" box, and a
reload button. It is bound to localhost and refuses non-localhost `Host`
headers and cross-origin POSTs.

## Hot reload

`kill -HUP <pid>` or the dashboard's reload button re-reads the config file
and the dictionary files. The new rules apply only if the whole config is
valid; otherwise the running rules stay and the error is shown. Requests
already in flight finish with the rules they started with. The vault (and so
every existing mapping) is kept.

Applied on reload: `rules` (and dictionary contents), `policy.on_error`,
`policy.max_body_bytes`.
Need a restart (a reload logs `WARN restart required for: ...` and keeps the
running values): `listen`, `ui.listen`, `upstreams`, `vault.*`,
`allow_non_loopback`.

`SIGINT` / `SIGTERM` stop the proxy with a 10 s drain for in-flight requests;
a second signal closes at once.

## Vault

- `vault.mode: memory` (default): mappings live only in the process, under a
  random key that never touches disk. After a restart, synthetic values in
  history that Codex resends are unknown to the new vault and pass through
  unrestored.
- `vault.mode: file`: mappings are stored encrypted (AES-256-GCM, file format
  `CUIV2`) in `vault.file`, keyed by `vault.key_file` (32 bytes, created with
  mode 0600 on first run; a key file with wider permissions is refused). The
  vault is saved every 30 s when it changed and on shutdown. A vault file that
  cannot be read (wrong key, missing key file, older format, corruption) stops
  startup; it is never silently replaced. To start over, remove the vault
  file yourself.

### Synthetic address pool

IPv4 (and CIDR) synthetics come from the 762 RFC 5737 documentation
addresses (`192.0.2.x`, `198.51.100.x`, `203.0.113.x`). When those are
used up, the vault falls back to an overflow pool of 130,048 addresses in
`198.18.0.0/15` (RFC 2544 benchmarking).

- Exhaustion: when no synthetic is left, the request is blocked (HTTP 422,
  code `sanitize`, kind `pool_exhausted`; nothing is forwarded; see
  [Block diagnostics](#block-diagnostics)). Mappings are never
  freed: the vault keeps growing for the life of the process (and of the
  vault file in file mode), and even a blocked request may have used up
  addresses.
- Under `policy.on_error: passthrough`, exhaustion is a sanitize failure, so
  the ORIGINAL request, with its real IPs, would be forwarded.
- `198.18.0.0/15` can carry real traffic (lab and benchmark networks, and
  proxies or VPNs with a "fake-IP" mode). To keep a real address there from
  being confused with a synthetic: once the vault has seen any real address
  in that range, it stops coining overflow addresses (it then fails closed
  after the 762 RFC 5737 addresses); and a request containing an address
  that equals an existing overflow synthetic is blocked, because the proxy
  cannot tell whether it is real. Leading-zero spellings
  (`198.018.001.001`) count as the same address, and an IPv4/CIDR value that
  cannot be parsed also disables the overflow pool. In file mode this
  decision is saved with the vault and survives restarts. Each exact
  spelling still gets its own synthetic and restores byte for byte (some
  tools read leading-zero octets as octal); no synthetic ever equals any
  spelling of a known real. Residual risk: if your real traffic uses
  that range, expect blocks. A model reply that names a real
  `198.18.0.0/15` address the proxy has issued as a synthetic is restored to
  the wrong value.

## Block diagnostics

A blocked request is answered with an OpenAI-style JSON error whose `code`
is the block reason (`route`, `body_too_large`, `compressed`, `header`,
`sanitize`, `unrestorable`, `bad_request`, `internal`). When the body could
not be sanitized, the message also names a kind and, when one span caused
it, the detector rule (a name from your config, never a value):

```
cui-proxy: request blocked (reason=sanitize kind=pool_exhausted rule=builtin.ipv4)
```

The same appears in the proxy log as
`request blocked (reason=... kind=... rule=... class=...)`, and in the
dashboard feed as `sanitize:<kind>` with a `blocked:<rule>` count. The
metrics count blocks by reason only.

| kind | meaning | what to do |
| --- | --- | --- |
| `pool_exhausted` | no synthetic left for the class (see Synthetic address pool) | restart with a fresh vault (memory mode) or remove the vault file |
| `ambiguous` | a value equals an issued overflow synthetic (or a zero-padded spelling of one) | see Synthetic address pool |
| `pseudonym` | no synthetic can be made for the value (e.g. an invalid CIDR from a custom rule) | fix the rule's pattern or `synth` class |
| `unrestorable` | the sanitized text would not restore even after downgrading (code `unrestorable`) | rare: report the rule and kind from the log line |
| `bad_body` | not a single JSON object, invalid UTF-8, or an image/file/audio part | expected for binary input |
| `depth` | JSON nested deeper than 64 levels | expected for pathological input |
| `other` | any other failure | check the log line |

A value whose synthetic would not restore where it stands (for example
`id:fe80::1`, `ID123-45-6789` or `x4111 1111 1111 1111`, where the
address/number synthetic would be glued to a letter or hex digit) is not
blocked: it is masked as a `REDACTED-<8hex>` token instead and counted as
`downgraded` in the metrics.

## Known limitations / behavior

- Credential headers (`Authorization`, `Chatgpt-Account-Id`, cookies) are
  forwarded unscanned and are never logged or stored.
- Other request headers and the query string are scanned, never rewritten:
  any detected value BLOCKS the request (HTTP 422, code `header`).
  `X-Codex-Turn-Metadata` carries the working directory (cwd; see
  docs/verification.md), so a dictionary entry that matches a part of that
  path blocks every request made from that directory with `header`. Work
  from a directory whose path holds no dictionary term. (Sanitizing that
  header like the body may follow once a real session confirms its shape;
  today it is only scanned.)
- Only streamed (SSE) responses are restored. A non-streaming
  (`stream: false`) response is relayed as is, with synthetics in it.
- Unseparated card-like digit runs and long numeric ids become `REDACTED`
  tokens rather than format-preserving synthetics. So do tcpdump-style
  `addr.port` tokens (`10.20.30.15.443`, `2001:db8::1.443`, port included)
  and values glued where their synthetic would not restore (see
  [Block diagnostics](#block-diagnostics)). Five single-digit dotted
  components (`1.2.3.4.5`) are taken for a version number and not masked.
- Detection is pattern-based: the model receives whatever the patterns and
  your dictionary miss. This reduces exposure; it does not bound it.
- Hostnames that merely encode an address, such as
  `ip-10-20-30-15.ec2.internal`, are not IP literals and are not detected.
  Add your own regex or dictionary rule for them.
- Usernames and local paths (for example in the `<environment_context>`
  block Codex sends: cwd, shell, home directory) are not detected by the
  builtin set. Put the ones that matter in a dictionary.
- Memory-mode restarts lose all mappings (see Vault).
- `tools[].name` and some protocol identifier fields are not scanned, so
  that the protocol keeps working; unknown `include` values are scanned, not
  skipped.
- The builtin set has no AWS access key detector; `examples/config.yaml`
  adds one as a regex rule (`aws-keys`, `\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`).
- Upstream 3xx responses other than 304 become 502: a redirect (and its
  `Location`) is never relayed to the client.
- Any sanitize failure blocks the request (fail closed) unless
  `policy.on_error: passthrough`, which forwards the ORIGINAL request and is
  logged as UNSAFE at startup (and by `check-config`).
- Listeners are loopback-only unless `allow_non_loopback: true`; the bound
  address is checked after binding.
- The dashboard is localhost-only and never shows real values: only rule
  names, counts, paths and block reasons.

## Support

- Questions, bugs and feature requests: open a
  [GitHub issue](https://github.com/rozdolsky33/cui-proxy/issues). **Never paste
  real sensitive data (CUI, customer identifiers, credentials, tokens) into an
  issue or log excerpt;** use made-up values of the same shape.
- Security problems (a way around the protections, a leak): report privately,
  see [SECURITY.md](SECURITY.md).
- This is best-effort, community-style software released under the
  [MIT license](LICENSE), with no warranty. It is not an approved compliance
  control.

## Releasing (maintainers)

```sh
git tag v0.1.0 && git push origin v0.1.0
```

`.github/workflows/release.yml` runs vet and the race tests, builds the four
archives with `scripts/package.sh`, and publishes them with `checksums.txt` as
a GitHub release. It then updates the Homebrew tap if the repository secret
`TAP_GITHUB_TOKEN` exists (a fine-grained token with `contents: write` on
`rozdolsky33/homebrew-tap`). Without that secret, update the tap yourself after
the release is published:

```sh
scripts/update-tap.sh v0.1.0      # needs `gh` logged in with push access to the tap
```

`scripts/package.sh <version>` builds the same archives locally into `dist/`
for a dry run. The formula is always generated from the checksums of the
published release (`scripts/make-formula.sh`), never from a local build.
