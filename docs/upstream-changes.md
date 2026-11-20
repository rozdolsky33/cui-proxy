# Codex client changes

cui-proxy sits between Codex and its model service and depends on the format
of the requests and responses that pass through it. This project does not
control that format. A new Codex release can change it without notice.

This page says what the proxy does when the format changes, how a change is
noticed, which client versions have been checked, and how a change is handled.

## What happens when the client changes

| Change in the client | What the proxy does | Effect for you |
|---|---|---|
| A new text field anywhere in a request | Scans it like any other text | None |
| A new route or method | Refuses it | That client feature fails |
| A request the adapter cannot parse, or one that is compressed | Refuses it | Requests fail until a fix is released |
| A field that looks like an attachment in a place the adapter does not expect | Refuses the request | Requests fail until a fix is released |
| A new kind of response event | Passes it through without restoring values | A stand-in value can show up in place of the original |
| A response that is not an event stream | Blocks it | Requests fail until a fix is released |

Each row is tested in the [protocol matrix](protocol-support.md). In every case where the adapter does not recognise a request, the request is
refused, not forwarded uninspected. The cost of a client change is failed
requests or unrestored values, not original values reaching the model service.

One limit applies. The proxy cannot know that a new field carries encoded or
binary content if nothing in its name or type says so. Such a field is scanned
as text, and the detectors do not look inside encodings.

## How a change is noticed

`make client-probe` runs the Codex client installed on the machine against a
local stand-in for the model service. It contacts no model service, needs no
account and costs nothing. The client gets an empty home directory, so your
login, configuration and tool servers are not used.

The probe runs the client twice with the same made-up prompt: once pointed at
the stand-in, and once through `bin/cui-proxy`. It checks that:

- the client finished both runs, including one tool call and its result;
- the proxy forwarded every route the client used;
- the forwarded request bodies had the same structure as the direct ones;
- the made-up value in the prompt never reached the stand-in through the proxy;
- the client still printed the original value at the end.

It also records what the client sent as a list of routes, header names, JSON
key paths and `type` and `role` values, with no content. It compares that list
with the
[recorded baseline](https://github.com/rozdolsky33/cui-proxy/blob/main/docs/verification-results/client-shapes/baseline.json)
and fails if the client sent anything the baseline lacks.

A [scheduled workflow](https://github.com/rozdolsky33/cui-proxy/blob/main/.github/workflows/client-drift.yml)
runs the same probe every Monday against the newest published client. GitHub
stops scheduled workflows in a repository with no activity for 60 days.

### What the probe does not cover

- **Real responses.** The stand-in sends a small fixed event stream. A new
  event type from the real service is not seen until a
  [live check](verification.md) is run.
- **Signed-in routes.** The probe uses a key-based provider. Requests that only
  a ChatGPT login makes are not exercised.
- **Most request types.** One text turn and one tool call are exercised.
  Attachments, long-session compaction, tool servers and delegated agents are
  not.
- **Other hosts.** Only the command-line client is run, on the machine or
  runner that runs the probe.

A passing probe means the checked paths still work. It does not mean the
release is compatible in full.

## Checked client versions

| Client | Date | Proxy | Result |
|---|---|---|---|
| `codex-cli 0.167.0` | 2026-11-07 | `main` at that date | [Live check](verification.md) against the signed-in service passed: text and one tool call. |
| `codex-cli 0.166.0` | 2026-11-19 | v0.7.0 | Probe passed. Found the tool-definition change described below. |
| `codex-cli 0.170.0` | 2026-11-19 | v0.7.1 | Probe passed. Same request structure as 0.166.0. |
| `codex-cli 0.171.0` | 2026-11-19 | v0.7.1 | Probe passed on a Linux runner. One new request header, described below. |
| `codex-cli 0.170.0` | 2026-11-20 | v0.7.2 | Probe passed. No change from the baseline except the header that 0.171.0 added. |

Each release from now on records the client version the probe passed with in
its release notes.

## Handling a change

1. **Confirm the refusal.** Run the probe and read which check failed. If the
   client's requests fail through the proxy, find the refusal with
   [`cui-proxy explain`](block-diagnostics.md). If the proxy forwarded
   something it should have refused, stop and report it privately as described
   in the [security policy](https://github.com/rozdolsky33/cui-proxy/blob/main/SECURITY.md).
2. **Write a failing test.** Reproduce the new structure in an adapter test
   with made-up values. Use key paths from the probe, not captured content.
3. **Fix the adapter.** Accept the new structure only where the client uses it.
   Add tests for nearby structures that must still be refused.
4. **Run the probe and the full test suite**, then update the baseline with
   `go run ./scripts/clientprobe -write docs/verification-results/client-shapes/baseline.json`.
5. **Update the [protocol matrix](protocol-support.md)** and the table above.
6. **Release the fix** and add an entry to the record below, with the date the
   client changed if known, the date it was noticed and the date of the fixed
   release.

## Record of changes handled

### Tool definitions moved into the input list (2026-11-19)

- **What changed.** Codex 0.166.0 sends its tool definitions as an input item
  of type `additional_tools` that holds namespaces. The adapter was written
  for a top-level `tools` list.
- **How it was noticed.** The first run of the probe recorded the new key
  paths. The release in which the client made the change is not known. Nothing
  was checking before this run.
- **What the proxy did before the fix.** It scanned every string in the new
  item, so nothing went out uninspected. It did not recognise the item as tool
  definitions. A tool whose parameter list had a property named like an
  attachment field, such as `image_url`, caused the whole request to be
  refused. The client's own tools have no such property, so ordinary sessions
  worked.
- **The fix.** The adapter treats tool definitions in an `additional_tools`
  item the same way as those in top-level `tools`
  ([pull request 68](https://github.com/rozdolsky33/cui-proxy/pull/68)).
  Attachment fields outside a tool's parameter list are still refused.
- **Time taken.** Noticed, fixed and released in v0.7.1 on the same day.
- **Not confirmed.** That tool servers' definitions also arrive in this item.
  The probe runs the client without tool servers.

### The client asks for compressed responses (2026-11-19)

- **What changed.** Codex 0.171.0 on Linux sends an `Accept-Encoding` header
  with its model requests. Codex 0.170.0 on macOS did not. Whether the release
  or the platform makes the difference is not known.
- **How it was noticed.** The first run of the scheduled workflow's probe
  failed on the new header name.
- **What the proxy does.** It already replaces this header with `identity`
  before forwarding, so the service answers uncompressed and the response can
  be restored. `TestAcceptEncodingIdentityUpstream` covers this. A compressed
  response would be blocked, not passed through.
- **The fix.** None needed. The baseline now includes the header.
