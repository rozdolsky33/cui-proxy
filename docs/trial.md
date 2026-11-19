# Join the operator trial

cui-proxy has so far been used only by its author. The trial asks people who
did not write it to run it in their own daily work for at least four weeks and
to report what goes wrong. The project's
[roadmap](https://github.com/rozdolsky33/cui-proxy/blob/main/ROADMAP.md) needs
five such operators before a 1.0 release.

Taking part costs a few minutes a week. You decide what work goes through the
proxy, and you can stop at any time.

## Before you start

- This is supervised, text-only use. Read the
  [text-only release notes](early-trial.md) and the
  [known limitations](limitations.md) first. Performance under four agents is
  not yet qualified.
- The proxy is an extra local safeguard. It is not a compliance control and
  it does not make it acceptable to send data that your organization
  restricts. Keep your existing approvals and review in place.
- Everything you report is public. Use made-up values, and do not name your
  employer, customers or projects.

## Set up

1. Install the current release. See [install](install.md).
2. In your configuration, set `policy.attachments` and `policy.on_error` to
   `block`. The [required trial policy](early-trial.md#required-trial-policy)
   explains why.
3. Validate the configuration and [test your rules](testing.md) with made-up
   values.
4. Start with one agent on work that is easy to undo. Add more only after a
   few days without surprises.
5. Open a
   [trial enrollment issue](https://github.com/rozdolsky33/cui-proxy/issues/new?template=trial-enrollment.yml).
   The four weeks count from the start date you give there.

## Each week

Add one comment to your enrollment issue. A few lines are enough:

- The version you ran, and whether you upgraded.
- Roughly how many days you used it and with how many agents.
- Blocks or replacements you did not expect, with links to the issues you
  opened for them.
- Delays you noticed.
- Anything you turned off or worked around, and why.

A week with nothing to report is useful too. Say so.

## What to report, and where

| What happened | Where to report it |
| --- | --- |
| A sensitive value reached the upstream, a log, the dashboard or an error message | [Private vulnerability report](https://github.com/rozdolsky33/cui-proxy/security/advisories/new). Never a public issue. |
| A harmless value was replaced, or ordinary work was blocked | [False positive form](https://github.com/rozdolsky33/cui-proxy/issues/new?template=unwanted-match.yml) |
| An error, a crash, a hang or a noticeable delay | [Bug form](https://github.com/rozdolsky33/cui-proxy/issues/new?template=bug.yml) |
| Installing, upgrading, rolling back or starting failed | [Install form](https://github.com/rozdolsky33/cui-proxy/issues/new?template=install-upgrade.yml) |

If the proxy blocks something, do not disable the rule or switch
`policy.on_error` away from `block` to get past it. Report the block. If you
do change a rule, say so in your weekly comment.

## What is safe to include

These carry no request or response content:

- The output of `cui-proxy version`.
- Your operating system, architecture, install method and Codex CLI version.
- The error class and reason from an error message, and its request ID.
- The output of `cui-proxy explain REQUEST-ID`. The
  [decision log](decision-log.md#what-is-never-recorded) records rule names,
  counts and timing, never values.
- A made-up value with the same shape as the real one, once you have
  confirmed with `cui-proxy test-rules` that it reproduces the problem.

Read everything before you post it. Rule names and file paths are recorded
verbatim, so a rule named after a customer or a host would reveal it.

Never include request or response bodies, real matched values, replacement
values, vault or key files, dictionary contents, tokens, or a full
configuration file.

## How reports are handled

- Every report gets the `needs-triage` label when it is opened. Triage
  removes that label and either fixes the problem, records it as a
  [known limitation](limitations.md), or closes it with the reason.
- A private report of a leak is handled first, ahead of any other work.
- The issue records how each report was resolved and in which release.

This is best-effort software with one maintainer. There is no guaranteed
response time.

## Finishing or leaving

After four weeks, add a closing comment to your enrollment issue: whether you
will keep using the proxy, and the one thing that would most change your
answer. You are welcome to carry on after that.

To leave early, comment on the issue and close it. A short note on why is the
most useful thing you can give the project.
