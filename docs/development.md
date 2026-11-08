# Build and release (maintainers)

## Build

Requires Go 1.26.10 or newer and a staticcheck build compatible with that
toolchain (validated with staticcheck 2026.2.2).

```sh
make build            # -> bin/cui-proxy
make test lint        # go test -race, go vet, staticcheck
go build -ldflags "-X main.version=v0.4.0" -o bin/cui-proxy ./cmd/cui-proxy
```

## Releasing (maintainers)

```sh
# Choose a NEW version after checking existing tags.
git tag vX.Y.Z && git push origin vX.Y.Z
```

`.github/workflows/release.yml` runs vet and the race tests, builds the four
archives with `scripts/package.sh`, and publishes them with `checksums.txt` as
a GitHub release. It then updates the Homebrew tap if the repository secret
`TAP_GITHUB_TOKEN` exists (a fine-grained token with `contents: write` on
`rozdolsky33/homebrew-tap`). Without that secret, update the tap yourself after
the release is published:

```sh
scripts/update-tap.sh v0.4.0      # needs `gh` logged in with push access to the tap
```

The same tag runs `.github/workflows/docs.yml`, which publishes the manual
under `vX.Y.Z/` and moves the `latest/` alias to it; see
[publishing and rollback](contributing-docs.md#publishing-and-rollback).

`scripts/package.sh <version>` builds the same archives locally into `dist/`
for a dry run. The formula is always generated from the checksums of the
published release (`scripts/make-formula.sh`), never from a local build.

## Documentation site

See [Maintain the documentation](contributing-docs.md) for preview, executable
example validation, the strict site build, screenshots, and GitHub Pages publishing.
