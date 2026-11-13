# Task 04 supplemental concurrency coverage

## Intent and authority

The roadmap requires text-only, attachment-only and mixed traffic at 1/4/8/16 clients. The detailed workload contract defines reference image/PDF timing at one/four clients and fairness as three image clients plus one text client. Those reference runs are retained. Supplementary capacity runs close the broader concurrency coverage without changing the reference workload or release gates.

This is an implementation choice under the owner's standing authorization, not a new user-approved performance target. All supplemental results remain unqualified and separately identify their runner revision and executable hashes.

## Approach

Extend the existing Python load scheduler and extraction runner with explicit exploratory concurrency settings. Keep the four-client defaults and reference behavior identical. Reusing the existing environment, semantic oracles, stage collector, resource sampler and private reporting avoids a separate harness with different safety semantics. Repeating the entire extraction matrix at eight/sixteen clients is unnecessary for the roadmap's workload-category capacity observation; one existing representative image template exercises the same subprocess boundary. Treating the existing four-client reference as proof of broader coverage was rejected because it would leave the roadmap checkbox unsupported.

## Supplemental matrix

- Attachment-only: existing distinct PNG template 0, 100 measured operations, eight and sixteen clients, three fresh-state repetitions each. Reuse the existing one/four-client template-0 results as the separately sourced reference observations. Keep failures in the 100-operation denominator; no replacement successes.
- Mixed: 1 MiB normal text and existing template-0 images; one, eight and sixteen clients; 120-second measured windows, three repetitions, matched proxy/bypass arms alternating order. At eight/sixteen clients, one text worker and seven/fifteen image workers extend the existing one-text-worker fairness shape. A single sequential client cycles text/image/image/image, so both classes run without pretending there is concurrent fairness at one client.
- Every arm warms for at least 60 seconds and 100 successful operations, uses seed 1102026 and normal policy/state, and records offered/completed counts by class, actual concurrency, timings and resources. Measured windows exclude warm-up and preserve draining outcomes separately.
- These bounded exploratory windows do not satisfy the normal text 1,000-sample or ten-minute throughput gates; existing full reference measurements evaluate those gates. No new numerical threshold is introduced. Unequal fixed-window arm counts remain explicit; incomplete paired samples do not become complete by truncation. Report per-arm latency quantiles separately where a full paired comparison is unavailable.

## Invariants

Preserve fail-closed policies, semantic/boundary assertions, audit durability, seeded vault state, private output, independent proxy/bypass state, failed-operation denominators and cleanup. No raw fixtures or reports are committed. Missing resource/timing observations stay incomplete. Peak RSS limits stay 512 MiB proxy and 1 GiB combined children. Resource failures remain visible even though exploratory performance does not qualify the release.

Do not edit the frozen source or run builds/tests while its current churn measurements are active. Implement and verify in the existing isolated results branch afterward, recording the new runner revision separately. Keep primary main clean.

## Completion evidence

Tests establish preserved four-client scheduling and argument defaults, the single-client operation cycle, actual requested concurrency, cancellation/exception cleanup and class accounting. Native smoke establishes real extraction and boundary checks. Six attachment phases and eighteen mixed arms retain reports and before/after environment snapshots. Summaries include p50/p95/p99 where samples exist, counts, windows, memory and limitations, with no cross-revision pooled qualification claim.
