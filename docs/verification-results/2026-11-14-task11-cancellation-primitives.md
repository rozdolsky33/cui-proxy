# Task 11 cancellation primitives: native verification

This verifies the diagnostic building blocks. It does not qualify queued or active extraction cancellation against the proxy, and does not qualify overall performance or the full soak.

## Scope and source

The tested worktree was based on `bd051f3`, with the two native test files added in this change. The optimized reference benchmark was paused between pairs, and its owned services were confirmed absent before native execution. The installed user proxy was not changed.

The gate tests execute a private Python delegate that reports input length/hash and process identity. They verify immediate version delegation, unarmed and gated binary input preservation, delegation by exec with the same PID, rejection above the 80 MiB input cap before admission, release after a context error, and no delegation after a held launcher is killed. This proves launcher mechanics; it is not a real OCR correctness test.

The transport test uses a private loopback server that receives a request but withholds headers. Shutting down the tracked socket wakes the request worker; worker-side close records transport completion, and the HTTP connection factory is restored. This does not measure a production extractor's cancellation latency.

## Results

- First native run: one test expectation failed because `arm()` returns the generation directory and the marker records its basename. The captured failure was retained, and the assertion was corrected to compare the basename.
- Combined cancellation checks: 42 tests passed, including seven native tests.
- Full workflow suite: `python3 -m unittest discover -s scripts/workflow` passed 761 tests in 45.64 seconds, with no failures.
- Raw subprocess and fixture output was captured privately. No fixture contents or request values were printed.

Private evidence is retained under `/private/tmp/cui-cancellation-native-first.log`, `/private/tmp/cui-cancellation-native-green.log`, and `/private/tmp/cui-cancellation-workflow-first.log`. These local paths are not portable release artifacts.

## Remaining qualification

Complete the three-trial native cancellation runner and complete artifact evaluator. Run the original four-client warmup of at least 60 seconds and 100 successful operations, then prove queued cancellation within one second and active extractor-slot release within two seconds. Each trial must bind helper identities, actual interrupted transports, audit attribution, final upstream absence, semantic recovery, privacy and owned-process cleanup. Required review and CI remain outstanding for this branch.

The separate reference fairness queue has now passed three 768 KiB pairs and its first 1 MiB pair on the frozen optimized runtime. The latter passed all ten per-pair checks, with 1,000 paired text samples, added first-byte p95 146.918 ms and p99 170.408 ms, and relay p99 59.138 ms over 32,000 samples. This is partial campaign evidence; additional repetitions, sizes and the other original qualification campaigns remain required.
