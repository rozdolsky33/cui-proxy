# Quick start

**Goal:** run a local proxy, check a fabricated string, and send your first Codex
request through it. You need macOS or Linux and a working Codex CLI installation
signed in to the backend you intend to use. This default walkthrough uses a
ChatGPT login. It does not change your Codex configuration permanently.

## 1. Install and create a configuration

```sh
brew install rozdolsky33/tap/cui-proxy
cui-proxy version
cui-proxy init
```

Other installation options are in [Install and upgrade](install.md).
`init` creates `config.yaml` and `hosts.txt` under `~/.config/cui-proxy/`
(or `$XDG_CONFIG_HOME/cui-proxy/` when set). The commands below assume the
default location; use your actual directory if different.

Already initialized? Keep your files and skip `init`. Do not use `-force` to
change one setting: it overwrites the YAML **and** your dictionary.

## 2. Choose the names you need to detect

Open `~/.config/cui-proxy/hosts.txt` in your editor. Keep one literal hostname
per line. For a safe first exercise, use these fictional names:

```text
db.demo.example
build.demo.example
```

The built-in bundle catches address, email, token, and other recognizable
patterns. Your dictionary adds names that those patterns cannot recognize.
Adding an entry makes it sensitive; this file is **not an allowlist**.
See [Rules](rules.md) before adding paths or opaque identifiers.

Validate before starting:

```sh
cui-proxy check-config -config ~/.config/cui-proxy/config.yaml
cui-proxy policy check -config ~/.config/cui-proxy/config.yaml
printf '%s\n' 'Check db.demo.example at 203.0.113.42' |
  cui-proxy test-rules -config ~/.config/cui-proxy/config.yaml
```

Expect `ok: 3 rules` for the unchanged starter and a masked address/hostname
in the test output. Synthetic values vary between test invocations. The
starter uses `sanitize`, so matches are replaced; a strict config rejects them.

## 3. Start the proxy

=== "macOS menu bar"

    ```sh
    cui-proxy menubar install
    ```

    This builds and starts the native helper. If the Swift compiler is missing,
    install Apple's Command Line Tools with `xcode-select --install`, then retry.
    Click the shield → **Open Dashboard**. You can enable **Start at Login**.
    See [menu-bar operations](menubar.md) for status, restart and upgrade details.

=== "Terminal (macOS or Linux)"

    ```sh
    cui-proxy serve -config ~/.config/cui-proxy/config.yaml
    ```

    Leave this running. Open <http://localhost:8788/> in a browser. Use a second
    terminal for the next step. Ctrl-C stops this proxy.

Use one way to start your proxy. Two instances cannot share the same listeners
or the same file vault.

## 4. Try the dashboard locally

The header should show **profile: sanitize** and **config ok**. Under
**Test a string**, paste:

```text
Check db.demo.example at 203.0.113.42
```

Click **Sanitize**. Expect a synthetic hostname and address, plus rule/count
labels. This test is local: it does not contact the model, change live counters,
or add mappings to the service's vault. It does not test Codex routing yet.

## 5. Connect Codex for one session

Run this in a second terminal. Your existing model selection is preserved.

```sh
codex -c 'model_provider="cui-proxy"' \
  -c 'model_providers.cui-proxy={name="cui-proxy",base_url="http://localhost:8787/v1",wire_api="responses",requires_openai_auth=true,supports_websockets=false}'
```

Enter this fabricated prompt:

```text
Reply with the address 203.0.113.42 exactly. Do not use any tools.
```

After the response finishes, the dashboard should show a request to
`POST /v1/responses`. If the model echoed the address, the proxy should restore
it to `203.0.113.42` in your client. The request's rule counts should include
the address detector. A normal conversation can trigger more than one request.

!!! tip "Verify each layer"
    A response alone is not proof of routing. Check the live-feed row, replacement
    counters, and [decision log](decision-log.md). For repeatable checks of
    restoration and tool output, follow [Test your setup](testing.md).

## 6. Make it your own

- [Connect Codex permanently or with a profile](codex.md).
- [Follow the complete configuration tutorial](tutorial.md).
- [Choose a complete configuration recipe](recipes.md).
- [Understand a failed first request](troubleshooting.md).

Read the [limitations](limitations.md) before using real material. A successful
demo confirms those test cases; it does not establish complete detection.
