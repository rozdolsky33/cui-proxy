# Roadmap to 1.0

Status as of 2026-11-19, release v0.7.0.

cui-proxy is an early-stage, single-maintainer project. Its tests, fuzzing and
CI are extensive for its age, but it was first committed on 2026-11-06, has
one human author, and has no recorded use by anyone outside the project. This
roadmap lists what has to happen before a 1.0 release is justified.

Most of the remaining work is not new features. It is outside scrutiny, real
use, and time. A sanitizing proxy earns trust when people who did not write it
fail to get a sensitive value through it, and that cannot be produced by
adding commits.

This document is a plan. It does not change any release claim. The gates and
evidence rules in [release readiness](docs/release-readiness.md) stay
authoritative, and the current boundaries are in
[limitations](docs/limitations.md) and [assurance](docs/assurance.md).

## Ground rules

- Fail-closed behavior and the existing quality and security gates are
  preserved throughout. No milestone is met by waiving a failed check.
- A milestone is met by its exit criteria, not by the passage of time or by
  the amount of work done toward it.
- Failed, interrupted and rejected runs are kept and reported alongside
  passing ones, as release readiness already requires.
- Reports and evidence use fabricated values only. Suspected bypasses go
  through private vulnerability reporting, as described in
  [SECURITY.md](SECURITY.md).

## Milestones

Milestones are listed in priority order. Milestones 1 to 5 can run in
parallel; 6 and 7 depend on elapsed time and on other people; 8 waits for the
rest.

### 1. Close the open v0.7.0 qualification gates

These are already recorded as open in release readiness and the
[text-only release notes](docs/early-trial.md).

- Four-client mixed-load performance: the corrected build measured 151.50 ms
  p95 added first-byte latency against the 150 ms target.
- The sustained four-hour fixed and churn soak has not passed.
- Live-model task quality with substitutions in place is unmeasured.

Exit criteria: each gate passes under the matched-run protocol in the
reference workload contract, or the supported claim is narrowed in the
documentation to what the evidence shows. The target is not moved to fit a
result.

Progress: the proxy now flushes event-stream headers before the first
restored event, which removed a first-byte delay found while investigating
the failed gate. See the
[diagnostic summary](docs/verification-results/2026-11-19-sse-header-flush.md).
All three gates remain open until the reference-machine campaign and soak are
rerun and the live run is budgeted.

### 2. Stabilize the interfaces

Fourteen tags were published in the first thirteen days. Nothing has been
stable long enough for an operator to rely on it.

- Declare which interfaces are stable: configuration schema, CLI commands and
  flags, vault and key format, decision-log format.
- Write a compatibility and upgrade policy: what a minor release may change,
  how deprecations are announced, and which older vault formats a release
  reads.
- Move to a slower release cadence, with fix-only releases between feature
  releases.

Exit criteria: the policy is published, and two consecutive releases ship
without a breaking change to a declared-stable interface.

Progress: the [compatibility and upgrade policy](docs/compatibility.md) is
published and tests lock the declared interfaces. The two-release count
starts with the first release after v0.7.0.

### 3. Trial with outside users

- Recruit operators who did not write the project to run it in daily work for
  several weeks. A starting target is five to ten.
- Collect what tests do not show: false positives that push people to disable
  rules, latency they notice, unusual configurations, install and upgrade
  failures.
- Track every report as an issue and record how it was resolved.

Exit criteria: at least five outside operators have each used the proxy for
four weeks or more, and their reports are triaged with no open report of a
sensitive value reaching the upstream.

Progress: the [trial guide](docs/trial.md) and issue forms for enrollment,
false positives, bugs and install problems are in place. No outside operator
has enrolled yet. Enrollment issues carry the `trial` label, and each one
counts toward the criterion once its operator has reported for four weeks.

### 4. Independent adversarial review

