# Architecture decision records

Short records of design decisions that are hard to reverse or easy to forget
the reasons for: what was decided, what else was considered, and what it costs.
Earlier decisions made while building v0.1 are in
[`docs/superpowers/specs/2026-11-06-cui-proxy-design.md`](../superpowers/specs/2026-11-06-cui-proxy-design.md)
(section 13) and in the README's "Known limitations".

| ADR | Title | Status |
|---|---|---|
| [0001](0001-govsec-policy-enforcement.md) | Policy enforcement for restricted data: block by default, verified approval, auditable decisions | Accepted (phase 3 blocked on 0002) |
| [0002](0002-approval-record-format.md) | Approval record format and verification | Deferred (needs input from the approving authority) |
| [0003](0003-tool-result-image-extraction.md) | Extract text from images in Codex tool results | Accepted |

## Conventions

- One file per decision: `NNNN-short-title.md`, numbered in order, never renumbered.
- **Status** is one of `Proposed`, `Accepted`, `Deferred`, `Superseded by NNNN`, `Rejected`.
  Change the status line and add a row to the ADR's "Status log"; do not
  rewrite history. If a decision changes, write a new ADR that supersedes it.
- Keep real sensitive data, internal policy text and names of customers or
  employers out of ADRs: this repository is public. Describe requirements
  generically.
- Each ADR has an **Operability** section: how the decision is rolled out,
  monitored, what fails and how, and how an operator recovers.
