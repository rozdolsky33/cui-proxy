# Documentation Site Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Publish a complete, tested cui-proxy operator manual on GitHub Pages.

**Architecture:** MkDocs Material builds curated existing and new Markdown in
`docs/`. A dedicated Actions workflow validates examples and links, then deploys
the static output with the official Pages artifact/deployment actions.

**Tech Stack:** Markdown, MkDocs Material, Python validation, Go CLI, GitHub Pages.

**Spec:** `docs/superpowers/specs/2026-11-08-documentation-site-design.md`

## Global constraints

No runtime behavior changes; use synthetic fixtures only. Keep personal paths
out of public content. Complete YAML downloads must pass the real CLI. Preserve
the existing docs URLs in GitHub. Exclude historical plans/specs/spikes from the
site and link to them in GitHub when needed. Explicit publication authorization
is already present in the user's request.

## Review focus

- Loopback addresses and relative config paths work when pasted.
- Strict rules block, while sanitize rules mask; no nonexistent allowlists.
- Test a string is local and uses a throwaway vault, with no upstream request.
- Unsupported content and credential exceptions are described accurately.
- Project Pages subpath, search, diagram and asset links work on desktop/mobile.

## Task 1: Site infrastructure and onboarding

Files: `mkdocs.yml`, `requirements-docs.txt`, `.github/workflows/docs.yml`,
`docs/index.md`, `docs/quickstart.md`, `docs/tutorial.md`, `docs/codex.md`,
`docs/recipes.md`, `docs/testing.md`, `docs/examples/*`, `scripts/check-docs.py`.

- [x] Create site configuration, navigation, restrained styling and deployment.
- [x] Write onboarding and Codex guide, citing official provider configuration.
- [x] Create complete safe YAML downloads, matching walkthroughs and test samples.
- [x] Validate downloaded examples with the CLI and run strict site build.

## Task 2: Technical concepts and rules

Files: `docs/architecture.md`, `docs/rules.md`, `docs/glossary.md`.

- [x] Read implementation and tests for adapters, detection, transform and vault.
- [x] Write lifecycle diagrams, field coverage, rule mechanics, built-in classes,
      synth behavior, overlap semantics, boundaries and glossary.
- [x] Cross-check claims against source and existing limitations.

## Task 3: Dashboard and operational reference

Files: `docs/dashboard.md`, `docs/reference.md`, `docs/troubleshooting.md`.

- [x] Read CLI/config/UI source and write all fields, commands and UI explanations.
- [x] Explain local string tests, test cases and expected outcomes; link to assets
      `assets/dashboard-overview.png` and `assets/dashboard-test.png`.
- [x] Check reload/restart behavior, defaults, error codes and troubleshooting.

## Task 4: Integrate, verify and publish

Files: `docs/assets/*`, `README.md`, `docs/development.md`, existing guides as needed.

- [x] Capture isolated synthetic dashboard screenshots, hide log path, inspect.
- [x] Update discovery links and documentation maintenance instructions.
- [x] Run strict build, executable example validation, link/privacy checks, browser
      desktop/mobile/search/copy/diagram checks, and independent accuracy review.
- [ ] Commit and push branch; merge after required CI, configure Pages deployment.
- [ ] Observe successful deployment and verify live site; report public URL.

## Completion evidence (2026-11-08)

- Resumed the existing `docs/github-pages` worktree without changing runtime Go code.
- `mkdocs build --strict`: passes, including navigation, links and anchors.
- `scripts/check-docs.py`: five complete YAML configurations, 37 behavior checks,
  TOML syntax and authored-page privacy/fence checks pass.
- `make test`: all Go race tests pass outside the macOS sandbox (the menu-bar
  process-list test cannot inspect processes inside that sandbox).
- Independent source review corrected restoration counter semantics and protocol
  include exceptions; aligned version references and fresh-vault recovery advice.
- Screenshots contain synthetic demo values; the decision-log path is hidden.
- Desktop/mobile browser verification covers navigation, search, copy buttons,
  light/dark themes, dashboard images and both Mermaid diagrams. Dark-theme link
  contrast was corrected during visual inspection.
- Publication steps remain pending until GitHub CI and live checks succeed.
