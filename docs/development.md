# Build and release (maintainers)

## Build

Requires Go 1.26.10 or newer.

```sh
make build            # -> bin/cui-proxy
make test lint        # go test -race, go vet, staticcheck
go build -ldflags "-X main.version=v0.2.0" -o bin/cui-proxy ./cmd/cui-proxy
```

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
