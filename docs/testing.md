# Test your setup

Test in layers. A YAML parser, a string preview, and a live Codex round trip
answer different questions. Use fabricated values throughout so the test itself
does not expose real information.

| Layer | What it proves | What it does not prove |
|---|---|---|
| `check-config` | YAML, rule files, supported classes and upstream configuration load | Running process uses that file; upstream works |
| `policy check` | Effective profile and configured restrictions | Organizational approval or complete coverage |
| `test-rules` / dashboard preview | Detection and policy for that text | HTTP/metadata parsing, credentials, routing or stream restoration |
| Dashboard health/config | Local UI responds and reports active configuration | Model backend is reachable |
| Live synthetic Codex run | Routing and the tested model-response path | Every payload type or future client version |
| Tool round trip | Tested tool-output and argument restoration path | All tools, permissions or external destinations |

## 1. Validate the file you actually use

```sh
cui-proxy version
cui-proxy check-config -config ~/.config/cui-proxy/config.yaml
cui-proxy policy check -config ~/.config/cui-proxy/config.yaml
cui-proxy info -config ~/.config/cui-proxy/config.yaml
```

Use your installation's actual path. `info` reads a file; it does not prove the
running service loaded it. Compare the dashboard's effective configuration and
startup log after reload/restart.

## 2. Build a small positive and negative test set

For the [downloadable masking recipe](recipes.md), these are useful cases:

| Fabricated text | Expected under sanitize | Expected under strict |
|---|---|---|
| `db.demo.example` | Dictionary match, masked | Blocked |
| `DB.DEMO.EXAMPLE` | Case-insensitive dictionary match | Blocked |
| `xdb.demo.examplex` | No dictionary match at alphanumeric boundaries | Unchanged if no other rule matches |
| `203.0.113.42` | Built-in address match, masked | Blocked |
| `DEMO-123456` | Custom ticket regex match | Blocked |
| `DEMO-12345` | No ticket regex match | Unchanged if no other rule matches |
| `Explain retry backoff` | Unchanged | Unchanged |

```sh
printf '%s\n' 'Check db.demo.example at 203.0.113.42 for DEMO-123456' |
  cui-proxy test-rules -config /path/to/recipe/config.yaml
```

For a masking result, stdout contains the transformed text and stderr contains
rule/class/count rows. Strict or explicit blocking rules produce failure
without forwarding anything. Expected rule names depend on your YAML.
The stock starter does not contain the tutorial's ticket rule.

Use the same samples in [Test a string](dashboard.md). A new throwaway vault is
used each time, so compare the rule names, counts and remaining text rather
than an exact synthetic spelling. “No matches” only describes the configured
detectors and that input; it does not mean the text is safe.

## 3. Check local service availability

```sh
curl -fsS http://localhost:8788/healthz
```

Expect `ok`. For the tutorial instance use port 18788. This endpoint belongs
to the **dashboard**, not the model-facing listener. Open the UI and check its
profile, upstream, listeners and rule actions. Start with [troubleshooting](troubleshooting.md)
if they differ from your intended settings.

## 4. Verify routing and response restoration

Launch Codex using the [one-session command](codex.md#try-it-for-a-single-session)
and submit:

```text
Reply with 203.0.113.42 exactly. Do not run tools.
```

After completion, check:

1. A `POST /v1/responses` row appears in this proxy's dashboard.
2. Address-rule replacements increase. Other client context may also match.
3. If the model echoes the value, the client sees the original fabricated
   address and restore counts increase.
4. The decision record says sanitized (or blocked under strict), with rule
   names and counts but no raw text.

The feed only gets a row when the request finishes. A model may not follow an
exact-echo instruction; inspect the evidence before concluding restoration is
broken. The UI does not expose the upstream body. For wire-level evidence,
follow the controlled, synthetic [backend verification procedure](verification.md).

## 5. Verify a harmless tool round trip

Use a new conversation through the proxy and ask:

```text
Run printf '%s\n' '203.0.113.42' in the shell, then repeat its output exactly.
Do not contact any host or change any files.
```

If your Codex execution policy allows the command, this exercises restored tool
arguments, local execution, sanitization of tool output in the next request,
and the final streamed reply. Inspect the executed command and final output in
your client. Permission denial is a separate client/tool issue; it does not
prove a proxy policy block.

For JSON/newline and workspace metadata regressions, use the specific cases in
[verification evidence](verification.md). Do not replace them with a single
plain-text preview: they test different parser paths.

## 6. Check strict or selective blocking

Use an isolated config such as the [tutorial](tutorial.md) to avoid changing a
live session unexpectedly. A string matching a blocking rule should fail in
the preview. A matching complete request should receive a value-free `422`
diagnostic. Check the request ID with:

```sh
cui-proxy explain -config /path/to/config.yaml REQUEST-ID
cui-proxy audit verify -config /path/to/config.yaml
```

Some guards, such as malformed metadata or an unknown route, block independently
of rule matches. See [Block diagnostics](block-diagnostics.md).

## Check attachment conversion (unreleased)

`test-rules` and the dashboard text box do not invoke PDF extraction or OCR.
After building/upgrading to a binary that supports [attachments](attachments.md),
check the effective setting and use fabricated inline documents with a controlled
fake upstream. Confirm that it receives only sanitized `input_text`, with no
`file_data`, `image_url`, filename, or original sensitive test value. Under strict,
the same detected value must block with no upstream call. Also test disabled
mode, file IDs/URLs, invalid data and missing tools; inspect `explain REQUEST-ID`.

Repository tests cover this boundary with real Poppler when installed, and
controlled OCR executables for deterministic invocation/failure checks. Actual OCR
quality and the client wire format require separate deployment verification.

## After edits and upgrades

Repeat the local samples whenever you change rules. After a binary/client/model
upgrade, restart the running proxy and repeat the routing, streaming and tool
checks. Record versions and synthetic cases that passed. Preserve a working
configuration backup so a failed rule edit can be reverted and reloaded.

The repository runs executable checks of its downloadable examples and a strict
documentation build in CI. This validates the examples; it does not replace
testing your own dictionaries, network environment or backend account.
