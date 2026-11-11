# Resumable performance campaign runner

`performance_campaign.py` executes the existing isolated measurement runners sequentially and maintains a private atomic manifest. It covers normal text timing, throughput, image/PDF extraction, mixed text/image fairness and overload diagnostics. It does not run external model calls or modify the live proxy.

## Running and resuming

Provide optimized proxy and seed binaries plus an existing validated fixture directory:

```sh
python3 scripts/workflow/performance_campaign.py \
  --binary "$PROXY_BINARY" --seed-binary "$SEED_BINARY" \
  --fixtures "$FIXTURE_DIR" --output "$CAMPAIGN_DIR" --profile smoke
```

The output directory must be new. Add `--resume` with the same arguments to resume an interrupted campaign. The manifest verifies the job plan, source commit and script hashes, Python version, executable hashes and fixture manifest hash. It also verifies retained report hashes before starting work. Changed inputs require a new campaign directory. Completed verified jobs are skipped; unfinished or failed jobs get a new attempt report, preserving earlier evidence. An exclusive lock prevents overlapping use of one campaign directory.

The smoke profile runs six short jobs across all supported sections. The reference profile uses the workload contract's sample counts, durations and repetitions. `--sections text throughput extraction fairness overload` selects a subset. Text jobs prioritize four clients, then one, eight and sixteen. They allocate `ceil(1000 / clients)` requests per client, reaching the required aggregate count without multiplying 1,000 by the client count. Every repetition is independently checkpointed, and CLI repetition offsets preserve alternate proxy/bypass ordering after resume.

Reference runs require a clean checkout and the declared Mac hardware/OS, patched Go toolchain, optimized build, AC power and Low Power Mode off. Known competing benchmark, build and extraction processes are checked before and after each job. Environment observations are retained with the attempt. Missing or changed conditions mark the job failed even if its child command completed; that report remains available. These boundary checks do not prove the absence of arbitrary host activity throughout a long interval.

Child output is captured into private logs. Interruptions request cleanup of only the owned child process group, escalating based on live group members rather than leader status. A normal leader exit also waits for a bounded group drain; persistent descendants are terminated and fail the job. The campaign stops at the first failed job and leaves later jobs pending. It never overwrites previous attempt reports. Inspect metadata/status/counts in assistant output; do not paste raw fixture reports or logs into an assistant session.

## Fairness runner

`fairness_performance.py` runs one text client alongside three image clients, validates fixture provenance, alternates arms by repetition, and compares only the matched text records. It retains image outcomes and extraction stages separately in each phase. Existing output reports are refused and partial reports are saved atomically. `--repetition-offset` is also available in the text and extraction CLIs; overload accepts `--repetition` to preserve its arm order and report identity.

## Scope and verification

Portable tests exercise aggregate sample counts, native client partitioning, repetition ordering with the real text comparator, private child logs, owned-process cleanup, exclusive locking, input/report tampering, preserved failed attempts, and resume without repeating completed work. The final workflow discovery run completed 127 tests successfully (123 passed, four native opt-in skips). The complete six-job native smoke campaign passed, followed by a successful resume with one attempt per job and no repeated work. Fresh review identified one process-group cleanup defect; real subprocess regressions reproduced both interrupted and successful leaders leaving descendants, then passed after the fix. The final suite and native smoke passed after that correction.

All outputs remain explicitly unqualified. Stress/churn, component profiling, complete acceptance evaluation and consolidated bottleneck evidence remain required for Task 04. Full soak qualification belongs to its separate roadmap task. Campaign completion means the selected measurements were collected, not that their numerical gates passed.

## Native process-probe correction

On macOS, `ps` truncated the executable column when it preceded arguments, making Chrome paths appear to end in `Go`. Reference preflight consequently reported unrelated browser processes as competing Go work. The probe now reads full-width command and argument snapshots separately, preserves paths containing spaces, and ignores zombie states. Regression tests retain positive detection of live Go, OCR and benchmark processes. The corrected native host preflight passed with zero competing jobs; unrelated applications were left running.

The process-probe correction completed 129 workflow tests successfully (125 passed, four native opt-in skips), passed native host preflight, and received a clean fresh review.
