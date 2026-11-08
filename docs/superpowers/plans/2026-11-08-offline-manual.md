# Offline manual implementation plan

Goal: bundle the operator manual, open it from the menu bar, merge the PR, and release v0.4.0.
Spec: ../specs/2026-11-08-offline-manual-design.md

## Global constraints

Offline browser reading works with proxy stopped. No runtime downloads, server, or GitHub fallback. Standalone binaries carry the snapshot. Install resources before signing/replacing. Preserve existing application and config on failed installation. Version v0.4.0 includes attachment text support.

## Review focus

- Missing/corrupt resources and browser-open failure have a clear remedy.
- Spaces/non-ASCII in installation paths produce valid file URLs.
- No automatic external requests; local navigation, search, diagrams and recipe downloads work.
- Source changes cannot silently ship a stale embedded manual.
- Release artifacts contain the manual without a source checkout; existing installations need menu-bar reinstall.

## Tasks

1. Write failing install/headless resource tests and offline archive tests. Generate a deterministic MkDocs offline archive and implement safe extraction with source-freshness verification. Add the menu command and error handling. Run targeted Go and offline-browser checks.
2. Update docs to v0.4.0 availability, menu behavior and maintainer regeneration steps. Regenerate snapshot, run full race/lint/build/docs/package checks and independent review.
3. Commit and create PR; wait for all checks, merge, pull main, tag fresh v0.4.0, verify release assets/checksums and Pages/tap workflows.

## Verification before PR

Implementation and docs checks passed: full race suite, vet/all staticcheck,
strict Pages build, 6 downloadable configs / 44 behavior checks, and four
v0.4.0 release archives. Offline browser checks pass with networking disabled,
including header-home navigation, full-text search, screenshots and responsive
layout. Go tests catch stale snapshots and symlink escapes. Native installation
checks exercise signing, Unicode/space paths and resource repair. The manual is
about 1.2 MB compressed. Diagrams retain readable definitions with prose offline.
A separate review found the home-link defect; a failing regression test and
browser check were fixed and the full suite rerun successfully.
