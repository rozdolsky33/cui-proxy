# Menu-bar app details

Look for the shield in the menu bar and click it:

| Menu item | What it does |
|---|---|
| **Open Dashboard** | Opens the dashboard in your browser (greyed out while the proxy is not running). |
| **Open Documentation** | Opens the bundled v0.4.0 manual in your default browser, including when the proxy is stopped or there is no internet connection. |
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


## Offline documentation

From v0.4.0, **Open Documentation** opens a local `file://` page inside
`CuiProxy.app/Contents/Resources/Manual/`. Navigation, full-text search,
screenshots and downloadable example configurations are bundled. No proxy,
HTTP listener, GitHub connection or Python installation is needed to read it.
External reference links still need a connection if you choose to follow them.
Diagrams appear as their readable definitions offline; the numbered walkthroughs
explain the same flows without loading a diagram renderer from a CDN.

The manual is a versioned snapshot of the app you installed. Upgrade the binary
and reinstall the helper to get the new menu and documentation:

```sh
brew update && brew upgrade cui-proxy
cui-proxy menubar install
```

Keep any custom `-config`, `-binary` or `-dir` options you used for the original
installation. This refreshes the app without changing your config or dictionary.
`menubar status` reports its `documentation_url`; a missing manual or browser
failure shows a local alert with reinstall instructions instead of redirecting
you to GitHub. The offline snapshot is not updated by **Reload rules**.
