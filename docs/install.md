# Install and uninstall

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
`INSTALL_DIR=/some/dir` and `CUI_PROXY_VERSION=v0.5.0`.

Prefer to read it first?

```sh
curl -fsSL https://raw.githubusercontent.com/rozdolsky33/cui-proxy/main/install.sh -o install.sh
less install.sh && sh install.sh
```

### Manual download or from source

Download an archive and `checksums.txt` from the
[releases page](https://github.com/rozdolsky33/cui-proxy/releases), check it
with `shasum -a 256 -c --ignore-missing checksums.txt`, and put `cui-proxy` on
your `PATH`. To build from source, see [Build](development.md#build).

Archives exist for macOS and Linux on amd64 and arm64. Check the install with
`cui-proxy version`.

## Uninstall

```sh
cui-proxy menubar uninstall                               # FIRST, if you installed the macOS menu-bar app
brew uninstall cui-proxy                                  # Homebrew
brew untrust --formula rozdolsky33/tap/cui-proxy          # optional: drop the trust entry
brew untap rozdolsky33/tap                                # optional: drop the tap
rm "$(command -v cui-proxy)"                              # curl install
rm -r ~/.config/cui-proxy                                 # config and dictionary, if you want them gone
```
