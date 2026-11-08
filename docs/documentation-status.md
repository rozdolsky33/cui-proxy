# Documentation status

Reviewed for v0.4.0 on 2026-11-08. It includes workspace metadata sanitization,
value-free accounting, and the JSON escape dataflow repair. The configuration
walkthrough covers defaults, override limits, the separate Git/SSH execution
path, and the fixed argument buffering limits.

[Attachment text inspection](attachments.md) and the menu bar's
[offline manual](menubar.md#offline-documentation) are available from v0.4.0. It adds an opt-in
setting, local PDF/OCR extraction, and persisted attachment diagnostics. Real
Poppler extraction/rendering and controlled OCR-process tests were exercised;
actual Tesseract recognition was not verified on the development machine.

## Current operator documentation

The [published manual](https://rozdolsky33.github.io/cui-proxy/) adds a guided
[quick start](quickstart.md), [end-to-end tutorial](tutorial.md),
[Codex setup](codex.md), [validated recipes](recipes.md), [dashboard guide](dashboard.md),
[rule reference](rules.md), and [request lifecycle](architecture.md).



| Document | Purpose / current scope |
|---|---|
| [README](https://github.com/rozdolsky33/cui-proxy/blob/main/README.md) | Quick start and navigation |
| [Executive overview](https://github.com/rozdolsky33/cui-proxy/blob/main/EXECUTIVE-OVERVIEW.md) | Purpose, data flow, implemented capabilities and limits; memory vault is the default, file persistence is optional |
| [Configuration walkthrough](configuration.md) | Files, every YAML field/default, sanitize/strict, supported overrides, rule examples, Git/SSH troubleshooting, reload/restart |
| [Attachment inspection](attachments.md) | Opt-in local PDF/image text conversion, tool requirements, limits and safe failure details; available from v0.4.0 |
| [Policy](policy.md) | Profiles, blocking behavior and restrictions |
| [Block diagnostics](block-diagnostics.md) | Reason/kind interpretation and remedies |
| [Limitations](limitations.md) | Inspection exceptions and unsupported behavior |
| [Decision log](decision-log.md) | Audit defaults, accounting, verification, rotation and trust limits |
| [Install](install.md), [menu bar](menubar.md) | Installation and service operation |
| [Development](development.md) | Build and release workflow; use a new version when tagging |
| [Assurance](assurance.md), [security policy](https://github.com/rozdolsky33/cui-proxy/blob/main/SECURITY.md) | Trust boundary, verification evidence and private reporting |
| [Verification](verification.md) | Dated live results and repeatable synthetic checks; not a promise that every request format works |

The overview remains relevant as an introduction, but is not a configuration
reference or a claim of complete data-loss prevention. Its earlier claims
about always seeing only fake values, automatic persistence and seamless
operation have been corrected.

## Historical and proposed material

- `superpowers/specs/` and `superpowers/plans/` retain original designs,
  implementation notes and checklists. Their proposed fields and old status
  statements are not current configuration instructions.
- [The redirect spike](https://github.com/rozdolsky33/cui-proxy/blob/main/docs/spikes/2026-11-06-codex-redirect-spike.md) records an
  earlier Codex version and a capture-server experiment. The later live runs
  are linked from verification.
- `verification-results/*.json` are immutable run evidence: a record saying
  "local patch only; no release published" describes that test's point in
  time, not today's release status.
- [ADR 0001](https://github.com/rozdolsky33/cui-proxy/blob/main/docs/adr/0001-govsec-policy-enforcement.md) has delivered profiles and
  persisted audit logging. Approval verification, account allowlists and
  policy locking remain unimplemented. [ADR 0002](https://github.com/rozdolsky33/cui-proxy/blob/main/docs/adr/0002-approval-record-format.md)
  is deferred. An accepted architecture does not mean every phase is shipped.

Release [v0.3.3](https://github.com/rozdolsky33/cui-proxy/releases/tag/v0.3.3)
includes the metadata and JSON escape fixes. The
[dataflow report](https://github.com/rozdolsky33/cui-proxy/blob/main/docs/verification-results/2026-11-08-json-escape-dataflow.md)
records regression coverage and verification scope. Source defaults are defined in
[`internal/config/config.go`](https://github.com/rozdolsky33/cui-proxy/blob/main/internal/config/config.go); rule construction
in [`internal/detect`](https://github.com/rozdolsky33/cui-proxy/blob/main/internal/detect/); restart behavior in
[`cmd/cui-proxy/app.go`](https://github.com/rozdolsky33/cui-proxy/blob/main/cmd/cui-proxy/app.go).
