# Compatibility and upgrade policy

This page says which parts of cui-proxy you can build on, what a release is
allowed to change, and how a change that breaks existing setups is announced.
It applies from the first release after v0.7.0.

cui-proxy is still before 1.0. Until 1.0 the policy below is the commitment;
it is narrower than the one a 1.0 release will make.

## Stable interfaces

Four interfaces are declared stable. A change that removes, renames or
redefines any part of them is a breaking change.

| Interface | What is covered | Checked in CI by |
| --- | --- | --- |
| Configuration file | Every YAML key in the [reference](reference.md#fields-and-defaults), its value type, its documented meaning and its default | `TestStableConfigSchema` |
| Command line | Every command and flag in the [command table](reference.md#commands), and exit codes 0, 1 and 2 | `TestStableCommandSurface`, `TestStableExitCodes` |
| Vault file and key | The file identifier `CUIV2`, and that the same key file opens the same vault | `TestStableFileFormatIdentifier` and the vault load tests |
| Decision log | The record fields, their canonical encoding, and the hash chain that `audit verify` checks | `TestCanonicalGoldenVector` and the verification tests |

The tests fail when a key, flag, exit code, identifier or record encoding is
removed, renamed or retyped. They do not detect a change in what a key means
or in its default. Those depend on review and on the release notes.

## What is not covered

These can change in any release. Do not script against them.

- The dashboard pages and the local `/api/` endpoints behind them.
- The wording of messages, warnings, tables and help text printed by commands.
- Metric, stage and counter names.
- The preflight report, which carries its own `schema_version`.
- The menu-bar app's internals and its installed file layout.
- Go packages. All of them are internal and none is an import target.
- The workflow harness under `scripts/workflow/` and its report formats.

Detection is also outside this policy. A release may improve a built-in rule
so that it matches values it previously missed, or stops matching values it
should not have. Such changes are listed in the release notes. Re-run
[your rule tests](testing.md) after every upgrade.

## What each kind of release may change

| Release | May do | May not do |
| --- | --- | --- |
| Patch, `0.Y.Z` | Fix defects. Add nothing that an operator must act on. | Remove, rename or redefine a stable interface. Change a default. Introduce a new vault or decision-log format. |
| Minor, `0.Y.0` | Add configuration keys, commands and flags. Complete a deprecation that was announced earlier. | Remove or redefine a stable interface without the deprecation steps below. |
| Major, after 1.0 | Make breaking changes. | |

Adding a key, command or flag is compatible. An existing configuration file
and an existing script keep working.

One exception applies to every release. A change that is needed to stop a
sensitive value from reaching the upstream, or to keep the proxy failing
closed, can ship immediately and without a deprecation period. The release
notes state that the exception was used and what an operator must do.

## How a breaking change is announced

1. **Deprecation.** A minor release announces the change in its release notes
   and in this manual. The old form keeps working in that release.
2. **Grace period.** The old form keeps working for at least one further
   minor release.
3. **Removal.** A later minor release removes it and lists it under
   "Breaking changes" in the release notes.

Every release's notes carry a compatibility section that lists breaking
changes, deprecations, changed defaults and format changes, or says that there
are none.

## Configuration across versions

The proxy rejects a configuration file that contains a key it does not know.
That has two consequences.

- **Upgrading.** A configuration written for an older release keeps working,
  within the rules above. If a key has been removed after its grace period,
  `check-config` and `serve` fail with an error. The key is never silently
  ignored.
- **Rolling back.** An older binary rejects keys that a newer release added.
  Keep the configuration file that matches each binary, and run
  `check-config` with the older binary before starting it.

See [upgrade and rollback with active histories](release-readiness.md#upgrade-and-rollback-with-active-histories)
for the full procedure.

## Vault format across versions

A vault file starts with a format identifier. The current one is `CUIV2`. A
binary refuses to open a file whose identifier it does not know. It reports
the unsupported version and leaves the file as it is.

When a release introduces a new format:

- The format gets a new identifier.
- That release, and at least the next minor release, still read `CUIV2` and
  convert it. The release notes describe the conversion.
- The release notes state that older binaries cannot read the new format, so
  that rollback is planned before the upgrade. A vault and key backup taken
  before the upgrade does not contain mappings issued after it.

Vault files written in a format older than `CUIV2` are not read by current
releases.

The vault and its key file belong together. Losing either one makes existing
mappings unrecoverable. See [mapping recovery](vault-recovery.md).

## Decision log across versions

A decision-log record has a closed set of fields, and verification rejects a
record that has any other field. Adding a field is therefore a format change
and not a compatible addition.

When a release changes the record format:

- `audit verify` and `explain` in that release still accept records written
  before the change. Rotated files routinely span several releases.
- The release notes state that older binaries cannot verify logs written
  after the change.

## Release cadence

- Patch releases ship when a fix is ready and contain fixes only.
- Minor releases batch new features. The project aims to leave at least two
  weeks between them, so that each one is in use long enough for problems to
  be reported before the next arrives.

The [roadmap](https://github.com/rozdolsky33/cui-proxy/blob/main/ROADMAP.md)
requires this policy to hold across two consecutive releases before 1.0.
