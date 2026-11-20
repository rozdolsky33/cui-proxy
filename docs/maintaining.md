# Maintain the project

cui-proxy has one maintainer. This page says what depends on that one person,
how sensitive changes are meant to be reviewed, how a release and a security
report are handled, and how someone else can take on part of the work.

It is written so that a second person could follow it without asking the
first.

## Current state

| Question | Answer today |
|---|---|
| Who can merge and release? | One person, the repository owner |
| Who receives private security reports? | The same person |
| Are sensitive changes reviewed by someone other than their author? | No. There is no second reviewer yet |
| Is that review enforced by the repository? | No. The rule is written down, and [turned on](#turning-enforcement-on) when a second reviewer exists |
| Are release archives signed? | No. Each release has a `checksums.txt` file only |

Much of the code is written with an AI coding assistant working under the
maintainer's account. That work counts as the maintainer's own. A review by
the same assistant, or by the same account, is not an independent review.

## What depends on one person

| Thing | Where it lives | If that person is unavailable |
|---|---|---|
| Merging and tagging | Admin rights on `rozdolsky33/cui-proxy` | No fix or release can be published from this repository |
| Private security reports | GitHub private vulnerability reporting, visible to repository admins | Reports are not read |
| Homebrew formula | `rozdolsky33/homebrew-tap` and the `TAP_GITHUB_TOKEN` repository secret | `brew upgrade` stops offering new versions |
| Published manual | GitHub Pages on the same repository | The site stays at its last version |
| Performance reference machine | One physical machine named in the [workload contract](https://github.com/rozdolsky33/cui-proxy/blob/main/docs/superpowers/specs/2026-11-10-daily-use-workload-contract.md) | Qualification runs cannot be repeated |

The source is under the license in the repository, so anyone can fork it and
release from the fork. Nothing above can be recovered by someone else from
inside this repository.

Installed copies keep working if the project stops. The proxy does not call
home, and it needs no service run by the project.

## Sensitive paths

A mistake in these packages can let an original value reach the model service,
a log or the dashboard, or can corrupt a restored tool call.

| Path | What it decides |
|---|---|
| `internal/adapter/` | Which parts of a request are scanned, which are passed unchanged, and how response events are restored |
| `internal/detect/` | What counts as a sensitive value |
| `internal/transform/`, `internal/synth/` | How a value is replaced and put back |
| `internal/sse/` | How a response stream is split into events |
| `internal/proxy/` | Which routes, headers and responses are allowed |
| `internal/vault/` | Where the mapping between original and stand-in values is kept |
| `internal/attachment/` | How text is taken from images and PDFs |
| `internal/decisionlog/`, `internal/audit/` | What is recorded, and that it holds no values |
| `internal/ui/` | What the dashboard shows and who can reach it |
| `.github/`, packaging and install scripts, `SECURITY.md` | What is built, published and installed, and these rules themselves |

The same list is in
[CODEOWNERS](https://github.com/rozdolsky33/cui-proxy/blob/main/.github/CODEOWNERS).
Change both together.

## The review rule

A change to a sensitive path is merged only after a review by someone other
than its author.

Until a second reviewer exists, this rule cannot be met. In that period:

- the pull request says that no second reviewer was available;
- the description states whether anything refused before is now accepted;
- the change waits for all checks, with none waived;
- the change is listed for the next outside reviewer to look at after the
  fact.

### Changes merged without independent review

Changes to sensitive paths since this rule was written:

| Date | Pull request | What it changed |
|---|---|---|
| 2026-11-19 | [62](https://github.com/rozdolsky33/cui-proxy/pull/62) | `internal/proxy`: response headers are sent before the first event |
| 2026-11-19 | [67](https://github.com/rozdolsky33/cui-proxy/pull/67) | `internal/detect`: two address shapes that were not fully masked |
| 2026-11-19 | [68](https://github.com/rozdolsky33/cui-proxy/pull/68) | `internal/adapter`: tool definitions accepted in a second place in the request |
| 2026-11-19 | [73](https://github.com/rozdolsky33/cui-proxy/pull/73) | `internal/detect`: ordinary words after `Bearer` are no longer masked, and neither is a word-shaped token outside an `Authorization` header |
| 2026-11-19 | [74](https://github.com/rozdolsky33/cui-proxy/pull/74) | `internal/detect`: short card-like slices inside a longer digit run are masked only with a card prefix for their length |
| 2026-11-20 | [76](https://github.com/rozdolsky33/cui-proxy/pull/76) | `internal/detect`: the same limit for 16 and 17 digit slices |

Everything merged before this table was started is also unreviewed by anyone
outside the project. The [security review guide](security-review.md) covers
that code as a whole.

### Turning enforcement on

When a second person has write access, add a ruleset for `main` in the
repository settings with:

- pull request required before merging;
- one approval required;
- review from code owners required;
- approval of the most recent push required;
- the `test` and `build` checks required.

Then add that person to the sensitive lines in `CODEOWNERS`. GitHub does not
count an author's approval of their own pull request, so with these settings a
sensitive change by either person needs the other.

Do not turn this on with one code owner. Every sensitive change would then
need an administrator override, which teaches the habit of overriding.

## What a review of a sensitive change checks

Read the description first. If it does not say whether something refused is
now accepted, ask.

**For every sensitive change**

- A test fails without the change.
- Inputs next to the new case that must still be refused have tests.
- No original value appears in a log line, an error message, a metric label or
  a dashboard field. Messages name a key or a rule, never a value.
- When something cannot be parsed, restored or classified, the code refuses or
  blocks. It does not fall back to forwarding.
- `make test`, `make lint` and `make fuzz` pass.

**Request handling** (`internal/adapter/`, `internal/transform/`)

- Every string in a request is scanned unless it sits at one of the listed
  protocol positions. A new unscanned position needs a reason, and blocking
  rules must still apply to it.
- A new exemption is tied to an exact position and type, and has tests for the
  same shape in other positions.
- Numbers keep their spelling. A sensitive number that cannot be replaced
  safely is refused.

**Response handling** (`internal/adapter/`, `internal/sse/`)

- A stand-in value split across events is still restored, at every split
  point.
- Restoration state is separate for each output item and does not carry over
  to another request.
- Tool-call arguments are valid JSON after restoration, or the stream fails.

**Detection** (`internal/detect/`)

- Run `go test ./internal/detecteval`. If a count moves, the baseline and
  [the published figures](detection-quality.md) change in the same pull
  request, and the description says which shapes moved and why.
- A wider pattern is checked against the look-alike shapes for new false
  matches. A narrower one is checked for values it no longer catches.

**Proxy, vault, logs and dashboard**

- Unknown routes, compressed requests and unreadable responses are still
  refused.
- The vault file format and key handling follow the
  [compatibility policy](compatibility.md).
- The decision log chain still verifies, and its records hold no values.
- The dashboard stays bound to the local machine and keeps its origin checks.

**Stable interfaces and the client**

- The tests named `TestStable...` pass unchanged, or the change follows the
  deprecation steps in the [compatibility policy](compatibility.md).
- `make client-probe` passes with a current Codex client; see
  [Codex client changes](upstream-changes.md).

The [security review guide](security-review.md) lists the project's claims and
its known limits. A reviewer should read it once before the first review.

## Releasing

[Build and release](development.md) has the commands. Anyone with rights to
push a tag can release. Before the tag:

1. `main` is green on Linux and macOS.
2. `make client-probe` passes, and the client version goes in the release
   notes.
3. The release notes have a compatibility section, as the
   [compatibility policy](compatibility.md) requires.
4. Open gates in [release readiness](release-readiness.md) are stated in the
   notes, not left out.

After the tag:

1. The release has four archives and `checksums.txt`.
2. The Homebrew formula points at the new version.
3. The manual shows the new version.
4. A fresh install on a clean machine starts and passes
   [the setup test](testing.md).

A bad release is not deleted and its tag is not reused. Publish a new patch
release and mark the bad one as such in its notes.

## Handling a security report

Reports arrive through GitHub private vulnerability reporting, as
[the security policy](https://github.com/rozdolsky33/cui-proxy/blob/main/SECURITY.md)
describes.

1. **Acknowledge** the report in its private thread. A report of a leak comes
   before all other work.
2. **Reproduce** it with made-up values. If the report contains real data, ask
   the reporter to remove it and do not copy it anywhere.
3. **Decide** whether it is a defect or a [documented limit](limitations.md).
   Tell the reporter which, and why.
4. **Fix in private.** Use the advisory's private fork, not a public branch.
   Do not describe the problem in a public issue, pull request or commit
   message before a fixed release exists.
5. **Add a regression test** or fuzz seed that fails without the fix.
6. **Release** the fix as a patch release.
7. **Publish the advisory** with the affected versions, the fixed version and
   what an operator should do. Credit the reporter if they agree.
8. **Record** the handling time: reported, confirmed, fixed, released.

A gap found by the project's own measurement is handled the same way: fix
first, publish second.

The project gives no guaranteed response time and pays no bounty. If the
maintainer is unavailable, private reports are not read. That is the main
reason a second person is needed.

## Becoming a reviewer or maintainer

**Reviewer.** Someone who reviews sensitive changes but does not merge.

1. Read [life of a request](architecture.md), the
   [security review guide](security-review.md) and this page.
2. Build the project, run the tests and run [the setup test](testing.md).
3. Review a few pull requests with the checklist above. Review comments on
   merged changes in the [table above](#changes-merged-without-independent-review)
   are a good place to start.
4. After that, the maintainer adds the reviewer to `CODEOWNERS` for the paths
   they have reviewed.

**Maintainer.** Someone who can also merge, release and read private reports.
This needs a period as a reviewer first, because it gives access to
unpublished security reports and to the release path. The maintainer grants
it by adding the person as a repository administrator and as a collaborator
on the Homebrew tap.

To offer, open an issue. Nobody has yet.
