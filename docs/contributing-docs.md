# Maintain the documentation

The published manual is built from `docs/` with MkDocs Material. Markdown is
reviewed in the same repository as the implementation. Changes to `main`
automatically build and deploy to [GitHub Pages](https://rozdolsky33.github.io/cui-proxy/).
Pull requests build and validate the site without publishing.

## Preview locally

Use Python 3.12 or newer and the Go version specified in `go.mod`:

```sh
python3 -m venv .venv-docs
.venv-docs/bin/python -m pip install -r requirements-docs.txt
go build -o bin/cui-proxy ./cmd/cui-proxy
.venv-docs/bin/python scripts/check-docs.py --binary bin/cui-proxy
.venv-docs/bin/mkdocs serve
```

Open the local URL printed by MkDocs. Before submitting:

```sh
.venv-docs/bin/mkdocs build --strict
git diff --check
```

Strict mode rejects broken local links, missing anchors, and navigation gaps.
The executable check validates every YAML download with the real CLI and checks
masking, blocking, dictionary boundaries, regex matches, and TOML syntax.

## Content conventions

- Put a new user guide in the relevant section of `mkdocs.yml`.
- Explain the task and expected result before giving a command.
- Label complete files versus partial edits. Use explicit filenames and tell
  the reader whether reload or restart is required.
- Keep downloadable YAML in `docs/examples/`. Include it in code blocks with
  `pymdownx.snippets` so the displayed file and download cannot drift.
- Use fabricated test data. Rule names, paths and regex patterns are visible
  in the dashboard, so review them before capturing screenshots.
- Capture the actual dashboard in an isolated local demo. Hide all personal
  paths before the screenshot is written. Never publish real vaults, key files,
  dictionaries, tokens, or decision logs.
- Keep the version statement and reference defaults consistent with source.
  Link dated test evidence as evidence, not a general compatibility promise.

Plans, specs, spikes, ADR proposals and raw verification records remain in the
repository but are excluded from the site build. Link to their GitHub URLs
when a current guide needs the historical context.

## Publishing and rollback

The repository's Pages setting must select **GitHub Actions** as its build
source. `.github/workflows/docs.yml` builds with read-only repository access.
Only its main-branch deployment job receives `pages: write` and `id-token: write`.
No personal access token is needed to publish the artifact.

To republish, run the **Documentation** workflow on `main`. To roll back a
documentation change, revert its commit through the normal review process;
the resulting main-branch run publishes the reverted content. The Go release
workflow remains separate, so publishing the manual does not release a binary.


## Keep the bundled offline manual current

The menu-bar installer unpacks `internal/manual/site.zip` from the Go binary
into the app before signing it. This committed generated artifact keeps normal
`go build`, Homebrew and standalone binary installs independent of Python.
`mkdocs-offline.yml` reuses the online theme and navigation with local HTML
links, offline search, no repository widget, and a local iframe-worker shim.
Diagrams remain readable source with adjacent prose; no CDN renderer is loaded.
Third-party asset licenses are included in the archive.

After changing a published guide, example, screenshot, docs configuration or
manual-build input, regenerate the snapshot with the pinned documentation tools:

```sh
.venv-docs/bin/python scripts/build-manual.py
go test ./internal/manual
```

Commit `internal/manual/site.zip` with the source changes. The Go test compares
the full published-input list and SHA-256 digests against the embedded manifest,
so stale or omitted source files fail CI and release tests. Historical excluded
plans, specs, ADRs and raw evidence do not force a manual rebuild. The build is
local and uses the vendored offline search shim in `scripts/manual-assets/`;
reading the result makes no automatic external requests. Preview the extracted
`index.html` as a file URL with networking disabled to check navigation, search,
images and downloads. These checks are separate from the HTTP Pages preview.
