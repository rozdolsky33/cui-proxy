# Adding a client or feature

cui-proxy does one thing for one client. This page says what the current scope
is, why it is not being widened yet, and what an addition has to bring when it
is.

## Current scope

| Area | In scope today |
|---|---|
| Client | Codex command-line client, Responses protocol over HTTP with an event-stream response |
| Platform helper | Menu-bar helper on macOS. The proxy and its commands run on macOS and Linux |
| Attachments | Text taken from images and PDFs in user messages, and from images in tool results, when the operator turns extraction on |

The [protocol matrix](protocol-support.md) and
[capabilities and limitations](limitations.md) give the detail.

## Why scope is not growing yet

Each addition is new code that decides what leaves the machine. The
[roadmap](https://github.com/rozdolsky33/cui-proxy/blob/main/ROADMAP.md) puts
four things ahead of any addition, because they show whether the current scope
can be trusted:

| Condition | State on 2026-11-19 |
|---|---|
| Outside operators have used the proxy for four weeks each | Trial is open. Nobody has enrolled |
| An external adversarial review is complete | Review guide is published. No reviewer is engaged |
| Detection rates are measured on a corpus from outside the project | Measured on a corpus written inside the project |
| A client change has been handled and a fixed release published | Done in v0.7.1, for one small change |

Until those are met, a request to widen scope is recorded and not built. A
second client added now would double the code that has had no outside review,
and it would have no trial operators and no measurement of its own.

This is a rule about what the project maintains and releases. The license lets
anyone build an addition in a fork.

## What an addition must bring

Every addition:

- an issue that says who needs it and what it changes about what leaves the
  machine;
- an entry in [capabilities and limitations](limitations.md) for what it does
  not cover;
- review by someone other than its author, as
  [Maintain the project](maintaining.md) requires for sensitive paths;
- a trial with at least one outside operator before it is described as
  supported.

### A new client

A client is added as an adapter in `internal/adapter/`. It must have:

- **Routes.** An explicit list of the routes it handles. Every other route is
  refused.
- **Request scanning.** Every string scanned unless it is at a listed protocol
  position, with a test that walks every string in a representative request.
- **Response restoration.** Restoration that holds for a stand-in value split
  at every point in the stream, and a failed stream when restored tool-call
  arguments are not valid.
- **Fuzz targets** for request sanitizing and response rewriting, run in CI.
- **A protocol matrix** like the [one for Codex](protocol-support.md), with a
  test named for each row.
- **A client probe** like [the one for Codex](upstream-changes.md), with a
  recorded baseline and a scheduled run.
- **A live check** with made-up values against the real service, recorded in
  [verification evidence](verification.md).

A test fails when a directory is added under `internal/adapter/`, so that a
new adapter is a decision and not a side effect.

### A helper on another platform

- The same controls as the macOS helper, or a list of the ones left out.
- No new network listener and no new way to change policy.
- Install, upgrade and removal steps, each tested on a clean machine.
- A named person who uses that platform and will maintain it.

### Wider attachment support

- A statement of what is extracted and what is lost, such as layout or pixels.
- Refusal, not forwarding of the original bytes, when extraction fails or the
  part is in an unexpected place.
- Size and time limits, with tests at each limit.
- A measurement of extraction on made-up documents before detection figures
  are quoted for it.

## Requests recorded so far

None from outside the project. The roadmap names three areas the maintainer
expects to be asked about: more clients, a helper beyond macOS, and wider
attachment support. None is designed or scheduled.

To ask for an addition, open an issue. It will be labelled and kept until the
conditions above are met.
