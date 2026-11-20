# Security policy

cui-proxy is a tool that reduces the chance of sensitive data reaching a cloud
model. It is **not a compliance control** (see the README), so a way around its
protections is a security issue worth reporting.

## Reporting a vulnerability

Please report privately: open the repository's **Security** tab and choose
**Report a vulnerability** (GitHub private vulnerability reporting). Do not open
a public issue for a bypass or leak.

Useful reports include: a request shape or text the detectors miss, a path
where a real value reaches the upstream, logs, the dashboard or an error
message, a way to make restore corrupt a tool call, or a dashboard
cross-origin / DNS-rebinding problem.

**Never include real sensitive data (CUI, customer identifiers, credentials,
real tokens) in a report.** Use made-up values that have the same shape.

## Reviewing the project

If you want to look for problems deliberately, the
[review guide](docs/security-review.md) lists the claims to attack, where the
code is, what is already known, and how to test without real data.

## What happens to a report

[Maintain the project](docs/maintaining.md#handling-a-security-report) lists
the steps. The project has one maintainer and gives no guaranteed response
time. A private report of a leak is handled ahead of any other work.

## Supported versions

Only the latest release is supported.

## Scope notes

- Detection is pattern-based; things the patterns do not cover are listed
  under "Known limitations" ([docs/limitations.md](docs/limitations.md)). A gap that is already listed is
  still welcome as a feature request, but it is documented behavior.
- The credential headers (`Authorization`, `Chatgpt-Account-Id`, cookies) are
  forwarded to the upstream unchanged by design.