Earlier review findings are recorded under
[findings already addressed](docs/assurance.md#review-findings-addressed).
This milestone is about review by people who had no part in writing the code.

- Commission or invite a review by someone with no part in writing the code.
  A paid audit is the strong form; invited security-minded peers working
  against a written scope is the lower-cost form.
- Scope it to the trust boundary: detection bypass through encodings and
  escapes, values split across stream chunks, tool-call and attachment paths,
  restoration corrupting a tool call, vault and dashboard exposure.
- Add each confirmed finding as a regression test or fuzz seed.

Exit criteria: one external review is complete, its findings are fixed or
documented as limitations, and a summary is published without sensitive
detail.

Progress: the [review guide](docs/security-review.md) gives a reviewer the
claims to attack, a map of the code, the known limitations, safe test setups
and the reporting route. No external reviewer has been engaged yet.

### 5. Measure detection quality

Unit tests show that the rules do what was intended. They do not show how
much real sensitive data the rules catch.

- Build or obtain an evaluation corpus that the rules were not written
  against, using fabricated values with realistic shapes.
- Report false-negative and false-positive rates per rule family, and repeat
  the measurement when detection changes.
- State plainly which data classes are out of scope for pattern and
  dictionary detection.

Exit criteria: the rates are published with the corpus description and
method, and the documentation states detection coverage in those terms.

Progress: [measured detection quality](docs/detection-quality.md) publishes
false-negative and false-positive rates for the built-in families on a
generated corpus, with the method, and a test fails when a detection change
moves any count. The first run found two shapes that were not fully masked,
and both are fixed. The corpus was written inside the project. A corpus from
outside it, or shapes drawn from de-identified real traffic, is still needed
before this milestone is met.

### 6. Show resilience to upstream change

The proxy depends on the Codex wire protocol, which this project does not
control.

- Keep the [protocol matrix](docs/protocol-support.md) current against new
  Codex releases, and record the tested client version with each release.
- When the upstream protocol changes, confirm that unrecognized shapes are
  refused, not forwarded uninspected, and record how long the fix took.

Exit criteria: at least one upstream protocol change has been handled with
fail-closed behavior intact and a fixed release published, and the handling
is written up.

Progress: `make client-probe` runs an installed Codex client against a local
stand-in for the model service and compares its requests with a recorded
baseline, and a weekly workflow runs it against the newest client.
[Codex client changes](docs/upstream-changes.md) holds the procedure, the
checked versions and the first write-up: Codex 0.166.0 moved its tool
definitions, the proxy stayed fail-closed, and the adapter was fixed the day
the change was noticed. No release contains that fix yet, and the probe does
not see real responses, so this milestone is not met.

### 7. Reduce single-maintainer risk

- Bring in at least one more person with enough context to review changes to
  detection, transformation and streaming.
- Require review by someone other than the author for changes to those paths.
- Document the release and security-response process so that it does not
  depend on one person.

Exit criteria: a second maintainer or regular outside reviewer is active, and
security-sensitive changes are merged with a review from someone other than
their author.

Progress: [Maintain the project](docs/maintaining.md) lists what depends on
one person, the sensitive paths, what a review of them checks, the release
and security-report steps, and the path to becoming a reviewer.
`CODEOWNERS`, a contribution guide and a pull request template are in place.
There is still one maintainer and no second reviewer, so the review rule is
written down but not enforced, and this milestone is not met.

### 8. Broaden scope, after the rest

Additional clients, menu-bar parity beyond macOS, and wider attachment
support each enlarge the surface that milestones 3 to 6 must cover. They are
deferred until those milestones are met for the current scope, and each
addition goes through the same review, measurement and trial steps.

## What 1.0 requires

- [ ] The v0.7.0 qualification gates are passed, or the claims are narrowed.
- [ ] A compatibility policy is published and has held across two releases.
- [ ] Five or more outside operators have used the proxy for four weeks each.
- [ ] One external adversarial review is complete and its findings resolved.
- [ ] Detection false-negative and false-positive rates are published.
- [ ] One upstream protocol change has been handled fail-closed.
- [ ] Security-sensitive changes get review from someone other than the author.

## What does not count as progress

- Commit, test or release counts taken alone.
- Benchmarks from a build other than the released binary.
- Review by the author, or by tooling the author directed, presented as
  independent review.
- A passing result reached by changing the target after a failure.

## Timing

A rough estimate is three to six months from this document's date. Most of
that is trial duration and review lead time, not implementation. The estimate
is not a commitment; the checklist above decides when 1.0 is ready.
