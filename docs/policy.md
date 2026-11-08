# Policy profiles: sanitize or strict

`policy.profile` decides what a detection does.

| Profile | A detected value... | Use it when |
|---|---|---|
| `sanitize` (default) | is replaced with a synthetic value and restored in the reply | you want the model to keep working on the task and masking is allowed |
| `strict` | **blocks the whole request** before anything is forwarded | a policy forbids sending restricted data at all, or masking needs an approval you do not have |

```sh
cui-proxy policy init -profile strict      # a starter config with the strict profile
cui-proxy policy check -config ~/.config/cui-proxy/config.yaml
cui-proxy policy check -config ... -require-strict   # exit 1 unless strict (for scripts and audits)
```

Any rule can also set `action: block` to refuse a request that matches it
even under `sanitize` (for example a dictionary of names that must never
leave). Under `strict` every rule blocks and a rule may not set
`action: sanitize` (a verified approval may allow that in a later release; see
[docs/adr](https://github.com/rozdolsky33/cui-proxy/tree/main/docs/adr)). `strict` needs at least one rule (with none it would
detect nothing). **Any blocking rule (the strict profile or an
`action: block`) also requires `policy.on_error: block`**: the config is
refused otherwise, because `passthrough` forwards the original request when it
cannot be inspected, and a failure can hit before the blocking rule is checked.
The proxy enforces the same thing at runtime, whatever the config says.

- A policy block is a **decision, not a failure**: `policy.on_error:
  passthrough` never forwards it. The error code is `policy` (HTTP 422) and
  the message names the rule, never the value:
  `cui-proxy: request blocked (reason=policy kind=rule_match rule=builtin.ipv4)`.
- When any rule blocks, fields that are normally passed through unscanned so
  the protocol keeps working (`model`, ids, `role`, `status`, tool names and
  types) are **checked, not rewritten**, and so is every
  number in the request, not just text (number literals are expanded exactly,
  so `4.111111111111111e15` is checked as 4111111111111111). Known protocol
  `include` constants remain unscanned; unknown `include` strings are inspected.
  Another exception is the content of `encrypted_content` (the vendor's encrypted
  reasoning blobs, resent as received): it is not scanned, but while a rule
  blocks it must look like base64 ciphertext or the request is refused. A
  value written only with base64 characters could still ride in it; nothing
  short of scanning the blob would catch that, and scanning risks false
  positives on genuine ciphertext.
- The `Authorization`, `Cookie` and `Chatgpt-Account-Id` headers are
  credentials and are forwarded as they are. Codex `workspaces` and `cwd`
  metadata is sanitized under `sanitize`; strict/block rules still reject
  detections there with reason `header`. Other metadata fields, headers and
  the query string are inspected and a match blocks.
- The profile applies on reload (SIGHUP, the dashboard button or the
  menu-bar item); the running profile is shown in the dashboard header and
  logged on every start and reload. **A reload can lower the policy** (this is
  a local operator action): it is logged as
  `WARN policy weakened by reload: ...` when the profile goes from `strict` to
  anything else or fewer rules block.
  Switching to `strict` mid-session is safe for values the vault already
  issued as synthetics, which Codex resends in history: they do not block.
  One exception: an address from the `198.18.0.0/15` overflow pool can be a
  real address, so it does block, and because history is resent, so does every
  later turn of that conversation; start a new session.
- `policy.unsupported: block` is the only value and is the default: content
  the proxy cannot inspect (images, files, compressed bodies, unknown routes,
  oversized, too deeply nested or unparseable bodies) is always refused,
  **even under `policy.on_error: passthrough`**.
- `policy check` prints the effective action of every rule and states plainly
  what is enforced, what is not implemented yet (verified approvals and
  account allowlists) and what no local tool can do.

`strict` blocks more, and Codex resends the whole conversation each turn, so
one false positive can block every later turn. Tune the rules with synthetic
text (`cui-proxy test-rules`, the dashboard's test box) before switching.
Why this design, and what is deferred: [ADR 0001](https://github.com/rozdolsky33/cui-proxy/blob/main/docs/adr/0001-govsec-policy-enforcement.md).

For complete defaults, file locations and examples of mixed masking/blocking,
see the [configuration walkthrough](configuration.md#sanitize-strict-and-overrides).
There is no per-value allowlist or individual built-in detector toggle in v0.3.3.
