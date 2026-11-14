# Release readiness and operating limits

A successful build or healthy dashboard does not establish four-agent performance or safe restoration of old histories. Tie every release claim to the tested binary and retained evidence below.

## Current status

The deterministic protocol, attachment policy, restoration, recovery and bounded-admission checks are implemented. Four-client mixed-load performance and the sustained four-hour workload remain open. The latest vault memory fix and workload interruption fix are integrated in [PR 48](https://github.com/rozdolsky33/cui-proxy/pull/48); their diagnostics do not close those acceptance gates.

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

Run full qualification on demand and before a release that changes detection, transformation, streaming, vault lifetime/persistence, extraction, admission or their configuration. Repeat affected measurements after fixes; retain rejected and interrupted runs. Schedule a longer soak on the controlled reference machine when that machine is available, not as an unattended hosted-runner job with unbounded cost. A recurring calendar schedule has not been installed.

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
