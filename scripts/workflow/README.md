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

### Real extraction fixtures (reference macOS)

`python3 scripts/workflow/extraction_fixtures.py /private/tmp/cui-extraction-fixtures`
creates a new private directory and validates its contents using installed
Tesseract English data and Poppler. Existing directories are refused. Use
`--count 2` for a short fixture smoke test. Missing tools or a text mismatch
exit unsuccessfully; this command never qualifies proxy performance.

The versioned CoreText recipe renders Menlo Regular at 28 points, white
background, fixed margins/line spacing, and JPEG quality 0.9. Four templates
cover PNG/JPEG at 1024x768 and 1920x1080. Each template has 100 warm-up
indices and 100 disjoint measurement indices, printed as six digits in the
raster. Two PDFs contain the same known English reference sentences on two
pages: one has a text layer and the other has only rendered page images.
The strict word oracle ignores whitespace only.

The private manifest records expected text, indices, dimensions, byte counts,
image/document hashes, font hash, rendering recipe and OS version. Do not
commit or print it. Validation verifies file containment and byte integrity
before invoking extraction tools. Every attachment is limited to 2 MiB.
Fixture verification is separate from end-to-end proxy timing, sanitization,
queue measurements and fairness; those Task 04 gates remain outstanding.

Portable unit tests run in ordinary discovery. On the reference Mac, run
`CUI_REAL_EXTRACTION_TEST=1 python3 -m unittest discover -s scripts/workflow -p test_extraction_fixtures.py`
with stdout/stderr captured locally as required by AGENTS.md. This explicitly
runs actual rendering, OCR and both PDF paths; the opt-in test is skipped on
ordinary CI and must not be described as real-tool coverage there.

### Real proxy extraction measurements

The version-2 fixture manifest adds complete recipe coverage validation, resolved
font identity, Swift/Tesseract/Poppler versions and the English traineddata hash.
Regenerate version-1 fixtures before using this runner; old artifacts remain
valid historical evidence but are not silently upgraded.

Run `scripts/workflow/extraction_performance.py` with `--binary`, `--seed-binary`,
`--fixtures`, `--report`, and `--template` (0–3, `pdf-text`, or `pdf-scan`). It uses
the normal seeded policy and an isolated file vault/proxy/fake upstream, with
`--clients 1` or `--clients 4`. Defaults are 100 measured requests, 60 seconds
and at least 100 warm-up completions, and three independent repetitions.
`--repeated` warms and reuses one image; otherwise measured image indices are
disjoint from warm-up and used once per phase. PDF timing is a separate workload.
For a diagnostic smoke, use `--count 4 --warmup-count 4 --warmup-seconds 0
--repetitions 1`. Capture command output locally as required by AGENTS.md.

The client checks an independent expected transformed request against a digest
from the actual upstream body. Only whitespace in declared text fields is
normalized; image bytes, metadata, structure and changed words cannot disappear
from this check. The extra digest is opt-in and is not computed in normal text
benchmark runs. Existing response/restoration checks still apply.

The runner enables existing process-local session correlation to separate warm-up
from measured extraction stage records. Raw correlation identities are excluded
from reports. Each measured operation must have extraction-run and queue-wait
measurements; missing coverage invalidates the phase and retains its operation
counts. Per-request bytes, terminal outcomes, resource summaries and stage samples
are private report metadata. Collection failures retain completed and failed
operations in the measured denominator.

This runner always reports `performance_qualified: false`. These are real
extraction diagnostics, not the complete fairness, overload, stream or repeated
reference qualification gate. Short runs do not establish tail-latency guarantees.

Report destinations must be new files in an existing writable directory. The
CLI reserves the destination before starting work and atomically replaces it
with each completed report; a failed write preserves prior completed evidence.
Warm-up duration must be finite and nonnegative. If cleanup fails while a
warm-up failure is active, the original phase and its operation evidence remain
available.

The diagnostic stage collector uses the existing 256-entry, 15-minute history.
Phases are capped at 100 measured operations; unusually slow phases may exceed
the retention window and are invalidated rather than reporting incomplete
percentiles. CLI provenance preflight checks tool/data versions; full stored
fixture validation additionally checks the recorded font name.

## Read-only baseline acceptance summary

Use `baseline_acceptance.py CAMPAIGN_DIR --output PRIVATE_SUMMARY_PATH` to evaluate retained evidence without starting load. It verifies report hashes, preserves partial-run status, and writes a new private summary. A successful command means summary generation succeeded; `performance_qualified` remains false. See [the evaluator guide](../../docs/verification-results/2026-11-11-baseline-acceptance-evaluator.md) for numerical gates, integrity checks and remaining evidence.


## Exploratory concurrency supplement

The extraction CLI accepts `--clients 1|4|8|16`. Its measured `--count` is an exact total across clients, including when the total is not divisible by concurrency. Balanced worker quotas preserve distinct fixture indices and keep warm-up separate.

The fairness CLI accepts the same client counts and either `--count` or `--duration`, never both. Omitting both keeps the existing default of 1,000 text operations; the default remains four clients. At four/eight/sixteen clients, one worker sends text and the others send images. At one client, a single worker cycles text/image/image/image sequentially; this is mixed-traffic capacity, not concurrent fairness.

Duration mode retains each arm's complete records, configured window, drain outcomes and per-class counts. Unequal arm sample sets remain an incomplete paired comparison; they are not truncated to manufacture matching samples. Settings use the metadata-only report schema. All supplemental reports remain explicitly unqualified. Short capacity windows do not replace the contract's normal text sample counts, ten-minute throughput windows, warm-up requirements or four-client acceptance gates.
