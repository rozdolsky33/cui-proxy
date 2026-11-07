# Repository Guidelines

## Project Structure & Module Organization

`cui-proxy` is a Go proxy that sanitizes Codex requests and restores values in streamed responses. CLI commands live in `cmd/cui-proxy/`; the macOS menu-bar helper is in its `menubar/` subdirectory. `internal/` contains adapters, detection, transformation, proxy routing, vault storage, audit logging, configuration, and dashboard packages. Dashboard assets live in `internal/ui/static/`. Tests sit beside implementation files; fixtures and fuzz seeds use `testdata/` directories. See `examples/` for configuration, `docs/adr/` for architecture decisions, and `scripts/` for packaging.

## Build, Test, and Development Commands

Use Go 1.26 as declared in `go.mod`, and install `staticcheck` for linting.

- `make build`: build `bin/cui-proxy`.
- `make test`: run all Go tests with the race detector.
- `make lint`: run `go vet` and `staticcheck` (all checks enabled).
- `make fuzz`: run three fuzz targets for 20 seconds each.
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
