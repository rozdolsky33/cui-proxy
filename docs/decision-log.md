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
values and logs `WARN restart required for: audit....`. A reload that only
changes `policy.profile` does not count as an `audit.on_error` change, even
though the `on_error` default follows the profile. A reload to `strict` while
the log was disabled at startup is **refused**: the old config stays active,
and the error says to restart. Strict never runs without the log.

```yaml
audit:
  enabled: true          # default true; false is refused under the strict profile
  file: /Users/you/.local/state/cui-proxy/decisions.jsonl
  on_error: block        # block | warn (default: block under strict, warn under sanitize)
  max_size_mb: 50        # rotate before a record would pass this size (1 or more)
  retention_days: 90     # 0..106751 days; 0 keeps rotated files forever
```

- `file` defaults to `$XDG_STATE_HOME/cui-proxy/decisions.jsonl` (only when
  `XDG_STATE_HOME` is an absolute path), else
  `~/.local/state/cui-proxy/decisions.jsonl`, also on macOS. A relative path
  resolves against the config file's directory. `~` is not expanded.
- **The directory must be private.** It is created `0700`. An existing
  directory is refused, by `serve` and by `policy check`, when:
  - it is a symlink, or not a directory;
  - another user owns it;
  - group or others can write to it (for example `/tmp`).

  Someone else who can write there could plant, rename or delete files to
  forge or hide records. Only the log's own directory is checked; its parent
  directories are not.
- The file is `0600` and must be owned by you. An existing file with wider
  permissions is narrowed to `0600`.
