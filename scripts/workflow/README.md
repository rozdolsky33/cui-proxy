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

## Concurrent text measurements

Build the optimized proxy and the isolated vault helper before measuring:

```sh
go build -o bin/cui-proxy ./cmd/cui-proxy
go build -o bin/cui-seedvault ./scripts/workflow/seedvault
```

Run these commands through captured subprocesses; reports, fixtures, profiles
and assertion output remain private local artifacts. Never use a running user
proxy as the target. The runner creates its own loopback services and temporary
file vault, key, dictionary, configuration and audit log. The fake upstream is
a separate process and receives neither the original-value oracle nor vault.

```sh
python3 scripts/workflow/performance.py --binary bin/cui-proxy --seed-binary bin/cui-seedvault --clients 4 --count 3 --report /private/tmp/cui-text-smoke.json
python3 scripts/workflow/performance_matrix.py --binary bin/cui-proxy --seed-binary bin/cui-seedvault --directory /private/tmp/cui-text-matrix --mode smoke
```

The matrix covers 256 KiB, 768 KiB and 1 MiB at 1/4/8/16 clients with three
repetitions and alternating bypass/proxy order. `--mode text-qualification`
selects the contract's 60-second/100-success warm-up and at least 1,000 measured
operations per size/concurrency/repetition. That text matrix alone requires
roughly twelve hours at the prescribed backend pacing. Its name describes the
sample schedule, **not** a qualification result. This incremental runner always
reports `performance_qualified: false`; complete qualification is not implemented.

Each generated request has 40 protected occurrences (20 distinct values), 40
near misses, and ordinary prose spread across message and tool-result leaves.
Normal fixtures use builtin detectors, 50 regex declarations, 1,000 dictionary
entries and 10,000 prepopulated mappings. `--tier small|large|rules|vault` selects
the contract's independent size/rule/vault stress axes; `--size` selects a size
in that tier. The seed helper records actual compiled-detector and vault counts.

Both modes receive the same one-second backend wait and 32 deltas spaced at
20 milliseconds, including two values split across events. A private client
oracle checks the exact upstream body digest and the complete restored text.
The upstream digest is compared in memory and never included in reports.
First byte is the first socket read containing response bytes, including headers.
Logical-prefix timings record availability of 32 corresponding decoded text
prefixes, independently of wire chunk boundaries. Missing pairs invalidate the
comparison; no aggregate percentiles are subtracted.

`--duration 600` selects a fixed-size, ten-minute closed-loop throughput window.
Only successful completions inside the window count toward throughput; requests
started in the window are drained and retain their outcomes. This is a diagnostic
fixed-size window, not the contract's equally weighted mixed-size throughput
gate. Unequal sample sets do not yield paired latency claims. Client concurrency
is measured from overlapping request intervals; proxy activity is also sampled
from its own gauges. Fixture construction occurs before each request timer.

Resources are sampled once per second: proxy RSS and the full live descendant
RSS tree, plus the existing stage metrics and fixed Go runtime counters.
`runtime.heap_objects_bytes` measures heap objects currently allocated;
`runtime.live_heap_bytes` is the live heap at the most recent GC, identified by
`runtime.gc_cycles`. `runtime.goroutines` counts the whole Go process. Reading
these fields does not force GC or expose a debug endpoint. Stage snapshots are
bounded rolling observations and include setup/warm-up; they are attribution
evidence, not independent per-request paired latency samples. Missing resource
measurements remain unavailable. Reports record source/executable identities,
dirty state, runner/generator hashes, hardware and power/load observations.

### Component profiles

Generate a fresh private fixture directory, then profile real production
components using that same population:

```sh
python3 scripts/workflow/performance.py --binary bin/cui-proxy --seed-binary bin/cui-seedvault --prepare /private/tmp/cui-bench-fixture
CUI_BENCH_FIXTURE_DIR=/private/tmp/cui-bench-fixture go test ./internal/benchmark -run '^$' -bench . -benchtime=3x -cpuprofile /private/tmp/cui-cpu.pprof -memprofile /private/tmp/cui-heap.pprof -mutexprofile /private/tmp/cui-mutex.pprof -blockprofile /private/tmp/cui-block.pprof -o /private/tmp/cui-bench.test
```

Benchmarks cover detection, full request walking, warm/cold restoration indexes,
file-vault saves and mapping-triggered index rebuilds. They reject unexpected
match counts or unseeded values before timing. Repeat with separate fixture
directories for each payload/rule/vault tier. The three-iteration example is a
profile smoke test, not a stable performance estimate. Go profiles are local
diagnostics; production debug endpoints remain disabled.

Task 04 still requires real OCR/PDF and fairness fixtures, open-loop overload,
end-to-end churn, qualified resource/environment checks and sustained sample
windows. A successful diagnostic run verifies its supported text operations;
it does not establish the four-agent release milestone.
