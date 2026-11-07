#!/bin/sh
# cui-proxy installer.
#
#   curl -fsSL https://raw.githubusercontent.com/rozdolsky33/cui-proxy/main/install.sh | sh
#
# Downloads the release archive for this machine from GitHub, verifies its
# sha256 against the release's checksums.txt, and installs the `cui-proxy`
# binary. It never uses sudo and touches nothing but the binary.
#
# Environment:
#   CUI_PROXY_VERSION   release to install, e.g. v0.1.0 (default: latest)
#   INSTALL_DIR         where to put the binary (default: /usr/local/bin when it
#                       is writable, otherwise ~/.local/bin)
#   CUI_PROXY_BASE_URL  testing only: download from this base URL instead of the
#                       GitHub release (also disables the https-only restriction)
#
# Everything lives in main() and runs on the last line, so a download that is
# cut short can never execute half a script.
set -eu

REPO="rozdolsky33/cui-proxy"

say() { printf '%s\n' "$*"; }
die() { printf 'install: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "'$1' is required but was not found"; }

# fetch wraps curl: fail on HTTP errors, follow redirects, and (unless testing
# against CUI_PROXY_BASE_URL) accept only https with TLS 1.2+.
fetch() {
  if [ -n "${CUI_PROXY_BASE_URL:-}" ]; then
    curl -fsSL "$@"
  else
    curl -fsSL --proto '=https' --tlsv1.2 "$@"
  fi
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    die "need sha256sum or shasum to verify the download"
  fi
}

main() {
  need curl
  need tar
  need uname
  need mktemp
  need awk
  need install

  case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) die "unsupported OS '$(uname -s)' (supported: macOS, Linux)" ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) die "unsupported CPU '$(uname -m)' (supported: amd64, arm64)" ;;
  esac

  if [ -n "${CUI_PROXY_BASE_URL:-}" ]; then
    say "WARNING: using CUI_PROXY_BASE_URL=$CUI_PROXY_BASE_URL (testing only)"
  fi

  version="${CUI_PROXY_VERSION:-}"
  if [ -z "$version" ]; then
    # /releases/latest redirects to /releases/tag/<version>; no API call, no jq.
    final=$(fetch -I -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") ||
      die "could not look up the latest release (is there a release yet?)"
    version=${final##*/}
  fi
  case "$version" in v*) ;; *) version="v$version" ;; esac
  case "$version" in
    v[0-9]*.[0-9]*.[0-9]*) ;;
    *) die "unexpected version '$version'" ;;
  esac
  ver=${version#v}

  asset="cui-proxy_${ver}_${os}_${arch}.tar.gz"
  base="${CUI_PROXY_BASE_URL:-https://github.com/$REPO/releases/download/$version}"

  tmp=$(mktemp -d "${TMPDIR:-/tmp}/cui-proxy-install.XXXXXX") || die "could not create a temp directory"
  trap 'rm -rf "$tmp"' EXIT
  trap 'exit 1' INT TERM HUP

  say "Downloading $asset ($version)"
  fetch -o "$tmp/$asset" "$base/$asset" || die "download failed: $base/$asset"
  fetch -o "$tmp/checksums.txt" "$base/checksums.txt" || die "download failed: $base/checksums.txt"

  expected=$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/checksums.txt")
  case "$expected" in
    *[!0-9a-f]* | "") die "no valid checksum for $asset in checksums.txt" ;;
  esac
  [ "${#expected}" -eq 64 ] || die "no valid checksum for $asset in checksums.txt"
  actual=$(sha256_of "$tmp/$asset")
  [ "$actual" = "$expected" ] || die "checksum mismatch for $asset (expected $expected, got $actual); nothing was installed"
  say "Checksum OK"

  # Extract only the binary, by name.
  tar -xzf "$tmp/$asset" -C "$tmp" cui-proxy || die "could not extract the binary from $asset"
  [ -f "$tmp/cui-proxy" ] || die "the archive did not contain cui-proxy"

  if [ -n "${INSTALL_DIR:-}" ]; then
    dest=$INSTALL_DIR
  elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    dest=/usr/local/bin
  else
    [ -n "${HOME:-}" ] || die "HOME is not set; set INSTALL_DIR"
    dest="$HOME/.local/bin"
  fi
  mkdir -p "$dest" || die "cannot create $dest (set INSTALL_DIR to a directory you can write to)"
  [ -w "$dest" ] || die "$dest is not writable (set INSTALL_DIR to a directory you can write to)"
  install -m 0755 "$tmp/cui-proxy" "$dest/cui-proxy" || die "could not install into $dest"

  say "Installed: $dest/cui-proxy"
  "$dest/cui-proxy" version || die "the installed binary did not run"

  case ":$PATH:" in
    *":$dest:"*) ;;
    *) say ""
       say "NOTE: $dest is not on your PATH. Add it, e.g.:"
       say "  export PATH=\"$dest:\$PATH\"" ;;
  esac

  say ""
  say "Next:"
  say "  cui-proxy init        # write a starter config and dictionary"
  say "  cui-proxy serve -config ~/.config/cui-proxy/config.yaml"
  say ""
  say "This tool reduces risk; it is not a compliance control. Read the README first:"
  say "  https://github.com/$REPO#readme"
}

main "$@"
