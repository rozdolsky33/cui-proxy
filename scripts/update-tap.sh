#!/bin/sh
# Regenerate the Homebrew formula for a published release and push it to the
# tap repository.
#
#   scripts/update-tap.sh <version>
#
# Needs: a published GitHub release v<version> (with checksums.txt), git, curl,
# and `gh` logged in with push access to the tap (locally), or GH_TOKEN set to
# a token with contents:write on the tap (CI).
#
# Environment:
#   TAP_REPO  tap repository (default rozdolsky33/homebrew-tap)
#   REPO      this repository (default rozdolsky33/cui-proxy)
#   DRY_RUN=1 print the formula and stop; push nothing
set -eu

[ $# -eq 1 ] || { echo "usage: $0 <version>" >&2; exit 2; }
ver=${1#v}
repo=${REPO:-rozdolsky33/cui-proxy}
tap=${TAP_REPO:-rozdolsky33/homebrew-tap}
here=$(cd "$(dirname "$0")" && pwd)

tmp=$(mktemp -d "${TMPDIR:-/tmp}/cui-proxy-tap.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

curl -fsSL --proto '=https' --tlsv1.2 -o "$tmp/checksums.txt" \
  "https://github.com/$repo/releases/download/v$ver/checksums.txt" ||
  { echo "update-tap: no published release v$ver in $repo" >&2; exit 1; }

REPO=$repo "$here/make-formula.sh" "$ver" "$tmp/checksums.txt" > "$tmp/cui-proxy.rb"

if [ "${DRY_RUN:-}" = 1 ]; then
  cat "$tmp/cui-proxy.rb"
  exit 0
fi

git clone --quiet "https://github.com/$tap.git" "$tmp/tap"
mkdir -p "$tmp/tap/Formula"
cp "$tmp/cui-proxy.rb" "$tmp/tap/Formula/cui-proxy.rb"
cd "$tmp/tap"
git add Formula/cui-proxy.rb
if git diff --cached --quiet; then
  echo "update-tap: formula already up to date for v$ver"
  exit 0
fi
git commit --quiet -m "cui-proxy $ver"
# Use gh as the credential helper for this one push, without touching the
# user's git config.
git -c credential.helper= -c "credential.helper=!gh auth git-credential" push --quiet origin HEAD
echo "update-tap: pushed Formula/cui-proxy.rb for v$ver to $tap"