- A symlink at `file` is refused: every file is opened with `O_NOFOLLOW`.
- Next to the log, `serve` keeps two small files. Neither holds a record
  value.
  - `<file>.lock` is locked (`flock`) for as long as `serve` runs, including
    during rotation, so a second proxy with the same `audit.file` refuses to
    start. Two writers would break the chain.
  - `<file>.retention` holds one line per rotated file that retention
    deleted (see [Rotation and retention](#rotation-and-retention)).
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
| `decision` | `forwarded` (no masking, or explicit body passthrough), `sanitized` (sent with values masked) or `blocked` (never sent) |
| `reason` | empty unless `blocked`; then one of `route`, `body_too_large`, `compressed`, `header`, `policy`, `sanitize`, `unrestorable`, `bad_request`, `client`, `audit`, `internal` (the codes in [Block diagnostics](block-diagnostics.md)) |
| `rule_ids` | sorted names of the rules that matched |
| `counts` | per-rule match counts |
| `approval_id` | always `""` for now (reserved for approvals, ADR 0001 phase 3) |
| `prev_hash` | the previous record's `hash` (64 zeros for the first record of a chain) |
| `hash` | see [The hash chain](#the-hash-chain) |

Notes:

- Successful `workspaces` and `cwd` metadata replacements share the request
  vault and count alongside body replacements. Replacement counts are committed
  only after the whole request is eligible for forwarding. Original paths and
  remote URLs are never recorded.

- For a `blocked` request with a rule, `rule_ids` holds the first blocking
  match and `counts` is `1`: scanning stops at that match.
- A request whose body is forwarded **unsanitized** because `policy.on_error` is
  `passthrough` is recorded as `forwarded`, with the counts key
  `passthrough:<reason>` (for example `"passthrough:sanitize":1`) and no
  body replacement counts. Successfully sanitized workspace metadata retains
  its rule ids and counts alongside this marker. The proxy sets this marker
  in a separate field. It is never
  taken from a rule name, so a rule named `passthrough:sanitize` is recorded
  as the rule `passthrough_sanitize`.
- **At most 64 rules per record.** A request that matches more distinct
  rules keeps the 64 with the highest counts (ties by name) and adds
  `"truncated:rules":<number left out>`. This keeps every record well under
  the 64 KiB line limit that writer and verifier share (the longest possible
  record is about 10 KiB). The decision is still recorded, so this is
  fail-closed safe. The writer refuses, and never writes, a line the verifier
  would reject.
- Rule, adapter and profile names go through the same cleaning as the block
  diagnostics: bytes outside `[A-Za-z0-9._-]` become `_`, cut to 64 bytes.
- The record is built only from closed sets (decision, reason), the minted
  id, and those cleaned names. A record that does not fit that shape is not
  written. The write then counts as failed (see below).

## What is never recorded

Values, request or response bodies, prompts, headers (including
`Authorization`, cookies and `Chatgpt-Account-Id`), tokens, the URL, path
or query string, account ids, and anything else taken from a request or a
response. A test sends sentinel secrets through every path:
- a sanitized forward;
- header, query, route and policy blocks;
- an `on_error: passthrough` forward;
- a sanitize block with a diagnosis;
- an audit-failure block.

They travel in the body, headers, query, path and a dictionary rule's
entries. The test checks that none of them appears in the log file's bytes,
the error bodies or the log lines.

**Rule names are recorded verbatim** (after the cleaning above). Do not name
a rule after something sensitive, such as a project codename, a customer or
a host: the name lands in every record that rule matches. Name it after the
category instead (`customer-hosts`, `aws-keys`).

The log is still sensitive metadata: rule names, counts and timing say what
kind of data someone worked with, and when. Keep the file `0600` and set a
retention period that fits your policy.

## When a record is written

- **Before forwarding.** The decision is known once the request has been
  checked and sanitized. The record is written and `fsync`ed before the
  request goes upstream. Every path that forwards goes through that one step:
  sanitized and clean requests, `GET /v1/models`, and
  `policy.on_error: passthrough` forwards.
- **Once per request, at the moment of forwarding.** Anything that happens
  later is not recorded:
  - a redirect the proxy refuses to relay (`upstream_redirect`);
  - a response stream it cuts (`stream_rewrite`);
  - an upstream error;
  - an internal failure after the record.

  The proxy log line and the dashboard feed show those. If the proxy fails
  internally after the record was written, the client gets an error (500)
  but the record still says `forwarded` or `sanitized`. The request may or
  may not have reached the upstream by then. The proxy then logs
  `request <id> failed internally after its decision was recorded as <decision>`,
  so the two can be matched by id. This never fails open: the proxy never
  sends anything that was not recorded first.
- **Blocked requests** are recorded too, with their reason.

### If a record cannot be written

| `audit.on_error` | Forwarded or sanitized request | Blocked request |
| --- | --- | --- |
| `block` (default under strict; the only value allowed there) | **blocked**: HTTP 503, code `audit`, message `request blocked (reason=audit kind=<class>)`; never forwarded, not even under `policy.on_error: passthrough` | stays blocked with its own reason |
| `warn` (default under sanitize; UNSAFE) | forwarded anyway | stays blocked |

- In every case the failure is counted in `audit_write_failed`.
- A `WARN decision record not written (<class>)` line is logged at most once
  a minute, with the number of failures that were not logged.
- `<class>` is one of `no_space`, `permission`, `symlink`, `in_use`,
  `closed`, `invalid`, `damaged`, `unsafe_dir`, `io`. It never includes the
  path or the record.
- The `audit` code itself is never recorded, since the failed write is the
  problem.
- Under `strict`, a failed write blocks whatever `on_error` says.
- At startup, `serve` logs `WARN audit.on_error=warn: UNSAFE` when `warn`
  applies, and `WARN audit.enabled=false` when the log is off.

A write that fails part-way is truncated back, so the file never keeps half
a line. If that truncation also fails, every later record fails until a
restart.

### At startup

`serve` reads the active file and checks three things:
- its own chain;
- that its first record continues the newest rotated file;
- that the log as a whole starts a chain (see head cuts below).

If the last line is cut off, a line is corrupt, or the chain is broken:

- under `on_error: block`, `serve` refuses to start:
  `decision log is damaged: <file> at line N (<kind>); run cui-proxy audit verify -file <file>, then move the file aside to start a new chain`.
- under `on_error: warn`, the damaged file is moved aside as a rotated file
  and a new file is started. Its first record's `prev_hash` is the hash of
  the last record that verified, and a WARN is logged. `audit verify` then
  reports the break **and** a gap. It never reports a silent pass.

A log whose start was cut (see `head_cut` below) is refused under `block`.
Under `warn` it only causes a WARN, because there is nothing to move aside:
the damage is in the history.

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
- `canonical_json` is the record without the `hash` key:
  - keys in the order of the table above, with no spaces;
  - `rule_ids` sorted, and `counts` keys sorted;
  - integers in decimal;
  - `prev_hash` is part of it;
  - every string is ASCII from a set that needs no escaping.
- The line on disk is `canonical_json` with `,"hash":"<hash>"` inserted
  before the closing brace, then a newline.
- A golden vector computed outside Go (Python `hashlib`) is checked in
  `internal/decisionlog/record_test.go`.

`audit verify` re-encodes each line and requires it to match that encoding
byte for byte. A reformatted line, an extra or repeated key, or a reordered
key is reported as damage, even when the values are unchanged.

## Verifying

```sh
cui-proxy audit verify -config ~/.config/cui-proxy/config.yaml
cui-proxy audit verify -file /path/to/decisions.jsonl
cui-proxy audit verify          # the default file
```

It checks every file: the rotated files, in sequence order, then the active
file. It prints:

- the files, in chain order;
- the number of records checked;
- the trusted start: genesis, or the retention note that explains a later
  start;
- the head hash: the last record's hash, with its file and line;
- the **first break**, if any (file, line and kind):
  - `hash_mismatch`: the line was edited;
  - `prev_hash_mismatch`: a line before it was removed, inserted or
    reordered, or a whole middle file is missing;
  - `truncated`: the last line is cut off;
  - `malformed`: not a valid record line;
  - `head_cut`: the first surviving record does not start the chain (its
    `prev_hash` is not 64 zeros), and no retention note says that
    retention deleted the file it continues. Lines or files were removed
    from the start of the log.
- any later breaks, and **gaps**: places where the chain picks up again
  after damage, with the lines that are not part of it.

Exit codes: `0` intact, `1` any break or gap, a read error, or no log at
all, `2` usage error. `audit verify` and `explain` are read-only and create
nothing.

**While `serve` is running**, the newest record may be half-written at the
moment verify reads the file. If the active file ends in a partial line and
`serve` holds the lock, verify does not count it as damage. It prints a note
saying a record was being written, and you can run verify again. With no
live writer, the same partial line is a `truncated` break. Someone could cut
the file mid-line while `serve` runs. That would also show as in flight,
until `serve` appends its next record. The cut line and the new record then
merge into a `malformed` line, which is detected.

Tip: record the head hash on a schedule somewhere the proxy's user cannot
write, for example a ticket, a central log or a signed note. That copy is
the external anchor the chain does not have. Up to that point it makes two
attacks detectable that otherwise are not: cutting the tail, and rewriting
everything.

## Explaining a request

```sh
cui-proxy explain c8d64e73d6d88a6a
cui-proxy explain -config ~/.config/cui-proxy/config.yaml c8d64e73d6d88a6a
```

It walks the log the way `audit verify` does, finds the record in the active
or a rotated file, and checks the record in place: its hash and its link to
the record before it. If the record does not verify, explain prints a
`WARNING: this record does NOT verify (<kind>)` before the details and exits
1. If it verifies, explain says so.

Explain states:
- the decision;
- the reason;
- the rules and counts;
- what would change the outcome, in the same wording as the block
  diagnostics.

Because the log holds no values, it cannot show what matched. Reproduce that
with synthetic text (`cui-proxy test-rules` or the dashboard's test box). The
log also has no `kind` field, so for a `sanitize` block, the proxy log line
for that request names the kind. An unknown id exits 1.

## Rotation and retention

- **Rotation.** Before a record would make the active file larger than
  `max_size_mb`, the file is renamed to `<file>.<seq>.<UTC timestamp>`, for
  example `decisions.jsonl.0000000003.20261107T202931.911376000Z`. `seq` is
  one more than the highest sequence number in the directory.
  - A new file is created and the directory is synced before the new file's
    first record. Its first `prev_hash` is the last hash of the rotated
    file, so the chain runs across files.
  - Files are ordered by sequence number, never by the clock, so a clock
    that steps back does not reorder them.
  - Rotated files named `<file>.<UTC timestamp>`, the naming of the first
    release, are still read, before every numbered file, in timestamp order.
- **Retention**, at startup and after each rotation, for `retention_days`
  greater than 0. It deletes rotated files whose name timestamp is older
  than `retention_days`, under these rules:
  - It works oldest first, and stops at the first file that does not
    qualify, so it never makes a hole in the middle of the chain.
  - It never deletes the newest rotated file or the active file, and never a
    file whose timestamp is in the future.
  - It deletes nothing at all while the clock reads earlier than the newest
    rotated file or the last record, which means the clock stepped back. It
    logs `WARN decision log retention skipped` instead.
  - Files whose name does not match the rotated pattern are never touched.
  - `retention_days: 0` keeps everything.
- **Retention notes.** Before deleting a file, retention appends a line to
  `<file>.retention` and syncs it. The line holds the time, the deleted
  file's name and its last record's hash, and no record content. With that
  note, `audit verify` accepts the oldest surviving file's first
  `prev_hash` as a **trusted start** and prints which deletion it continues.
  Without a matching note, a start that is not 64 zeros is a `head_cut`
  break. That covers lines cut from the top of a file, and a file deleted
  without retention, whatever `retention_days` is.
- A missing **middle** file is a break (`prev_hash_mismatch` at the first
  record of the next file).

## Limits

- **Tamper-evident, not tamper-proof.** Without an external anchor, a hash
  chain detects casual edits and damage: an edited, removed, inserted or
  reordered line, a line cut in half, or a start removed without a
  retention note. Anyone who can write the files can still:
  - **rewrite everything**, recomputing every hash: the algorithm is public
    and nothing is secret;
  - **cut the newest lines or files**: what remains is a shorter chain that
    still verifies;
  - **cut the oldest lines or files and add a matching line to
    `<file>.retention`**: the notes are not chained or signed either. A cut
    head is detected only when nobody also edits the notes.

  Keeping head hashes elsewhere is the mitigation for all three.
- **Clock.** Ordering does not depend on the clock. Retention does, by
  design, since retention is about age. Retention is skipped when the clock
  reads behind the log. A clock that is wrongly far **ahead** at startup or
  at a rotation looks exactly like a long idle period. Retention then
  deletes, early, every rotated file but the newest. Each deletion is noted,
  so verify shows it as retention, not as damage. Records carry the clock's
  `ts` as it read at the time.
- **Throughput.** Each record is `fsync`ed before the request leaves, one at
  a time under a lock. On macOS, Go's `File.Sync` issues `F_FULLFSYNC`,
  which flushes the disk's cache and takes several to tens of
  milliseconds. That caps one proxy at roughly 50-200 requests per second
  with the default settings. This is ample for one person using Codex, and
  it is the price of "on disk before the request leaves". Linux `fsync` is
  usually faster. (Batching the syncs of concurrent requests would raise
  the cap; it is not implemented.)
- It records the proxy's decisions only. Traffic that does not go through
  the proxy is not in it (see [limitations](limitations.md) and ADR 0001).
- The record holds no `kind` (for `sanitize` blocks), no schema version
  field and no approval yet (`approval_id` is always empty). The ADR's
  operability section plans a versioned `v` field. It is not in this
  version. Adding it will be a schema change that `audit verify` will have
  to accept alongside today's records.
- Only the log's own directory is checked for safety, not its parents.
- macOS and Linux only (the file lock, `O_NOFOLLOW` and the owner checks).
  The proxy does not build for Windows today anyway.

## Not in this release

- **Syslog or other central output.** It is deferred. Ship the file with
  your own log collector, and keep in mind that the shipped copy is a useful
  external anchor.
- An external anchor or signature for the chain or the retention notes.
- Approval ids (ADR 0001 phase 3, blocked on ADR 0002).
