# ADR 0001: Policy enforcement for restricted data

- **Status:** Accepted (phases 1, 2, 4, 5); phase 3 is blocked on [ADR 0002](0002-approval-record-format.md)
- **Date:** 2026-11-07
- **Deciders:** project maintainer (decision owner); GOVSEC / ISSM review required before any claim that this meets a given organization's policy
- **Related:** [ADR 0002](0002-approval-record-format.md) (approval record format, deferred); design spec section 13; README "Known limitations"

## Context

cui-proxy replaces sensitive values in Codex CLI traffic with synthetic ones
and restores them in the response (see the design spec). That is **masking**.
Organizations that handle government or other restricted data usually have
AI-use policies that ask for more than masking. Requirements of this kind are
common (this paraphrases the typical shape, not any one policy):

1. Only approved AI tools and accounts may be used.
2. Restricted categories of data must not go to an externally hosted model.
3. Sanitized or synthetic substitutes may be used only with an explicit,
   recorded approval from the security and legal authorities.
4. An exception is a written record: tool, data category, business need,
   approving authority, required controls, retention, and an expiry or review
   date.
5. Output is reviewed by a person before it is used operationally.
6. Suspected exposure is reported; use stops.
7. Decisions can be shown to an auditor afterwards.

Today the proxy covers part of requirement 2 (it masks what its detectors
recognize), blocks what it cannot inspect (images, files, compressed bodies,
unknown routes, headers or query strings that match a rule), and keeps only an
in-memory activity feed plus plain, value-free log lines. It does not block on a match, does not know whether a
use is approved, and keeps no durable record of decisions.

The problem: make the tool **enforce** what software can enforce, **record**
what an auditor will ask for, **state plainly** what it cannot do, and stay
configurable and easy to operate.

## Decision drivers

- Rigid where it must be: a policy violation fails closed, with no silent fallback.
- Configurable: organizations differ in what they approve, retain and log.
- Congruent: one policy file, one vocabulary (rule IDs, decisions, reason codes) shared by the config, the dashboard, the logs and the docs.
- User friendly: a blocked request says why and what to do; setup is a command, not a manual.
- Auditable without recording sensitive payloads.
- Honest about limits; never presented as a compliance control.

## Considered options

| Option | For | Against |
|---|---|---|
| **A. Keep masking only; tune rules in YAML** | No work; already shipped | Cannot express "refuse"; no approval or accountability; a missed pattern is silently forwarded |
| **B. Block by default, verified approval, persisted decision log (chosen)** | Matches how the policy reads: sanitizing is the approved exception; produces evidence | New config surface; more blocks for users (false positives hurt more); approval is only as trustworthy as who can write it |
| **C. External policy engine (for example OPA/Rego)** | Expressive, familiar to some security teams | Heavy dependency for a single-binary tool; harder for operators to read; same inputs still need to be built |
| **D. Rely on network and endpoint controls only** | Strongest against bypass | Cannot inspect content or approve a use; still needs B for content decisions |

D is complementary and still needed for bypass, which software on the
endpoint cannot prevent (see Limitations).

## Decision

Adopt **B**, delivered in phases (see "Implementation plan"). Each phase ships
and is useful on its own.

### 1. Policy profiles and per-rule actions

- `policy.profile` is `strict` or `sanitize`.
  - `strict`: any detection **blocks the whole request** before forwarding. Sanitizing is available only for categories an approval names (section 3).
  - `sanitize`: today's behavior (mask and restore).
- Each rule may set `action: block | sanitize` to override the profile for that rule, never to weaken `strict` unless an approval covers it.
- `policy.unsupported: block` is explicit and listed in the mapping document: images, files, audio, compressed bodies, unknown routes, over-deep or oversized bodies, unparseable bodies. (Behavior today; this makes it a visible, documented setting rather than an implementation detail.)
- A blocked response is a 422 with a stable reason code and rule ID, never request data (as today).

**Default profile (decided 2026-11-07).** `sanitize` stays the default while the version is 0.x so existing users are not surprised, and `strict` ships as a named profile (`cui-proxy policy init --profile strict`). The default is revisited at 1.0 (open question 5). The CLI and dashboard always show which profile is active, and `policy check` says plainly what `sanitize` does not do.

### Defaults

Defaults are the safe, boring choice for each setting; anything weaker is an explicit, logged opt-in.

