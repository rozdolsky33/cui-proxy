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


**Jump to:** [Quick start (macOS)](#quick-start-macos-menu-bar) ·
[Quick start (terminal)](#quick-start-terminal-linux-or-no-menu-bar) ·
[Everyday use](#everyday-use) · [Menu-bar details](#menu-bar-app-details) ·
[Install options](#install-all-options) · [Commands](#commands) ·
[Policy profiles](#policy-profiles-sanitize-or-strict) ·
[Block diagnostics](#block-diagnostics) · [Known limitations](#known-limitations--behavior) ·
[Uninstall](#uninstall) · [Support](#support)

## Quick start (macOS menu bar)

The easiest way to run it: no terminal stays open, and one click opens the
dashboard. Four steps, about two minutes.

**1. Install**

```sh
brew install rozdolsky33/tap/cui-proxy
```

**2. Create your starter config**

```sh
cui-proxy init
```

Then put your own sensitive host names in `~/.config/cui-proxy/hosts.txt`, one
per line (the file explains the format).

**3. Install the menu-bar app**

```sh
cui-proxy menubar install
```

This compiles a tiny native app on your Mac (it needs Apple's Command Line
Tools: `xcode-select --install`), starts it, and starts the proxy for you. A
**shield icon** appears in the menu bar.

**4. Use it**

Click the shield:

- **Open Dashboard** shows live requests, metrics and the active config.
  Try the "test a string" box with `ssh admin@10.20.30.15`: you get a fake
  address back.
- **Copy Codex command**, then paste it in a terminal to run Codex through
  the proxy for one session. Use a synthetic prompt the first time.
- Tick **Start at Login** once and it is always there.

That is the whole setup. Everything else on this page is optional detail.
Not on a Mac, or prefer a terminal? See the next section.

> **Before you rely on it:** the upstream mapping has not been tested against
> the real ChatGPT backend yet. Run the manual smoke in
> [docs/verification.md](docs/verification.md), and read
> [Known limitations](#known-limitations--behavior).

If the icon shows `!`, or Codex gets an error, see
[If the first Codex run fails](#if-the-first-codex-run-fails).


## Quick start (terminal, Linux or no menu bar)

```sh
brew install rozdolsky33/tap/cui-proxy              # or the curl installer below
cui-proxy init                                      # writes ~/.config/cui-proxy/{config.yaml,hosts.txt}
$EDITOR ~/.config/cui-proxy/hosts.txt               # your own sensitive host names, one per line
cui-proxy check-config -config ~/.config/cui-proxy/config.yaml
cui-proxy serve -config ~/.config/cui-proxy/config.yaml     # leave this running
```

Then, in another terminal, point Codex at the proxy for one run (a permanent
setup is in [Point Codex at the proxy](#point-codex-at-the-proxy)). Use a
**synthetic prompt** the first time:

```sh
codex -c 'model_provider="cui-proxy"' \
      -c 'model_providers.cui-proxy={name="cui-proxy",base_url="http://127.0.0.1:8787/v1",wire_api="responses",requires_openai_auth=true}'
```

Dashboard: <http://127.0.0.1:8788/>. Stop the proxy with Ctrl-C.

### If the first Codex run fails

Look at the dashboard's *Blocked* column and the `cui-proxy` terminal (or
**Open Log** in the menu-bar app):

| You see | Likely cause |
|---|---|
| Nothing in the dashboard | Codex is not reaching the proxy: is it running, and did you pass the `-c` flags? |
| `403 route` or a 404 | The upstream path guess is wrong (it has not been tested against the real ChatGPT backend). See the symptom table in [docs/verification.md](docs/verification.md#5-what-to-look-for-first-prompt). |
| `422 request blocked ...` | A safety check refused the request; the message names the rule and kind, never the value. See [Block diagnostics](#block-diagnostics). |


## Everyday use

| I want to... | Do this |
|---|---|
| Run Codex through the proxy | Menu bar: **Copy Codex command**, paste it. Or set it permanently: [Point Codex at the proxy](#point-codex-at-the-proxy). |
| See what is happening | Menu bar: **Open Dashboard**, or <http://127.0.0.1:8788/>. |
| Add a sensitive name | Add a line to `~/.config/cui-proxy/hosts.txt`, then **Reload rules** (menu bar) or `kill -HUP <pid>`. No restart. |
| Preview what the rules do | `echo 'ssh admin@10.20.30.15' \| cui-proxy test-rules -config ~/.config/cui-proxy/config.yaml` |
| Check my config is valid | `cui-proxy check-config -config ~/.config/cui-proxy/config.yaml` |
| Refuse restricted data instead of masking it | `cui-proxy policy init -profile strict`, see [Policy profiles](#policy-profiles-sanitize-or-strict). |
| See what is and is not enforced | `cui-proxy policy check -config ~/.config/cui-proxy/config.yaml` |
| Find out why a request was blocked | The `422` message names the rule, never the value: [Block diagnostics](#block-diagnostics). |
| Check the state of the menu-bar app | `cui-proxy menubar status` |
| Upgrade | `brew update && brew upgrade cui-proxy`, then **Quit** and reopen the menu-bar app. |
| Remove it | [Uninstall](#uninstall) |

All commands and flags: [Commands](#commands).


## Menu-bar app details

Look for the shield in the menu bar and click it:

| Menu item | What it does |
|---|---|
| **Open Dashboard** | Opens the dashboard in your browser (greyed out while the proxy is not running). |
| **Copy Codex command** | Puts the `codex -c ...` command for this proxy on the clipboard. |
| **Start / Stop proxy** | Starts or stops the proxy the app runs. |
| **Reload rules** | Re-reads the config and dictionary without a restart. |
| **Open Config Folder / Open Log** | Opens `~/.config/cui-proxy` or `~/Library/Logs/cui-proxy/proxy.log`. |
| **Start at Login** | Starts the app (and so the proxy) when you log in. Tick it once; untick to remove. |
| **Create starter config...** | Shown only when there is no config yet. |
| **Quit** | Stops the proxy the app started, then exits the app. |

The icon shows the state: a shield with a check (running), plain shield
(starting), slashed shield (stopped), shield with `!` (needs attention: no
config, `cui-proxy` not found, or it crashed; the menu says which). A crashed
proxy is restarted automatically, up to three restarts inside any one minute,
so a crash loop stops. If something else already serves on the configured
ports (for example a terminal running `cui-proxy serve`), the menu says
"Running (started elsewhere)": the app does not start a second one and leaves
yours alone when you quit. If the port is taken by something that is not
cui-proxy, or by another user, the icon shows `!` and the menu says so.

Things to know:

- It needs Apple's Command Line Tools for the Swift compiler
  (`xcode-select --install`). The app is built on your own Mac and signed
  ad hoc, so macOS does not show a "downloaded from the internet" warning.
- While the proxy is stopped, a Codex configured to use it fails (it does
  not fall back to talking to the model directly).
- The app starts the proxy with the app's environment, not your shell's. If
  your network needs `HTTPS_PROXY` for the upstream, run
  `launchctl setenv HTTPS_PROXY http://your-proxy:port` and restart the app
  (this lasts until you reboot; repeat it after a reboot), or run
  `cui-proxy serve` from a terminal instead.
- The app only supports loopback addresses (`listen` and `ui.listen` on
  `127.0.0.1`, `localhost` or `::1`) and needs cui-proxy v0.1.1 or newer.
- After `brew upgrade cui-proxy`, choose Quit and reopen the app
  (`open ~/Applications/CuiProxy.app`) so it runs the new binary; re-run
  `cui-proxy menubar install` to pick up a newer menu-bar app too.
- `cui-proxy menubar status` shows what the app sees;
  `cui-proxy menubar uninstall` removes the app (your config, dictionary and
  logs stay).


## Install (all options)

### Homebrew (macOS and Linux)

```sh
brew install rozdolsky33/tap/cui-proxy
```

That one command adds the tap and trusts only this formula. Upgrade with
`brew update && brew upgrade cui-proxy`.

Homebrew 6+ refuses to load formulae from a non-official tap until you trust
them. If you would rather tap first and install by short name, trust the
formula (or the whole tap) explicitly:

```sh
brew tap rozdolsky33/tap
brew trust --formula rozdolsky33/tap/cui-proxy    # or: brew trust rozdolsky33/tap
brew install cui-proxy
```

### curl

```sh
curl -fsSL https://raw.githubusercontent.com/rozdolsky33/cui-proxy/main/install.sh | sh
```

The installer downloads the release archive for your OS and CPU, **verifies
its sha256 against the release's `checksums.txt`**, and installs the single
`cui-proxy` binary into `/usr/local/bin` if you can write there, otherwise
`~/.local/bin`. It never uses `sudo` and touches nothing else. Options:
`INSTALL_DIR=/some/dir` and `CUI_PROXY_VERSION=v0.2.0`.

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


## Commands

| Command | Purpose |
|---|---|
| `init [-dir PATH] [-force] [-profile sanitize\|strict]` | Write a starter `config.yaml` and `hosts.txt` (mode 0600) into `$XDG_CONFIG_HOME/cui-proxy`, else `~/.config/cui-proxy`. Never overwrites without `-force`. |
| `policy init [-profile ...]` / `policy check -config PATH [-require-strict]` | `init` is the same as above; `check` validates the policy and says what is and is not enforced. |
| `menubar install\|uninstall\|status` | macOS only: build and manage the menu-bar app that runs the proxy in the background. |
| `info -config PATH` | Print the proxy and dashboard URLs for a config (used by the menu-bar app). |
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


## Uninstall

```sh
cui-proxy menubar uninstall                               # FIRST, if you installed the macOS menu-bar app
brew uninstall cui-proxy                                  # Homebrew
brew untrust --formula rozdolsky33/tap/cui-proxy          # optional: drop the trust entry
brew untap rozdolsky33/tap                                # optional: drop the tap
rm "$(command -v cui-proxy)"                              # curl install
rm -r ~/.config/cui-proxy                                 # config and dictionary, if you want them gone
```

## Build

Requires Go 1.26.

```sh
make build            # -> bin/cui-proxy
make test lint        # go test -race, go vet, staticcheck
go build -ldflags "-X main.version=v0.2.0" -o bin/cui-proxy ./cmd/cui-proxy
```


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

Applied on reload: `rules` (and dictionary contents), `policy.profile`, `policy.on_error`,
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

## Policy profiles: sanitize or strict

`policy.profile` decides what a detection does.

| Profile | A detected value... | Use it when |
|---|---|---|
| `sanitize` (default) | is replaced with a synthetic value and restored in the reply | you want the model to keep working on the task and masking is allowed |
| `strict` | **blocks the whole request** before anything is forwarded | a policy forbids sending restricted data at all, or masking needs an approval you do not have |

```sh
cui-proxy policy init -profile strict      # a starter config with the strict profile
cui-proxy policy check -config ~/.config/cui-proxy/config.yaml
cui-proxy policy check -config ... -require-strict   # exit 1 unless strict (for scripts and audits)
```

Any rule can also set `action: block` to refuse a request that matches it
even under `sanitize` (for example a dictionary of names that must never
leave). Under `strict` every rule blocks and a rule may not set
`action: sanitize` (a verified approval may allow that in a later release; see
[docs/adr](docs/adr/)). `strict` needs at least one rule (with none it would
detect nothing). **Any blocking rule (the strict profile or an
`action: block`) also requires `policy.on_error: block`**: the config is
refused otherwise, because `passthrough` forwards the original request when it
cannot be inspected, and a failure can hit before the blocking rule is checked.
The proxy enforces the same thing at runtime, whatever the config says.

- A policy block is a **decision, not a failure**: `policy.on_error:
  passthrough` never forwards it. The error code is `policy` (HTTP 422) and
  the message names the rule, never the value:
  `cui-proxy: request blocked (reason=policy kind=rule_match rule=builtin.ipv4)`.
- When any rule blocks, fields that are normally passed through unscanned so
  the protocol keeps working (`model`, ids, `role`, `status`, tool names and
  types, `include` entries) are **checked, not rewritten**, and so is every
  number in the request, not just text (number literals are expanded exactly,
  so `4.111111111111111e15` is checked as 4111111111111111). The one partial
  exception is the content of `encrypted_content` (the vendor's encrypted
  reasoning blobs, resent as received): it is not scanned, but while a rule
  blocks it must look like base64 ciphertext or the request is refused. A
  value written only with base64 characters could still ride in it; nothing
  short of scanning the blob would catch that, and scanning risks false
  positives on genuine ciphertext.
- The `Authorization`, `Cookie` and `Chatgpt-Account-Id` headers are
  credentials and are forwarded as they are; every other header and the query
  string are scanned and a match blocks.
- The profile applies on reload (SIGHUP, the dashboard button or the
  menu-bar item); the running profile is shown in the dashboard header and
  logged on every start and reload. **A reload can lower the policy** (this is
  a local operator action): it is logged as
  `WARN policy weakened by reload: ...` when the profile goes from `strict` to
  anything else or fewer rules block.
  Switching to `strict` mid-session is safe for values the vault already
  issued as synthetics, which Codex resends in history: they do not block.
  One exception: an address from the `198.18.0.0/15` overflow pool can be a
  real address, so it does block, and because history is resent, so does every
  later turn of that conversation; start a new session.
- `policy.unsupported: block` is the only value and is the default: content
  the proxy cannot inspect (images, files, compressed bodies, unknown routes,
  oversized, too deeply nested or unparseable bodies) is always refused,
  **even under `policy.on_error: passthrough`**.
- `policy check` prints the effective action of every rule and states plainly
  what is enforced, what is not implemented yet (verified approvals, a
  persisted decision log, account allowlists) and what no local tool can do.

`strict` blocks more, and Codex resends the whole conversation each turn, so
one false positive can block every later turn. Tune the rules with synthetic
text (`cui-proxy test-rules`, the dashboard's test box) before switching.
Why this design, and what is deferred: [ADR 0001](docs/adr/0001-govsec-policy-enforcement.md).

## Block diagnostics

A blocked request is answered with an OpenAI-style JSON error whose `code`
is the block reason (`route`, `body_too_large`, `compressed`, `header`,
`policy`, `sanitize`, `unrestorable`, `bad_request`, `internal`). When the body could
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
| `rule_match` | a blocking rule matched (code `policy`: the strict profile, or a rule with `action: block`) | expected: remove the value, or use the `sanitize` profile / a rule without `action: block` if masking is allowed |
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
  your dictionary miss. This reduces exposure; it does not bound it. The
  `strict` profile refuses what is detected; it does not make detection
  complete.
- Hostnames that merely encode an address, such as
  `ip-10-20-30-15.ec2.internal`, are not IP literals and are not detected.
  Add your own regex or dictionary rule for them.
- Usernames and local paths (for example in the `<environment_context>`
  block Codex sends: cwd, shell, home directory) are not detected by the
  builtin set. Put the ones that matter in a dictionary.
- Memory-mode restarts lose all mappings (see Vault).
- Without a blocking rule, `tools[].name` and some protocol identifier fields
  are passed through unscanned so that the protocol keeps working (unknown
  `include` values are scanned, not skipped). When any rule blocks they are
  checked too (see Policy profiles); only the content of `encrypted_content`
  is not scanned (it must look like base64 ciphertext).
- The builtin set has no AWS access key detector; `examples/config.yaml`
  adds one as a regex rule (`aws-keys`, `\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`).
- Upstream 3xx responses other than 304 become 502: a redirect (and its
  `Location`) is never relayed to the client.
- Any sanitize failure blocks the request (fail closed) unless
  `policy.on_error: passthrough`, which forwards the ORIGINAL request only for
  a failure of the sanitizer itself (for example no synthetic left for a
  class). It never forwards unsupported content or a policy block, it is
  refused in the config next to any blocking rule, and it is logged as UNSAFE
  at startup (and by `check-config`).
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
- Design decisions and the plan for policy enforcement (block-by-default,
  approval checks, an audit trail) are tracked in [docs/adr](docs/adr/).
- This is best-effort, community-style software released under the
  [MIT license](LICENSE), with no warranty. It is not an approved compliance
  control.

## Releasing (maintainers)

```sh
git tag v0.2.0 && git push origin v0.2.0
```

`.github/workflows/release.yml` runs vet and the race tests, builds the four
archives with `scripts/package.sh`, and publishes them with `checksums.txt` as
a GitHub release. It then updates the Homebrew tap if the repository secret
`TAP_GITHUB_TOKEN` exists (a fine-grained token with `contents: write` on
`rozdolsky33/homebrew-tap`). Without that secret, update the tap yourself after
the release is published:

```sh
scripts/update-tap.sh v0.2.0      # needs `gh` logged in with push access to the tap
```

`scripts/package.sh <version>` builds the same archives locally into `dist/`
for a dry run. The formula is always generated from the checksums of the
published release (`scripts/make-formula.sh`), never from a local build.
