#!/bin/sh
# Build release archives for every supported platform into ./dist.
#
#   scripts/package.sh <version>        # e.g. v0.1.0 (the leading v is optional)
#
# Output: dist/cui-proxy_<ver>_<os>_<arch>.tar.gz for darwin/linux x amd64/arm64
# and dist/checksums.txt (sha256). Each archive holds the binary, LICENSE,
# README.md and examples/. Used by .github/workflows/release.yml and runnable
# locally to check the artifacts before tagging.
set -eu

[ $# -eq 1 ] || { echo "usage: $0 <version>" >&2; exit 2; }
ver=${1#v}
tag="v$ver"
case $ver in
  [0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "package: version must look like v1.2.3, got '$1'" >&2; exit 2 ;;
esac

cd "$(dirname "$0")/.."
rm -rf dist
mkdir -p dist

# COPYFILE_DISABLE stops macOS tar adding ._ AppleDouble files.
COPYFILE_DISABLE=1
export COPYFILE_DISABLE

for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  os=${target%/*}
  arch=${target#*/}
  name="cui-proxy_${ver}_${os}_${arch}"
  stage="dist/.stage/$name"
  mkdir -p "$stage/examples"
  echo "building $name"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
    go build -trimpath -ldflags "-s -w -X main.version=$tag" \
      -o "$stage/cui-proxy" ./cmd/cui-proxy
  cp LICENSE README.md "$stage/"
  cp examples/config.yaml examples/hosts.txt "$stage/examples/"
  tar -C "$stage" -czf "dist/$name.tar.gz" cui-proxy LICENSE README.md examples
done
rm -rf dist/.stage

cd dist
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum ./*.tar.gz | sed 's# \./# #' > checksums.txt
else
  shasum -a 256 ./*.tar.gz | sed 's# \./# #' > checksums.txt
fi
cat checksums.txt
