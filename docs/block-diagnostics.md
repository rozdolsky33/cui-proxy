# Block diagnostics

A blocked request is answered with an OpenAI-style JSON error whose `code`
is the block reason (`route`, `body_too_large`, `compressed`, `header`,
`policy`, `sanitize`, `unrestorable`, `bad_request`, `client`, `audit`, `internal`; `audit` is HTTP 503 and means
the decision log could not be written, see [Decision log](../README.md#decision-log)). When the body or structured metadata could
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
| `pool_exhausted` | no synthetic left for the class (see Synthetic address pool) | restart with a fresh vault (memory mode) or remove the vault file |
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
