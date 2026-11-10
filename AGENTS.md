# Repository Guidelines

## Working Through the Proxy

An assistant working on this repository may itself use the running proxy. Before reading local configuration, cloud CLI documentation, fixture files, test-runner source or detailed reports, capture and inspect them locally without printing their contents into tool output. This includes `/api/config`: patterns and dictionary entries can match explicit blocking rules. Fabricated values and sanitized output can also match those rules.

Use a local subprocess with captured stdout and stderr for `test-rules` and fixture runners. Report only exit status, rule names and counts; do not dump source, inputs, replacements or JSON reports into the conversation. See `docs/verification.md` for a captured-output example. Do not use bulk `cat`, `sed` or searches that print matching lines from these sources into an assistant session.

If a request fails with HTTP 422 `reason=policy kind=rule_match`, use `cui-proxy explain` with the request ID to confirm the decision. Repeated requests can resend the same triggering history. Start a fresh session after removing the source of fixture output; repeating a request or reopening the same source does not resolve it. Preserve blocking rules while investigating.

## Project Structure & Module Organization

`cui-proxy` is a Go proxy that sanitizes Codex requests and restores values in streamed responses. CLI commands live in `cmd/cui-proxy/`; the macOS menu-bar helper is in its `menubar/` subdirectory. `internal/` contains adapters, detection, transformation, proxy routing, vault storage, audit logging, configuration, and dashboard packages. Dashboard assets live in `internal/ui/static/`. Tests sit beside implementation files; fixtures and fuzz seeds use `testdata/` directories. See `examples/` for configuration, `docs/adr/` for architecture decisions, and `scripts/` for packaging.

## Build, Test, and Development Commands

Use Go 1.26 as declared in `go.mod`, and install `staticcheck` for linting.

- `make build`: build `bin/cui-proxy`.
- `make test`: run all Go tests with the race detector.
- `make lint`: run `go vet` and `staticcheck` (all checks enabled).
- `make fuzz`: run four fuzz targets for 20 seconds each.
- `bin/cui-proxy check-config -config examples/config.yaml`: validate the example configuration.
- `bin/cui-proxy serve -config examples/config.yaml`: start the proxy and local dashboard.
- `shellcheck install.sh scripts/*.sh`: check installer and packaging scripts.

## Coding Style & Naming Conventions

Format Go code with `gofmt`, using tabs and standard Go import grouping. Keep package names short and lowercase; use exported `CamelCase` names and unexported `camelCase` names. Follow existing descriptive filenames, with tests named `*_test.go`. Keep protocol-specific parsing in adapters and shared sanitization logic in `internal/transform/`.

## Testing Guidelines

Use Go’s `testing` package and `httptest` fake upstreams. Name tests `Test...` and fuzz targets `Fuzz...`. Cover fail-closed behavior, sanitization, stream restoration, and metadata-only logging when changing these paths. Retain fuzz failures as regression seeds. CI runs race tests, linting, and fuzz smoke tests on Linux and macOS; no numeric coverage threshold is configured. Follow `docs/verification.md` for manual backend verification using synthetic prompts.

## Commit & Pull Request Guidelines

Follow the history’s Conventional Commit style: `feat(proxy): ...`, `fix(audit): ...`, or `docs: ...`. Keep subjects concise and describe the resulting behavior. PRs should explain the problem, changes, and validation; link relevant issues and include screenshots for dashboard changes. Update documentation when CLI or policy behavior changes.

## Security & Configuration

Use fabricated sensitive values in fixtures and reports. Never commit credentials, vault keys, or real sensitive data. Preserve fail-closed checks and keep logs value-free. Report bypasses privately through GitHub vulnerability reporting as described in `SECURITY.md`.
