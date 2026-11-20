# Contributing

Thank you for looking at cui-proxy. This file says how to propose a change and
what a change needs before it is merged.

The project has one maintainer. Expect replies within days, not hours, and see
[Maintain the project](docs/maintaining.md) for what that means in practice.

## Before you start

- **A way around the protections is not a pull request.** If you found a value
  the proxy forwards when it should not, report it privately as described in
  [SECURITY.md](SECURITY.md). Do not open a public issue or pull request that
  shows how.
- **Use made-up values everywhere.** Tests, fixtures, issues and pull request
  text must never contain real credentials, identifiers or customer data. Use
  values with the same shape.
- **Open an issue first for anything large.** A new detector family, a new
  client or a change to a [stable interface](docs/compatibility.md) should be
  discussed before code is written.
- **New clients and platforms are not being added yet.**
  [Adding a client or feature](docs/adding-scope.md) says why and what an
  addition will need.

## Build and test

You need Go 1.26 and `staticcheck`.

```sh
make build      # bin/cui-proxy
make test       # all tests with the race detector
make lint       # go vet and staticcheck
make fuzz       # 20 seconds on each of four fuzz targets
```

Documentation changes under `docs/` also need the bundled manual rebuilt; see
[Maintain the documentation](docs/contributing-docs.md).

## What a pull request needs

- A Conventional Commit title, such as `fix(detect): ...` or `docs: ...`.
- A description of the problem, the change and how you checked it.
- A test for new or changed behavior. A bug fix needs a test that fails
  without the fix. A fuzz failure is kept as a regression seed.
- Passing checks on Linux and macOS.
- Updated documentation when a command, a configuration key or policy behavior
  changes.

## Changes to sensitive paths

Some packages decide what leaves the machine. They are listed in
[.github/CODEOWNERS](.github/CODEOWNERS). A change there needs more:

- State in the description whether anything that was refused is now accepted.
- Add tests for nearby inputs that must still be refused.
- Keep logs, error messages, metrics and the dashboard free of original values.
- Expect a slower and more careful review. [Maintain the project](docs/maintaining.md#what-a-review-of-a-sensitive-change-checks)
  lists what the reviewer checks.

A change that moves a published detection figure must update the baseline in
the same pull request; see [measured detection quality](docs/detection-quality.md).

## Becoming a reviewer

The project needs people who can review changes to detection, transformation
and streaming. If you want to do that, say so in an issue.
[Maintain the project](docs/maintaining.md#becoming-a-reviewer-or-maintainer)
describes the path.
