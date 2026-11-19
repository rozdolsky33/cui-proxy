# Review the proxy's security

There is no record of cui-proxy being reviewed by anyone outside the project.
Earlier findings and their fixes are listed under
[review findings addressed](assurance.md#review-findings-addressed). The
[roadmap](https://github.com/rozdolsky33/cui-proxy/blob/main/ROADMAP.md)
requires one external adversarial review before a 1.0 release.

This page is for a reviewer. It states the claims to attack, where the code
is, what is already known, how to test safely, and how to report.

## What counts as an independent review

- The reviewer took no part in writing the code or its tests.
- The reviewer chose their own methods. Review by the author, or by tooling
  the author directed, does not count.
- The review ends with a written statement of what was examined, including
  the areas where nothing was found.

A review that finds nothing still counts, as long as it says what it covered.

## The claims to attack

These are the properties the proxy is meant to hold. Breaking any one of them
is a finding.

1. **No detected value is forwarded.** A value that a configured rule matches
   does not reach the upstream in a supported request. This covers JSON
   string content and its escape sequences, numbers, object keys other than
   protocol field names, tool-call arguments, the `X-Codex-Turn-Metadata` header, other request headers and
   the query string.
2. **What cannot be inspected is refused.** A request the proxy cannot parse,
   or a shape it does not support, is blocked. It is never forwarded
   uninspected. The same holds for a successful response that cannot be
   restored.
3. **The proxy and the upstream read the same request.** No input makes the
   proxy inspect one thing while the upstream interprets another. Duplicate
   keys, invalid UTF-8, byte-order marks, trailing data and unusual numbers
   are the obvious places to look.
4. **Original values stay local.** They do not appear in service logs, the
   decision log, error bodies, metrics or the dashboard's activity views.
5. **Restoration is exact.** A response is restored only with originals from
   the local vault. It never restores the wrong original, never corrupts a
   tool call, and gives the same result however the stream is split into
   chunks.
6. **The local surface is closed to browsers.** A web page, including one
   that uses DNS rebinding, cannot reach the proxy API, the dashboard or the
   vault through them.
7. **The vault resists tampering and misuse.** Without the key, a vault file
   reveals no mappings. A modified, truncated or wrong-version file is
   refused. Two processes cannot overwrite the same vault.
8. **Failure does not open the proxy.** Overload, cancellation, a failed
   save, a failed audit write or a crash in one request does not cause
   another request to be forwarded unsanitized.

One further question has no claim attached, and an answer is welcome: what
does a synthetic value reveal about the original it replaces?

## Where the code is

| Area | Packages | Existing fuzz targets |
| --- | --- | --- |
| Request parsing and sanitizing | `internal/adapter/codex`, `internal/transform`, `internal/detect` | `FuzzSanitizeRequest`, `FuzzTurnMetadata`, `FuzzDecodeSimpleStringParity`, `FuzzOutputPlanStandardEncoder`, `FuzzWriteQuoteMatchesStandardJSON`, and seven detector targets in `internal/detect` |
| Response restoration | `internal/adapter/codex`, `internal/transform`, `internal/sse` | `FuzzSSERewrite`, `FuzzArgumentJSON`, `FuzzRoundTrip` |
| Routing, header checks, client and response guards | `internal/proxy` | None |
| Vault and key handling | `internal/vault` | None |
| Decision log and audit | `internal/decisionlog`, `internal/audit` | `FuzzRecordVerify`, `FuzzVerify` |
| Dashboard and local API | `internal/ui` | None |
| Attachment text extraction | `internal/attachment` | None |
| Configuration and commands | `internal/config`, `cmd/cui-proxy` | None |

[Life of a request](architecture.md) walks through the request path. The
[protocol matrix](protocol-support.md) lists the supported request and
response shapes. The design decisions are in the
[ADRs](https://github.com/rozdolsky33/cui-proxy/tree/main/docs/adr).

## What is already known

Read [known limitations](limitations.md) and the remaining limits in
[assurance](assurance.md#demonstrated-scope-and-remaining-limits) before you
start. The items below are documented behavior. A report that only restates
one of them is a feature request.

- Detection is by pattern and dictionary. A value that no rule matches is
  forwarded. That includes values in an encoding the rules do not describe,
  such as base64, and values the builtin rules do not cover, such as
  usernames and local paths.
- Credential headers are forwarded unscanned.
- The content of `encrypted_content` is not inspected.
- Some protocol identifier fields pass unscanned unless a blocking rule is
  configured.
- Other processes running as the same operating-system user are inside the
  trust boundary. So are the native client, the configuration, the vault key
  and the configured upstream.
- A client that does not use the proxy bypasses it.
- Restoring a value does not make a model-written command safe to run.
- Audit chains are tamper-evident but not externally anchored.

A new way to reach one of these outcomes through a path the documentation
calls protected is still a finding.

## Out of scope

- Attacks on the upstream service or on anyone else's systems.
- Social engineering, and physical access to the workstation.
- Resource exhaustion that only makes the proxy slow or unavailable. It is
  worth a note, and it becomes a finding if it leads to claim 8 failing.

## Test safely

- Test only an instance that you run on your own machine.
- Use made-up values throughout. Never use real sensitive data, real
  credentials or another person's data.
- Do not send attack traffic to the real Codex backend. Point the proxy at a
  listener you control.

Three ways to set up a target, from least to most effort:

1. **Detection only.** `cui-proxy test-rules -config PATH` reads text from
   standard input, writes the sanitized text, and makes no upstream request.
   It uses a throwaway vault.
2. **A local upstream.** Build the proxy, create a configuration with
   `cui-proxy init -dir PATH`, and set the `codex` entry under `upstreams` to
   a plain-HTTP listener on a loopback address. Plain HTTP is accepted only
   for loopback hosts. Your listener then sees exactly what the proxy would
   have forwarded, and can return any response you want to test restoration
   with.
3. **In-process tests.** The tests in `internal/proxy` start a proxy and a
   fake upstream in one process. Adding a test there is the quickest way to
   turn an idea into a reproduction. The fuzz targets in the table above run
   with `go test -run '^$' -fuzz '^NAME$' ./PACKAGE`.

[Build and release](development.md) covers building from source.

## Report a finding

Report privately through
[GitHub vulnerability reporting](https://github.com/rozdolsky33/cui-proxy/security/advisories/new).
Do not open a public issue for a leak or a bypass.

A useful report includes:

- The commit or release you tested.
- Which claim above it breaks.
- The smallest reproduction you have, using made-up values. A failing test
  or a fuzz input is ideal.
- The rule names and policy profile involved, without dictionary contents.
- What an attacker gains, and what they need in order to do it.

## What happens to a finding

- Each confirmed finding is fixed or, if it cannot be fixed, recorded as a
  [known limitation](limitations.md).
- Each fix lands with a regression test or a fuzz seed that fails without
  it.
- When the fixes are released, a summary of the review is published in the
  repository. It names the reviewer only with their permission, and it
  leaves out any detail that would help attack an unpatched installation.
- Publication timing is agreed with the reviewer.

The project pays no bounty at present. It has one maintainer and gives no
guaranteed response time. A private report of a leak is handled ahead of any
other work.
