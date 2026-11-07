#!/bin/sh
# Print the Homebrew formula for a release to stdout.
#
#   scripts/make-formula.sh <version> <checksums.txt>
#
# checksums.txt is the file scripts/package.sh writes and the release publishes
# ("<sha256>  cui-proxy_<ver>_<os>_<arch>.tar.gz" per line).
set -eu

[ $# -eq 2 ] || { echo "usage: $0 <version> <checksums.txt>" >&2; exit 2; }
ver=${1#v}
checks=$2
repo=${REPO:-rozdolsky33/cui-proxy}
[ -r "$checks" ] || { echo "make-formula: cannot read $checks" >&2; exit 2; }

sum() {
  s=$(awk -v f="cui-proxy_${ver}_$1.tar.gz" '$2 == f { print $1 }' "$checks")
  case "$s" in
    *[!0-9a-f]* | "") echo "make-formula: no valid sha256 for $1 in $checks" >&2; exit 1 ;;
  esac
  [ "${#s}" -eq 64 ] || { echo "make-formula: no valid sha256 for $1 in $checks" >&2; exit 1; }
  printf '%s' "$s"
}
url() { printf 'https://github.com/%s/releases/download/v%s/cui-proxy_%s_%s.tar.gz' "$repo" "$ver" "$ver" "$1"; }

darwin_arm64=$(sum darwin_arm64)
darwin_amd64=$(sum darwin_amd64)
linux_arm64=$(sum linux_arm64)
linux_amd64=$(sum linux_amd64)

cat <<RUBY
class CuiProxy < Formula
  desc "Fail-closed local proxy that masks sensitive data in Codex CLI traffic"
  homepage "https://github.com/${repo}"
  license "MIT"

  on_macos do
    on_arm do
      url "$(url darwin_arm64)"
      sha256 "${darwin_arm64}"
    end
    on_intel do
      url "$(url darwin_amd64)"
      sha256 "${darwin_amd64}"
    end
  end

  on_linux do
    on_arm do
      url "$(url linux_arm64)"
      sha256 "${linux_arm64}"
    end
    on_intel do
      url "$(url linux_amd64)"
      sha256 "${linux_amd64}"
    end
  end

  def install
    bin.install "cui-proxy"
    pkgshare.install "examples"
  end

  def caveats
    <<~EOS
      Create a starter config and dictionary, then start the proxy:
        cui-proxy init
        cui-proxy serve -config ~/.config/cui-proxy/config.yaml

      On macOS, to run it in the background with a menu-bar icon:
        cui-proxy menubar install

      cui-proxy reduces risk; it is not a compliance control. Read
      https://github.com/${repo}#readme before use.
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/cui-proxy version")
    system bin/"cui-proxy", "check-config", "-config", pkgshare/"examples/config.yaml"
  end
end
RUBY
