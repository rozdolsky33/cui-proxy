# Task 12 release evidence and recovery guidance

The daily-use workload contract remains the authority. This change makes release readiness inspectable without declaring the still-open Task 08/11 measurements complete.

## Approach

Use existing deterministic correctness checks and Go microbenchmarks. Add a captured-output, report-only base/candidate comparison after correctness gates; shared-runner timing must never become a flaky numeric merge gate. A separate controlled-machine acceptance record owns full latency, resource, fairness, and soak decisions. Do not schedule long paid hosted-runner campaigns or weaken existing safety gates.

Considered alternatives: a hard shared-runner latency threshold would confuse machine variance with regressions; running the complete reference campaign on every PR would take hours and use the wrong hardware. Retain short diagnostic evidence in CI and full acceptance on the reference workstation.

## Required behavior

- Unsupported vault-format errors preserve the existing vault and key, explain use of a compatible version, and forbid resuming histories after intentionally abandoning mappings. Startup still fails closed.
- Short benchmark comparisons use the same named benchmarks, equal iteration counts, alternating order and multiple samples. Preserve both latency and allocation changes, including regressions. Source revisions and build identities are attached. Raw benchmark output remains captured; publish numeric aggregates and test names only.
- Deterministic tests must pass before optional benchmark work. Failure cancels later jobs; diagnostic timing never overrides a failed correctness/security gate.
- Document on-demand and pre-release long qualification, precise version/source/configuration identity, protected local detailed artifacts, safe published summaries, and retention of rejected runs.
- Publish an honest compatibility/support matrix and operating envelope: unmeasured or failing cells stay open. Record upgrade/rollback verification using the same vault/key and historical restoration oracles. A health check cannot establish historical correctness.
- Reassess roadmap ratings only from available evidence. Do not publish a four-agent acceptance claim before Task 08 and Task 11 pass.

## Boundaries

No new external service, live model spend, credentials, production policy changes, vault eviction, or installation modifications. Existing CI remains the correctness gate. Optional live-model qualification still requires its separately specified spend cap.
