# Menu-bar app details

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
