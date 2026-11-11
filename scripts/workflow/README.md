# Deterministic workflow harness

This is the correctness harness for roadmap Task 02 and version 1 of the
[workload contract](../../docs/superpowers/specs/2026-11-10-daily-use-workload-contract.md).
It runs on macOS/Linux with Python 3.9+ and the built Go proxy. It uses only
the Python standard library and never contacts an external model or real
infrastructure.

Build the proxy with `make build`, then run the following through a local
subprocess that captures stdout/stderr, as required by repository `AGENTS.md`:

```sh
python3 scripts/workflow/harness.py --binary bin/cui-proxy --report /private/tmp/cui-workflow-report.json
python3 -m unittest discover -s scripts/workflow
```

On Linux use a private report location under `/tmp` instead. The harness itself
prints only pass/fail and counts; its exit status is zero only when all required
checks pass. Keep unittest output captured too, because assertion failures can
contain fixture values. The JSON report contains metadata, fixed assertion
identifiers and operation outcomes; it does not contain bodies, originals,
image/OCR data, credentials or raw workspace identities.

The runner launches an actual proxy binary, a separate fake upstream, and a
controlled OCR subprocess. Each environment has isolated encrypted vault,
key, audit log and configuration paths. The OCR stand-in consumes the image
and returns deterministic text; PNG validation, extraction invocation, shared
request limits, sanitization and streamed restoration still use production code.
The fake upstream does not receive the original-value oracle. It constructs
responses from the actual received sanitized requests. Client assertions verify
the upstream boundary, decoded response meaning and temporary-file/tool effects.

`manifest.json` declares scenarios and expected outcomes independently of the
proxy. The suite runs at one/four logical clients, including a parent-to-three-
children-to-parent flow. Bypass controls cover supported text/tool scenarios;
they are not interpreted as an alternative image/policy implementation.
Strict mode, explicit blocks, malformed JSON, unsupported image placement and
disabled extraction must refuse without forwarding. Non-SSE successful upstream
responses must be refused; upstream errors are preserved; cancellation must
reach the fake upstream within five seconds.

Boundary fault-injection checks deliberately bypass sanitization, swap restored
values, and forward a request that should be refused. The oracle must catch all
three. Unit regressions also check escaped originals, error/cancellation paths,
JSON value types, and cleanup of descendants whose direct parent has exited.
These checks validate the harness; they do not mutate the production source.

Capture IDs are bounded ASCII tokens (1–128 characters, beginning with a
letter/digit and containing only letters, digits or hyphens). The fake upstream
rejects other IDs before writing captures or cancellation markers. Both output
paths are resolved and checked against the capture directory with a trailing
separator before either write, including rejection of existing symlinks to a
location outside that directory.

Temporary raw fixtures/captures are removed at exit. Child process groups are
terminated even when their leader has already exited, with a twelve-second
grace period before forced cleanup. HTTP reads have a fifteen-second inactivity
timeout, a forty-five-second overall deadline checked between reads, and a
1 MiB response cap for these compact correctness fixtures. This means an
in-progress read may extend the overall deadline by up to the inactivity timeout.

## What a passing report does not establish

- Normal-tier performance: these compact fixtures use two regex rules and a
  growing small vault, not the 50-rule/10,000-mapping benchmark population.
- Real OCR quality, extraction fairness, throughput or resource budgets.
- W09 compaction/reconnect and W10 restart qualification, which belong to later tasks.
- Arbitrary clients, tools or future protocol representations.

Durations are diagnostic only. A separate two-request probe inserts fixed 80 ms
fake-upstream and OCR delays, verifies stage attribution, extraction exclusion,
request-size counts and gauge cleanup, and records bounded stage summaries in
`stage_timings`. It also checks correlation is disabled by default. CPU,
allocation and heap samples remain explicitly unavailable until Task 04.
The probe requests are reported separately from the main workflow counts.
Do not use the mixed-scenario duration quantiles as a release performance claim.
Changes to protocol support or the workload contract must update the manifest
and corresponding semantic assertions together.

## Performance report primitives (Task 04 in progress)

`performance_stats.summarize_pairs` matches unique seeded `operation_id` values
before subtracting proxy/bypass `first_byte_ms`. Quantiles use nearest rank;
negative added delays remain as scheduling jitter. Missing, duplicate, empty
or non-finite pairs invalidate the comparison instead of dropping observations.

Outcome records contain `operation_id`, `outcome` (contract section 8),
`supported` (fixed by the manifest), `completed` (full HTTP response received),
and finite nonnegative `duration_ms`. A timeout still contributes one attempted
supported operation with its observed duration and an attributed terminal outcome.
Only `supported_success` enters the success numerator. Expected negative cases
are separate; unsupported failures are still failures. Empty denominators yield
no rate. These primitives alone do not constitute a performance baseline.
