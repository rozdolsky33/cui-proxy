# Documentation site design

## Intent

Publish an approachable, searchable GitHub Pages manual for cui-proxy. A new
operator should be able to install it, connect Codex, prove a fabricated
request is sanitized, customize rules, and understand the limits. Experienced
operators need precise configuration and dashboard references.

## Approach

Use MkDocs Material with the existing `docs/` Markdown as the source, a curated
navigation tree, copyable code blocks, local search, light/dark themes, Mermaid
request diagrams, and GitHub Actions deployment to GitHub Pages. This is the
implementation choice for the user's requested publication. A plain Jekyll
site would need additional navigation/search work; a custom React site would
add maintenance without improving the manual. Keep historic plans, spikes,
and verification records in GitHub, outside the published manual.

## Content

- Start: overview, install, quick start, full tutorial, Codex provider setup.
- Configure: existing configuration walkthrough, rule anatomy and built-ins,
  sanitize/strict policies, complete downloadable recipes, full field reference.
- Operate: dashboard (all cards, columns, configuration and Test a string),
  menu bar, local and end-to-end testing, decision log, troubleshooting.
- Understand: request lifecycle, components and trust boundaries, glossary,
  capabilities/limitations and assurance.
- Contribute: existing development guide plus docs maintenance and publishing.

## Constraints

- Document the checked-out implementation; label historical evidence separately.
- No runtime Go behavior changes. No real secrets, dictionary entries, private
  workspace paths, personal account data, or live audit logs in published assets.
- Capture the actual dashboard using an isolated synthetic demonstration. The
  personal audit-log path must not appear; hide the demo log path before capture.
- Code blocks must say whether they are complete files or partial edits. YAML
  downloads must validate with `cui-proxy check-config`; behavior checks must
  exercise sanitize, strict, selective blocks, dictionary, and custom regex.
- Keep loopback listeners, fail-closed defaults, credential passthrough and the
  unsupported-content restrictions explicit. Do not imply a compliance guarantee.
- Use the existing project repository and publish at its standard Pages URL.
  Deployment is authorized by the user's explicit create-and-publish request.

## Verification and completion

Build with strict link/anchor validation. Validate examples with the Go binary,
check rendered desktop and mobile pages, search, navigation, copy buttons and
diagrams. Review source accuracy and privacy. Commit, push, merge the docs change,
enable Pages Actions publishing, observe a successful deployment, and verify
the public URL. Preserve unrelated local work and existing release workflows.
