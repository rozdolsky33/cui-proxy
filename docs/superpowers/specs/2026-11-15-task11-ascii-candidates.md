# Task 11: exact ASCII candidate scanners

The complete 1 MiB fairness repetition failed the unchanged 150 ms added-first-byte p95 gate at 153.052 ms. Profiling identifies repeated whole-input regex scans for the built-in IPv6/address-port and card/card-run candidate languages. Their validators remain necessary even when candidate syntax is simple.

Replace only those two fixed built-in candidate expressions with exact byte scanners. Return the same greedy, nonoverlapping byte-index pairs as Go regexp, including Unicode and invalid UTF-8 separators. Preserve every existing finder, refiner, span class, blocking policy and overlap-resolution rule. Custom regexes retain their existing engine. No request caching or new global mutable state.

Scheduling thresholds, dictionary-tail checks, alternative-literal prefilters and shared candidate results were measured privately and did not provide a sufficient improvement; retain those diagnostics and do not ship them. Direct scanning targets the measured regex cost without coupling detector results or changing parallel scheduling.

Acceptance requires differential table/exhaustive/fuzz tests against the original regexes, integration parity for built-in and blocking scans, race and repository quality checks, alternating workload benchmarks, independent final review and unchanged full qualification gates. Diagnostic speedups do not qualify the operating envelope.
