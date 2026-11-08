# ADR 0002: Approval record format and verification

- **Status:** Deferred (blocked on input from the approving authority)
- **Date:** 2026-11-07
- **Deciders:** project maintainer, with the security / legal authority that issues approvals at the target organization
- **Related:** [ADR 0001](0001-govsec-policy-enforcement.md) section 3 and open questions 1-2

Current implementation: profiles and decision logging have shipped; verified
approvals, account allowlists and policy locking have not. Example fields in
this ADR are proposals, not supported YAML.

## Why this is deferred

ADR 0001 has the proxy verify, before it sanitizes or forwards anything, that
the use is covered by an approval. What counts as "an approval" is defined by
the organization that issues them, not by this project. At the time of
writing we do not know what one looks like in practice (a ticket with an ID and
expiry, an email, a signed form, an entry in an approvals system). An invented
format would either not match anything real, or would give false comfort by
looking official while proving nothing.

**Decision: do not build verified approvals (phase 3 of ADR 0001) until the
questions below are answered.** Everything that does not depend on the answer
ships first (profiles, blocking, the decision log, allowlists, lock). This ADR
records the options and the design so the decision can be made quickly when
the input arrives.

## Information needed (checklist for the approving authority)

Answering these closes this ADR.

1. **What artifact** is issued when a use of an external AI tool is approved?
   (Ticket, email, signed form, record in an approvals tool, other.)
2. Does it carry a **stable identifier** and an **expiry or review date**?
3. Which of these does it state: the **tool**, the **account or workspace**,
   the **data categories**, the **business need**, the **approver**, the
   **required controls**, the **retention**?
4. **Who can issue it**, and is there a **way to verify** that it is genuine?
   (A signing key, a certificate, a lookup service, a registry.)
5. Is it **per person**, per team, or per system? Can one approval cover many
   machines and users?
6. How is it **renewed and revoked**, and how fast must a revocation take effect?
7. What must be **retained**, for how long, and **where** (for audit)?
8. May the proxy hold a **public key or CA certificate** for verification, and
   who distributes updates to it?

## Constraints any design must meet

- Fail closed: missing, malformed, expired, unverifiable, or not covering the
  configured tool means all traffic is blocked, with a message naming the problem.
- Never record or print sensitive payloads; the decision log references the
  approval by its ID only.
- Verification is local and offline-capable; no network call on the request path.
- Format is versioned and only ever extended; unknown fields are an error.
- A reviewer can read the record without special tools.
- Clear labelling: only a record verified against a key the approving authority
  controls may be called "verified". Anything else is "tracking only".

## Options

| Option | What it is | Fits when | Weakness |
|---|---|---|---|
| **A. Reference record** (`approval.mode: file`) | A YAML file the user or admin writes: approval ID (for example a ticket number), categories, expiry. The proxy enforces expiry and scope but cannot tell who wrote it. | The organization has an approval ID and expiry but no signing | Proves nothing about authenticity; **tracking only**, labelled as such |
| **B. Signed record, OpenSSH signature** | The record file plus a detached signature made with `ssh-keygen -Y sign`; the proxy verifies against an allowed-signers list from the approver | The authority can use SSH keys; easy for operators to create and inspect | Needs key custody and distribution; signature format must be parsed or delegated to `ssh-keygen` |
| **C. Signed record, minisign / age-style key** | Small, modern detached signatures | The authority is free to choose tooling | Another tool for them to adopt |
| **D. Signed record, X.509 / CMS** | Signature with a certificate chaining to an organization CA | The authority already has a PKI and issues certificates | Heavier parsing and revocation (CRL/OCSP) questions, hard offline |
| **E. External verification** | The proxy checks an approvals service when it starts or on a timer | A system of record already exists | Needs network access and an API; the request path must still work offline; couples the proxy to that system |
| **F. Wrapper around an existing artifact** | A small importer converts the organization's existing form or ticket export into the record | The artifact is already issued and signed elsewhere | Fragile if the source format changes |

## Provisional design (used when the decision is made; not final)

Record (version 1), fields taken from the exception requirements in ADR 0001:

```yaml
version: 1
id: APPR-2026-0042          # stable reference an auditor can look up
tool: codex-cli
upstreams: [chatgpt.com]
accounts_sha256: []         # optional: hashes of approved account or workspace IDs
data_categories: [ipv4, hostname, ocid]   # rule classes the approval allows to be sanitized
business_need: "short text"
approver: "role or name"
controls: [dictionary-reviewed, output-peer-review]
retention: "180d"
issued: 2026-11-01
expires: 2027-05-01
```

Behavior (decided in ADR 0001, repeated for clarity):

- `strict` blocks every detection except categories listed in `data_categories`, which are sanitized.
- A detection in a category the approval does not list is blocked.
- An approval that is missing, malformed, expired, unverifiable, or that does not list the configured upstream blocks all traffic.
- A warning appears in the dashboard, the menu-bar icon and `policy check` 14 days before expiry.
- `approval.mode: file` is the only mode possible without the answers above and is labelled "tracking only" everywhere it is shown.

Verification step (only for signed modes): detached signature over the exact
bytes of the record, checked against `approval.trusted_keys`; the signed bytes
are never re-serialized before checking.

## What can ship now without this decision

- Phases 1, 2, 4 and 5 of ADR 0001.
- In phase 3, `file` mode with expiry and scope enforcement, clearly labelled
  tracking-only, if wanted. Signed modes wait for this ADR.

## Operability notes (apply to whichever option is chosen)

- Expiry and revocation: the record is re-read on reload (SIGHUP, the dashboard button or the menu-bar item); expiry is evaluated against the clock on every request.
- Renewal: install the new record and reload; no restart.
- Key rotation: `approval.trusted_keys` may list several keys during a rotation; remove the old key after the last old-key approval expires.
- Failure messages name the problem (expired, invalid signature, wrong tool, wrong category) and never include the record's free-text fields.
- Test with synthetic records and keys only; no real approvals or keys in the repository.

## Decision procedure

1. Send the checklist to the approving authority (an ISSM or GOVSEC contact).
2. Record the answers in the status log below.
3. Pick an option (or a combination such as A for now, B later), update this ADR to `Accepted`, and unblock phase 3 of ADR 0001.
4. If the answers show no signable artifact exists, keep A (tracking only) and say so in `docs/govsec-mapping.md`; do not claim verified approval.

## Status log

| Date | Status | Note |
|---|---|---|
| 2026-11-07 | Deferred | Options and provisional design recorded; waiting on the checklist answers |
