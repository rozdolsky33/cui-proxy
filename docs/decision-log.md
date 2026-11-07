# The decision log

cui-proxy writes one line per request to a file on disk saying what it did
with that request: **forwarded**, **sanitized** or **blocked**, why, and
which rules matched. The lines are hash-chained, so editing, reordering or
removing a line in the middle shows up in `cui-proxy audit verify`.
`cui-proxy explain <request-id>` reads the log and puts one decision into
plain words.

This is phase 2 of [ADR 0001](adr/0001-govsec-policy-enforcement.md). The
in-memory feed on the dashboard is separate: it holds the last 500 requests
and is lost on restart.

**What it is not.** It is not a compliance control and does not replace the
approving authorities. It records decisions this proxy made. It does not
record what a client did without the proxy. It is **tamper-evident, not
tamper-proof** (see [Limits](#limits)).

## Where it is, and its settings

On by default. Every setting needs a restart: a reload keeps the running
values and logs `WARN restart required for: audit....`.

```yaml
audit:
  enabled: true          # default true; false is refused under the strict profile
  file: /Users/you/.local/state/cui-proxy/decisions.jsonl
  on_error: block        # block | warn (default: block under strict, warn under sanitize)
  max_size_mb: 50        # rotate before a record would pass this size (1 or more)
  retention_days: 90     # delete rotated files older than this; 0 keeps them forever
```

- `file` defaults to `$XDG_STATE_HOME/cui-proxy/decisions.jsonl` (only when
  `XDG_STATE_HOME` is an absolute path), else
  `~/.local/state/cui-proxy/decisions.jsonl`, also on macOS. A relative path
  resolves against the config file's directory. `~` is not expanded.
- The directory is created `0700` and the file `0600`. If an existing file
  has wider permissions, they are reduced to `0600`.
- A symlink at `file` is refused: the file is opened with `O_NOFOLLOW`, and
  `serve` and `policy check` both refuse it.
- The file is locked (`flock`) while `serve` runs, so a second proxy with the
  same `audit.file` refuses to start. Two writers would break the chain.
- Unknown keys under `audit:` are an error, as elsewhere in the config.
- `check-config` and `policy check` only read the config. They never create
  the file or its directory. `policy check` shows the audit status: enabled
  or not, the file, `on_error`, rotation, retention, and that the log is
  tamper-evident, not tamper-proof.
- The dashboard's config panel shows the same settings, and the metrics
  include `audit_write_failed`.

## What is recorded

One JSON object per line, keys always in this order:

```json
{"ts":"2026-11-07T20:29:31.911376Z","request_id":"c8d64e73d6d88a6a","adapter":"codex","profile":"strict","decision":"blocked","reason":"policy","rule_ids":["builtin.ipv4"],"counts":{"builtin.ipv4":1},"approval_id":"","prev_hash":"0000000000000000000000000000000000000000000000000000000000000000","hash":"8651b8d9f9db06ac4f8211af9104259454c3371394819730e5906b492266a97e"}
```

| Field | Meaning |
| --- | --- |
| `ts` | when the decision was recorded, RFC 3339 with nanoseconds, UTC |
| `request_id` | 16 lowercase hex digits (8 random bytes), minted per request |
| `adapter` | the adapter name (`codex`) |
| `profile` | the active `policy.profile` (`sanitize` or `strict`) |
| `decision` | `forwarded` (sent upstream unchanged), `sanitized` (sent with values masked) or `blocked` (never sent) |
| `reason` | empty unless `blocked`; then one of `route`, `body_too_large`, `compressed`, `header`, `policy`, `sanitize`, `unrestorable`, `bad_request`, `internal` (the codes in the README's "Block diagnostics") |
| `rule_ids` | sorted names of the rules that matched |
| `counts` | per-rule match counts |
| `approval_id` | always `""` for now (reserved for approvals, ADR 0001 phase 3) |
| `prev_hash` | the previous record's `hash` (64 zeros for the first record of a chain) |
| `hash` | see [The hash chain](#the-hash-chain) |

Notes:

- For a `blocked` request with a rule, `rule_ids` holds the first blocking
  match and `counts` is `1`: scanning stops at that match.
- A request forwarded **unsanitized** because `policy.on_error` is
  `passthrough` is recorded as `forwarded`, with the counts key
  `passthrough:<reason>` (for example `"passthrough:sanitize":1`) and no
  rule ids. It is never recorded as a clean forward.
- Rule, adapter and profile names go through the same cleaning as the block
  diagnostics: bytes outside `[A-Za-z0-9._-]` become `_`, cut to 64 bytes.
- The record is built only from closed sets (decision, reason), the minted
  id, and those cleaned names. A record that does not fit that shape is not
  written. The write then counts as failed (see below).

## What is never recorded

Values, request or response bodies, prompts, headers (including
`Authorization`, cookies and `Chatgpt-Account-Id`), tokens, the URL, path
or query string, account ids, and anything else taken from a request or a
response. A test sends sentinel secrets in each of these places, and in a
dictionary rule's entries, and checks that none of them appears in the log
file's bytes.

The log is still sensitive metadata: rule names, counts and timing say what
kind of data someone worked with, and when. Keep the file `0600` and set a
retention period that fits your policy.

## When a record is written

- **Before forwarding.** The decision is known once the request has been
  checked and sanitized. The record is written and `fsync`ed before the
  request goes upstream. Every path that forwards goes through that one step:
  sanitized and clean requests, `GET /v1/models`, and
  `policy.on_error: passthrough` forwards.
- **Once per request.** Anything that happens later is not recorded: a
  redirect the proxy refuses to relay (`upstream_redirect`), a response
  stream it cuts (`stream_rewrite`), an upstream error. The proxy log line
  and the dashboard feed show those.
- **Blocked requests** are recorded too, with their reason.

### If a record cannot be written

| `audit.on_error` | Forwarded or sanitized request | Blocked request |
| --- | --- | --- |
| `block` (default under strict; the only value allowed there) | **blocked**: HTTP 503, code `audit`, message `request blocked (reason=audit kind=<class>)`; never forwarded, not even under `policy.on_error: passthrough` | stays blocked with its own reason |
| `warn` (default under sanitize; UNSAFE) | forwarded anyway | stays blocked |

In every case the failure is counted in `audit_write_failed`. A
`WARN decision record not written (<class>)` line is logged at most once a
minute, with the number of failures that were not logged. `<class>` is one
of `no_space`, `permission`, `symlink`, `in_use`, `closed`, `invalid`,
`damaged`, `io`. It never includes the path or the record. The `audit` code
itself is never recorded, since the failed write is the problem. Under
`strict`, a failed write blocks whatever `on_error` says, including after a
reload from `sanitize`. `serve` logs `WARN audit.on_error=warn: UNSAFE` at
startup when `warn` applies, and `WARN audit.enabled=false` when the log is
off.

A write that fails part-way is truncated back, so the file never keeps half
a line. If that truncation also fails, every later record fails until a
restart.

### At startup

`serve` reads the active file and checks its chain. It also checks that the
file's first record continues the newest rotated file. If the last line is
cut off, a line is corrupt or the chain is broken:

- under `on_error: block`, `serve` refuses to start:
  `decision log is damaged: <file> at line N (<kind>); run cui-proxy audit verify -file <file>, then move the file aside to start a new chain`.
- under `on_error: warn`, the damaged file is moved aside as a rotated file
  and a new file is started. Its first record's `prev_hash` is the hash of
  the last record that verified, and a WARN is logged. `audit verify` then
  reports the break **and** a gap. It never reports a silent pass.

## The request id

Every response carries the id:

- in the error message, which ends with `request_id=<id>`, for example
  `cui-proxy: request blocked (reason=policy kind=rule_match rule=builtin.ipv4) request_id=c8d64e73d6d88a6a`;
- in the error object's `request_id` field;
- in the `X-Cui-Proxy-Request-Id` response header, on every response,
  including successful ones;
- in the dashboard feed, as the row's tooltip.

## The hash chain

```
hash = hex( SHA-256( prev_hash_bytes || canonical_json ) )
```

- `prev_hash_bytes` is the 32 bytes that the previous record's hex `hash`
  decodes to. The first record of a chain uses 32 zero bytes (`prev_hash` is
  64 zeros).
- `canonical_json` is the record without the `hash` key: the keys in the
  order of the table above, no spaces, the `rule_ids` sorted, the `counts`
  keys sorted, integers in decimal. `prev_hash` is part of it. Every string
  is ASCII from a set that needs no escaping.
- The line on disk is `canonical_json` with `,"hash":"<hash>"` inserted
  before the closing brace, then a newline.
- A golden vector computed outside Go (Python `hashlib`) is checked in
  `internal/decisionlog/record_test.go`.

`audit verify` re-encodes each line and requires the line to match that
encoding byte for byte. A reformatted line, an extra or repeated key, or a
reordered key is reported as damage, even when the values are unchanged.

## Verifying

```sh
cui-proxy audit verify -config ~/.config/cui-proxy/config.yaml
cui-proxy audit verify -file /path/to/decisions.jsonl
cui-proxy audit verify          # the default file
```

It checks every file: the rotated files, oldest first (by the timestamp in
their name), then the active file. It prints:

- the files, in chain order;
- the number of records checked;
- the trusted start (see retention below);
- the head hash: the last record's hash, with its file and line;
- the **first break**, if any (file, line and kind):
  - `hash_mismatch`: the line was edited;
  - `prev_hash_mismatch`: a line before it was removed, inserted or
    reordered, or a whole middle file is missing;
  - `truncated`: the last line is cut off;
  - `malformed`: not a valid record line;
- any later breaks, and **gaps**: places where the chain picks up again
  after damage, with the lines that are not part of it.

Exit codes: `0` intact, `1` any break or gap, a read error, or no log at
all, `2` usage error. `audit verify` and `explain` are read-only.

Tip: record the head hash somewhere the proxy's user cannot write, on a
schedule (a ticket, a central log, a signed note). That copy is the
external anchor the chain does not have, and it turns "the tail was cut" and
"everything was rewritten" from undetectable into detectable up to that
point.

## Explaining a request

```sh
cui-proxy explain c8d64e73d6d88a6a
cui-proxy explain -config ~/.config/cui-proxy/config.yaml c8d64e73d6d88a6a
```

It finds the record (in the active or a rotated file) and states the
decision, the reason, the rules and counts, and what would change the
outcome. The wording is the same as the block diagnostics. Because the log
holds no values, it cannot show what matched. Reproduce that with synthetic
text (`cui-proxy test-rules` or the dashboard's test box). The log also has
no `kind` field, so for a `sanitize` block the proxy log line for that
request names the kind. An unknown id exits 1.

## Rotation and retention

- Before a record would make the active file larger than `max_size_mb`, the
  file is renamed to `<file>.<UTC timestamp>` (for example
  `decisions.jsonl.20261107T202931.911376000Z`). A new file is started, and
  its first record's `prev_hash` is the last hash of the rotated file, so
  the chain runs across files.
- At startup and after each rotation, rotated files whose name timestamp is
  older than `retention_days` are deleted. The active file is never deleted,
  and files whose name does not match `<file>.<timestamp>` are left alone.
  `retention_days: 0` keeps everything.
- Deleting the **oldest** rotated files is allowed. `audit verify` then
  treats the oldest surviving file's first `prev_hash` as the **trusted
  start** and prints it. It cannot tell a file that retention deleted from
  one someone else deleted. If you need to show that nothing was removed,
  keep the head hashes elsewhere (see the tip above) or set
  `retention_days: 0` and archive the files yourself.
- A missing **middle** file is a break (`prev_hash_mismatch` at the first
  record of the next file).

## Limits

- **Tamper-evident, not tamper-proof.** Without an external anchor, a hash
  chain detects casual edits and damage in the middle (an edited, removed,
  inserted or reordered line, a line cut in half). It does **not** detect a
  full rewrite by someone who can recompute every hash, since the algorithm
  is public and nothing is secret. It also does not detect deleting lines
  from the end of the log, or the newest files: what remains is a shorter
  chain that still verifies. Anyone who can write the file can do both.
- The chain is anchored only at its start (64 zeros) or at the trusted start
  after retention. Keeping head hashes elsewhere is the mitigation.
- It records the proxy's decisions only. Traffic that does not go through
  the proxy is not in it (see the README's limitations and ADR 0001).
- The record holds no `kind` (for `sanitize` blocks), no schema version
  field and no approval yet (`approval_id` is always empty). The ADR's
  operability section plans a versioned `v` field. It is not in this
  version. Adding it will be a schema change that `audit verify` will have
  to accept alongside today's records.
- `fsync` is called for every record, which costs a little latency per
  request. It is there so a record that was acknowledged is on disk before
  the request leaves.
- macOS and Linux only (the file lock and `O_NOFOLLOW`). The proxy does not
  build for Windows today anyway.

## Not in this release

- **Syslog or other central output.** It is deferred. Ship the file with
  your own log collector, and keep in mind that the shipped copy is a useful
  external anchor.
- An external anchor or signature for the chain.
- Approval ids (ADR 0001 phase 3, blocked on ADR 0002).
