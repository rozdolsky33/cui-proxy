# Documentation status

Reviewed against v0.3.2 on 2026-11-08. The release includes workspace metadata
sanitization and value-free accounting. This documentation revision also
clarifies defaults, override limits, and the separate Git/SSH execution path.

## Current operator documentation

| Document | Purpose / current scope |
|---|---|
| [README](../README.md) | Quick start and navigation |
| [Executive overview](../EXECUTIVE-OVERVIEW.md) | Purpose, data flow, implemented capabilities and limits; memory vault is the default, file persistence is optional |
| [Configuration walkthrough](configuration.md) | Files, every YAML field/default, sanitize/strict, supported overrides, rule examples, Git/SSH troubleshooting, reload/restart |
| [Policy](policy.md) | Profiles, blocking behavior and restrictions |
| [Block diagnostics](block-diagnostics.md) | Reason/kind interpretation and remedies |
| [Limitations](limitations.md) | Inspection exceptions and unsupported behavior |
| [Decision log](decision-log.md) | Audit defaults, accounting, verification, rotation and trust limits |
| [Install](install.md), [menu bar](menubar.md) | Installation and service operation |
| [Development](development.md) | Build and release workflow; use a new version when tagging |
| [Assurance](assurance.md), [security policy](../SECURITY.md) | Trust boundary, verification evidence and private reporting |
| [Verification](verification.md) | Dated live results and repeatable synthetic checks; not a promise that every request format works |

The overview remains relevant as an introduction, but is not a configuration
reference or a claim of complete data-loss prevention. Its earlier claims
about always seeing only fake values, automatic persistence and seamless
operation have been corrected.

## Historical and proposed material

- `superpowers/specs/` and `superpowers/plans/` retain original designs,
  implementation notes and checklists. Their proposed fields and old status
  statements are not current configuration instructions.
- [The redirect spike](spikes/2026-11-06-codex-redirect-spike.md) records an
  earlier Codex version and a capture-server experiment. The later live runs
  are linked from verification.
- `verification-results/*.json` are immutable run evidence: a record saying
  "local patch only; no release published" describes that test's point in
  time, not today's release status.
- [ADR 0001](adr/0001-govsec-policy-enforcement.md) has delivered profiles and
  persisted audit logging. Approval verification, account allowlists and
  policy locking remain unimplemented. [ADR 0002](adr/0002-approval-record-format.md)
  is deferred. An accepted architecture does not mean every phase is shipped.

The release [v0.3.2](https://github.com/rozdolsky33/cui-proxy/releases/tag/v0.3.2)
contains the metadata fix. Source defaults are defined in
[`internal/config/config.go`](../internal/config/config.go); rule construction
in [`internal/detect`](../internal/detect/); restart behavior in
[`cmd/cui-proxy/app.go`](../cmd/cui-proxy/app.go).