| Setting | Default | Notes |
|---|---|---|
| `policy.profile` | `sanitize` | `strict` is one command away; shown everywhere |
| `policy.on_error` | `block` | `passthrough` is UNSAFE and warned about at startup (existing) |
| `policy.unsupported` | `block` | the only supported value; a documented setting, not a switch |
| rule `action` | follows the profile | under `strict` a rule may not weaken it (until an approval covers it) |
| `audit` (phase 2) | on, value-free, mode 0600, default path under the user's state/log directory | cheap and has no payloads, so it is on by default |
| `audit.on_error` (phase 2) | `block` under `strict`, `warn` under `sanitize` | a full disk should not break a personal session, but must not be silent under `strict` |
| `approval.mode` (phase 3) | `none` | `file` is labelled tracking-only; signed modes wait for ADR 0002 |
| listen addresses | loopback only | existing |
| unknown config keys | error | existing |

### 2. Persisted, tamper-evident decision log

- One JSON line per request decision, appended to `audit.file` (mode 0600).
- Fields: `ts`, `request_id`, `adapter`, `profile`, `decision` (`forwarded | sanitized | blocked`), `reason` (closed set), `rule_ids`, per-rule counts, `approval_id`, `prev_hash`, `hash`. **Never** values, headers, prompts, tokens or paths.
- `hash = SHA-256(prev_hash || canonical JSON of the record)`: a hash chain, so deleting or editing a line is detectable. `cui-proxy audit verify` checks the chain and reports the first break.
- Retention and rotation are configurable (`audit.retention_days`, size-based rotation that keeps the chain by recording the previous file's last hash). Optional syslog output for central collection.
- **Fail closed on audit failure** (default): if the record cannot be written, the request is blocked and a clear error is shown. `audit.on_error: warn` exists for personal use and is logged as UNSAFE, mirroring `policy.on_error: passthrough`.

### 3. Verified approval before sanitizing or forwarding

- `approval.mode`: `none | file | signed`.
- An approval record carries the fields an exception needs: `id`, `tool`, `upstreams`, `data_categories`, `business_need`, `approver`, `controls`, `retention`, `issued`, `expires`, optional `accounts`.
- Checked at startup, on reload and per request: missing, malformed, expired, or not covering the configured upstream blocks all traffic (fail closed) with a message naming the problem and the expiry.
- `data_categories` map to rule classes (`ipv4`, `ocid`, `ssn`, `card`, `secret`, `email`, `hostname`, dictionary rules, and so on). Under `strict`, a detection in a category the approval lists is sanitized; any other detection blocks.
- `file`: the record is a YAML file; it proves nothing beyond "someone wrote it", so it is for tracking and expiry only, and the docs say so.
- `signed`: the record has a detached signature verified against `approval.trusted_keys` supplied by the approving authority. Only this mode can be described as verified approval.
- **The signature scheme and record format are not decided** and depend on what the approving authority already issues (an open question below). Do not build `signed` before that is known.

### 4. Approved tool and account allowlists

- `allowed_upstreams` (exact hosts) is checked in addition to the existing HTTPS rule.
- Optional `approved_accounts`: SHA-256 hashes of account or workspace identifiers; the proxy hashes the account header Codex sends and blocks on a mismatch. A configured URL alone does not show that an account is the approved one; this is the control that does. The raw identifier is never stored or logged.

### 5. Operator experience

- `cui-proxy policy init [--profile strict|sanitize]` writes a policy file and a commented example approval.
- `cui-proxy policy check` validates the policy, approval, trusted keys and audit path, and prints what is enforced and what is not.
- `cui-proxy explain <request-id>` reads the decision log and says in plain words why a request was blocked and what would change that.
- The dashboard and menu-bar icon show the profile, approval status and days to expiry, and the count of blocks by reason.
- `cui-proxy lock` / `unlock`: a local kill switch that makes the proxy block everything, for incident response (requirement 6).

### 6. Documentation

`docs/govsec-mapping.md` maps each requirement above to the control that
addresses it, the setting that turns it on, how to verify it, and the limit,
so a reviewer can assess it without reading code. It states that the tool is
not a compliance control and does not replace the approving authorities.

## Limitations (explicit non-goals)

- **Classification.** Patterns and dictionaries cannot find every mission detail, health passage or restricted derivative. Human review and layered controls remain necessary.
- **Bypass.** Nothing in a local proxy stops a user running a client directly. That needs managed network and endpoint controls (option D).
- **Output review.** Peer review and change management happen outside the tool; it only records its own decisions.
- **Approval authenticity** exists only in `signed` mode and only as strong as key custody.
- **Tool identity.** Hashing the account header narrows it; it is not an attestation from the vendor.

## Consequences

**Good**
- Matches the policy's own structure: refuse by default, sanitize by approval.
- Produces evidence (decision log, approval record) instead of assertions.
- Each phase is independently useful and reversible; `sanitize` remains available.

**Costs and risks**
- `strict` blocks more: a false positive stops the turn, and Codex resends history, so one bad token can block every later turn. Mitigation: stable reason codes, `explain`, per-rule overrides, and the existing downgrade-to-redacted-token behavior only where an approval allows sanitizing.
- The decision log is sensitive metadata (rule names, counts, timing). Mitigation: 0600, no values, documented retention.
- Config surface grows. Mitigation: profiles, `policy init`, `policy check`, and unknown config keys remain an error.
- A weak approval mode can give false comfort. Mitigation: `file` mode is labelled as tracking only in the CLI output, dashboard and docs.

## Operability

**Rollout**
1. Run `policy check` and `test-rules` against synthetic samples that represent your categories.
2. Turn on `audit` first (no behavior change), confirm the log and `audit verify`.
3. Enable `strict` in a test environment; use `dry_run: true`, which records what `strict` *would* have decided while still enforcing the active profile (it never weakens the active profile).
4. Load the signed approval, confirm `policy check` reports its expiry, then enable in production.

**What an operator watches**
- Dashboard: blocks by reason, profile, approval days-to-expiry, audit status.
- Alert (via syslog or log shipping) on: `approval_expired`, `approval_invalid`, `audit_write_failed`, `account_mismatch`, a sustained rise in `blocked`.

**Failure modes**

| Event | Behavior | Recovery |
|---|---|---|
| Approval expired or invalid | All traffic blocked, message names the problem | Install a renewed approval, reload (no restart) |
| Audit file unwritable | Requests blocked (default) | Fix permissions or disk; `audit verify` after |
| Audit chain broken | `audit verify` reports the first bad line; the proxy keeps running and logs a warning | Investigate; archive the broken file and start a new chain, recording why |
| Trusted key rotated | Old approvals stop verifying | Distribute the new key, re-issue approvals |
| Clock wrong | Expiry misjudged | Fix time sync; `policy check` shows the clock it used |
| `lock` engaged | Everything blocked until `unlock` | Intentional; the decision log records lock and unlock |

**Routine tasks (runbook)**: renew the approval before its expiry (warning starts 14 days out); rotate trusted keys; export or ship the decision log; archive by retention; verify the chain on a schedule; tune rules from `explain` output using synthetic data only; review blocks weekly.

**Compatibility**: `policy.version` in the config; older configs keep working unchanged under the `sanitize` profile; the audit record schema is versioned (`v` field) and only ever extended.

**Testing**: unit tests per control; end-to-end tests asserting that each blocked or sanitized case never forwards a seeded value; fuzzing of the approval and signature parsers; mutation checks that disabling each control makes a test fail; an `audit verify` test that tampering is detected.

## Open questions

1. **What does an approval look like in practice at the target organization** (tracked in [ADR 0002](0002-approval-record-format.md)) (a ticket with an ID and expiry, an email, a signed form)? This decides the record format and whether `signed` is feasible. *Blocking for phase 3.*
2. Which signature mechanism can the approving authority actually produce and distribute (OpenSSH signatures, minisign, X.509, other)?
3. Which account or workspace identifier should `approved_accounts` match, and where may it come from?
4. Retention period for the decision log, and whether central shipping is required.
5. Should `strict` become the default at 1.0? (The 0.x default is decided: `sanitize`.)
6. Should header and query matches (already blocking) be reported under the same decision log reason codes? (Proposed: yes.)

## Implementation plan

Each phase is a release with its own tests and review.

| Phase | Delivers | Depends on |
|---|---|---|
| 1 | `policy.profile`, per-rule `action: block`, explicit `unsupported`, block reason codes, `policy init/check`, tests that strict never forwards a match | nothing |
| 2 | Persisted hash-chained decision log, `audit verify`, rotation/retention, fail-closed on write error, `explain` | phase 1 reason codes |
| 3 | Approval record (`file`, then `signed`), expiry handling, category-to-rule mapping, dashboard/menu-bar status | open questions 1-2 |
| 4 | `allowed_upstreams`, `approved_accounts`, `lock`/`unlock`, dry-run | phases 1-3 |
| 5 | `docs/govsec-mapping.md`, a tabletop review with GOVSEC, then decide the status of this ADR | all |

## Status log

| Date | Status | Note |
|---|---|---|
| 2026-11-07 | Proposed | Drafted from the review of the masking-only behavior; approval format pending |
| 2026-11-07 | Accepted | Maintainer accepted the plan, the `sanitize` default for 0.x with `strict` as a named profile, and the defaults table. Approval format split out to ADR 0002 (deferred); phase 3 stays blocked on it. Phase 1 build started. |
| 2026-11-07 | Phase 1 delivered | `policy.profile` (sanitize default, strict), per-rule `action`, `policy.unsupported`, reason code `policy` / kind `rule_match` (never forwarded by `on_error: passthrough`), `policy init` and `policy check` (`-require-strict`), `init -profile`, profile and per-rule action in the dashboard. Strict requires `on_error: block` and forbids `action: sanitize` until an approval can allow it. Phases 2-5 pending; phase 3 blocked on ADR 0002. |
