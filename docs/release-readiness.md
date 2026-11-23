# Release readiness and operating limits

A successful build or healthy dashboard does not establish four-agent performance or safe restoration of old histories. Tie every release claim to the tested binary and retained evidence below.

## Current status

The current published patch is v0.7.3; the [supervised text-only guide](early-trial.md) retains attachment blocking in the recommended operator configuration. Publication does not close the outstanding performance or sustained-load gates.

Version 0.7.4 is a frozen, unreleased candidate. Its [candidate evidence](verification-results/2026-11-23-v074-candidate.md) records workflow, real OCR recovery, cancellation and exact-package upgrade/latest-vault rollback checks against its exact executable. Full paired soak and reference campaign acceptance remain pending. Image/PDF support extracts text; it does not preserve model vision semantics.

The deterministic protocol, attachment policy, restoration, recovery and bounded-admission checks are implemented. Four-client mixed-load performance and the sustained workload remain open. The historical decoder optimization in [PR 57](https://github.com/rozdolsky33/cui-proxy/pull/57) measured 151.50 ms p95 added first-byte latency in one corrected-build four-client 1 MiB repetition against the unchanged 150 ms target. Later pacing and streaming fixes in PRs [79](https://github.com/rozdolsky33/cui-proxy/pull/79), [80](https://github.com/rozdolsky33/cui-proxy/pull/80) and [81](https://github.com/rozdolsky33/cui-proxy/pull/81) require fresh qualification. Preserve the historical failure and interrupted runs; measurements of earlier binaries do not qualify the frozen 0.7.4 executable.

| Capability | Evidence and boundary |
| --- | --- |
| Codex Responses JSON requests and SSE responses | [Protocol matrix](protocol-support.md); supported fields and local semantic effects are tested |
| Tool-result and user images | Extracted text is sanitized; original pixels, layout and model vision semantics are not preserved |
| User PDFs | Supported extraction positions and budgets only; tool-result PDFs remain outside the advertised contract |
| Concurrent histories and delegation | Shared mappings and per-stream restoration state; no arbitrary partial-stream replay contract |
| File-vault recovery | [Recovery contract](vault-recovery.md); preserve the same vault and key, including during rollback |
| Saturation and cancellation | [Admission bounds](admission.md); bounded refusal is distinct from normal-workload performance acceptance |
| Four-agent operating envelope | Unqualified pending all normal-tier fairness, resource and sustained-load gates |
| Eight/sixteen clients | Exploratory capacity measurements only; no supported-tier expansion |
| Live model task quality | Unmeasured; reversible substitutions do not prove equivalent model reasoning |

## Evidence required for a release claim

Record the full source revision, clean/dirty state, optimized build flags, Go version, executable SHA-256, platform and architecture. Record fixture/generator versions, controlled workload settings, policy revision identifier, vault cardinality, dependency versions, power state and competing-load checks. Keep configuration contents and raw input/output values private.

Correctness and security gates must pass on the exact candidate head before integration: workflow regressions, Go race tests, vet/staticcheck, vulnerability analysis, fuzz smoke, documentation and packaging. A skipped publication job is not a failed test; a skipped required acceptance measurement is still missing evidence.

Timing on shared CI runners is diagnostic. After correctness and packaging checks, CI runs five alternating base/candidate pairs of `BenchmarkRequestWalk`, ten iterations each, with two Go scheduler threads and the same generated 768 KiB normal-tier fixture. It records executable/source identities, fixture counts and signed median latency/allocation changes. A changed benchmark harness is explicitly unavailable; skipped, malformed, interrupted or unequal-work measurements cannot become a completed comparison. Raw logs remain private to the runner; the numeric summary artifact is retained for seven days. Compare individual samples with equal work and retain allocation increases and latency regressions. Do not turn runner variance into an arbitrary numeric merge gate. The [reference workload contract](https://github.com/rozdolsky33/cui-proxy/blob/main/docs/superpowers/specs/2026-11-10-daily-use-workload-contract.md) owns the actual release thresholds and matched-run protocol.

Full release qualification requires measurements after changes to detection, transformation, streaming, vault lifetime/persistence, extraction, admission or their configuration. The v0.7.0 supervised trial has not met that standard: it retains the failed and missing gates above. Publishing a stable-channel package does not satisfy or waive them. Repeat affected measurements after fixes; retain rejected and interrupted runs. Schedule a longer soak on the controlled reference machine when that machine is available, not as an unattended hosted-runner job with unbounded cost. A recurring calendar schedule has not been installed.

For the four-agent claim, require all three normal payload sizes, three repetitions, matched first-byte and logical-prefix stream timing, throughput, extraction/fairness, resource budgets, cancellations, recovery and the four-hour fixed/churn soak. Any failure or missing measurement leaves the claim open. See the contract for exact denominators and limits; do not substitute pooled percentiles or short diagnostics.

## Upgrade and rollback with active histories

1. Record the running process executable and version, not only the first binary on the shell PATH. Compare its hash to the release artifact. The shell binary and menu-bar-launched binary may differ.
2. Drain cooperating agents and active requests. Verify successful vault persistence and clean shutdown. Preserve a protected, consistent copy of the vault and its key together, plus the effective configuration and required dictionaries.
3. Check the candidate configuration and extraction dependencies in isolation. A successful configuration check is not a complete real-extractor probe; use the documented synthetic verification cases.
4. Start the candidate with the intended existing vault/key. Verify a previously issued substitution restores to the expected original in an isolated historical-continuation oracle. Do not print the history or vault into an assistant session.
5. For rollback, confirm the old binary supports the current vault format and configuration before starting it. Preserve the current vault/key and audit state. An older backup can omit mappings issued since that backup; retire affected histories instead of resuming them blindly.
6. If startup refuses the vault, preserve the evidence and resolve format/key/corruption/writer ownership. Do not delete the vault merely to make startup succeed. Deliberate retirement requires draining all users and abandoning every history that depends on its mappings.

Packaging CI validates archive construction and isolated executable checks. It does not modify the operator's installation or prove every package-manager downgrade. Record installation-specific verification separately.

## Retention and publication

Keep executed fixtures, detailed reports and captured stdout/stderr in protected local storage. Retain failed, interrupted and rejected runs alongside successful runs, with their distinct statuses. Preserve the source/build identities and complete per-operation denominators for as long as the associated release claim is supported; do not overwrite evidence when retrying.

Publish only safe summaries: versions, rule names/counts, test names, aggregate numeric timings/resources, pass/fail states, known limitations and PR references. Do not publish request bodies, extracted text, replacements, vault contents, credentials, raw session identifiers or fixture fingerprints that can identify content. CI diagnostic artifacts must follow the same boundary.

## Assessment

The repository has stronger correctness and recovery evidence than its initial implementation, including explicit protocol limits and fail-closed validation. Everyday four-agent efficiency remains unproven until the pending measurements pass. No revised numeric rating or production-equivalence claim is justified by unit tests alone. Prioritize the first reproducible failed user-visible criterion; do not replace it with unrelated tuning.

## Latest isolated upgrade evidence

[The November 15 packaging and upgrade/rollback check](https://github.com/rozdolsky33/cui-proxy/blob/main/docs/verification-results/2026-11-15-task12-upgrade-rollback.md) passed for macOS arm64, preserving old and newly issued mappings through rollback. It records the unavailable baseline preflight endpoint and missing embedded worktree revision explicitly. Full performance qualification remains open.
