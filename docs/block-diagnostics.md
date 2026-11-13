# Block diagnostics

A blocked request is answered with an OpenAI-style JSON error whose `code`
is the block reason (`route`, `body_too_large`, `compressed`, `header`,
`policy`, `sanitize`, `unrestorable`, `bad_request`, `client`, `audit`, `internal`; `audit` is HTTP 503 and means
the decision log could not be written, see [Decision log](https://github.com/rozdolsky33/cui-proxy/blob/main/README.md#decision-log)). When the body or structured metadata could
not be sanitized, the message also names a kind and, when one span caused
it, the detector rule (a name from your config, never a value):

```
cui-proxy: request blocked (reason=sanitize kind=pool_exhausted rule=builtin.ipv4)
```

Every error also carries a `request_id=` you can give to `cui-proxy explain`.
The same appears in the proxy log as
`request blocked (reason=... kind=... rule=... class=...)`, and in the
dashboard feed as `sanitize:<kind>` or `header:<kind>` with a `blocked:<rule>` count. The
metrics count blocks by reason only.

| kind | meaning | what to do |
| --- | --- | --- |
| `pool_exhausted` | no synthetic left for the class (see Synthetic address pool) | plan a fresh vault only after ending affected conversations; old synthetics cannot be restored without their mappings. Preserve the existing file/key pair before switching to new file paths, or restart memory mode |
| `ambiguous` | a value equals an issued overflow synthetic (or a zero-padded spelling of one) | see Synthetic address pool |
| `pseudonym` | no synthetic can be made for the value (e.g. an invalid CIDR from a custom rule) | fix the rule's pattern or `synth` class |
| `rule_match` | a blocking rule matched (code `policy` for the body, `header` for metadata: strict profile or `action: block`) | expected: remove the value, or use the `sanitize` profile / a rule without `action: block` if masking is allowed |
| `unrestorable` | the sanitized text would not restore even after downgrading (code `unrestorable`) | rare: report the rule and kind from the log line |
| `bad_body` | invalid JSON/UTF-8 or unsupported content; for metadata also invalid shape, repeated headers, over 64 KiB, or a match in an unchanged field | check the request shape and reported rule using fabricated input |
| `depth` | JSON nested deeper than 64 levels | expected for pathological input |
| `other` | any other failure | check the log line |

A value whose synthetic would not restore where it stands (for example
`id:fe80::1`, `ID123-45-6789` or `x4111 1111 1111 1111`, where the
address/number synthetic would be glued to a letter or hex digit) is not
blocked: it is masked as a `REDACTED-<8hex>` token instead and counted as
`downgraded` in the metrics.

For Git/SSH failures, first identify whether the error comes from the model
request or the local command. See the [configuration walkthrough](configuration.md#git-and-ssh-troubleshooting)
for the decision table and supported overrides.

## Attachment-specific details

Attachments default to `policy.attachments: block`. With
`policy.attachments: extract_text`, supported inline PDFs and PNG/JPEG images
are converted locally, then sanitized; original attachment bytes are never
forwarded. See [Attachment inspection](attachments.md) for tools and limits.

Attachment errors retain `reason=sanitize kind=bad_body` and add a safe
`detail=attachment_<category>_<cause>` plus a fixed remedy. For example,
`attachment_file_disabled` identifies the default refusal;
`attachment_file_reference` identifies a file ID or URL that cannot be inspected;
`attachment_pdf_missing_tool` identifies missing local extraction software.
`shape`, `unsupported`, `invalid`, `limit`, `failed`, `empty`, and `timeout`
distinguish other failures. Details are persisted for `cui-proxy explain` without
filenames or contents. Old records without details cannot recover the cause.

## Recovery metadata and repeated refusals

Proxy-generated JSON errors retain their existing HTTP status, `error.code`,
`error.type`, message and request ID. They additionally include `error.recovery`
with a closed `code`, `category`, `retry`, and fixed `remedy` text. The headers
`X-Cui-Proxy-Recovery-Code` and `X-Cui-Proxy-Retry` carry the same guidance.
No original values are included. Already-started streams are still aborted on
restoration failure; recovery metadata cannot be added after response headers.
Provider error responses retain provider semantics and are not reclassified.

| Category | Examples | Retry action |
| --- | --- | --- |
| policy | blocking rule, rejected metadata | `after_change`: remove rejected content/metadata; keep protection enabled |
| representation | unsupported route, attachment shape/format, compressed input | `after_change`: use supported input, remove the rejected representation from history |
| dependency | missing extraction tool, audit storage failure | `after_repair`: repair the service environment or storage first |
| budget | body/depth/extraction limits, synthetic pool exhaustion, processing timeout | `after_change`: reduce the workload; preserve mappings needed by active sessions |
| overload | extraction queue deadline (`attachment_*_busy`) | `backoff`: bounded delay and lower attachment concurrency |
| transient | upstream connection failure | `backoff`: check task state because delivery may be uncertain, then retry with a bound |
| internal | unclassified failure | `after_repair`: investigate safe logs and request ID before retrying |

For deterministic categories, `category_occurrences` counts that category across
all clients of one handler in a one-minute window (saturating at one million).
It is **not** proof of an identical request or a particular client's retry loop.
The tracker has at most 128 entries, uses only closed diagnostic codes, expires
old entries and is never persisted. It stores no bodies or content hashes.
Transient/overload categories have no recurrence count. `cui-proxy explain`
provides recovery guidance from the persisted reason/attachment detail; it does
not invent recurrence counts or recover kinds absent from the decision record.

Client retry behavior is advisory: the proxy cannot force Codex or another
client to honor custom headers/fields. No end-to-end suppression claim is made.
If the same image or blocked value is retained in history, cancel the retrying
operation, replace/remove that content locally, then start a fresh session with
only permitted context if the client keeps resending it. Reopening the same
session or repeating the same request is not a remedy. The proxy never deletes
context or relaxes policy automatically.

## Vault persistence refusal

HTTP 503 with reason `vault` means the required mapping checkpoint failed. The request was not forwarded. Repair storage space, path availability or permissions, then retry; preserve the vault and its key. Recovery metadata classifies this as a `dependency` requiring `after_repair`. See [mapping recovery](vault-recovery.md).
