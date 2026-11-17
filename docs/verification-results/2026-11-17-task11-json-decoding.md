# Task 11: validated JSON string decoding

The durable reference campaign stopped at its first 1 MiB four-client text repetition: added first-byte p95 was 151.48266695905477 ms against the unchanged 150 ms limit. All 1,000 requests in each arm succeeded; relay p99 and resource gates passed. The failed report remains unqualified.

The targeted change avoids a second general JSON validation pass when decoding ordinary escapes in string tokens from documents already validated by the adapter. Plain strings retain their existing fast path. Unicode escapes, surrogate handling and invalid UTF-8 retain encoding/json decoding. The change does not alter input validation, policy, detection, restoration, streaming or numeric acceptance thresholds.

Three diagnostic rotating-request runs on the same synthetic 768 KiB workload gave baseline times of 27.428112, 27.218147 and 27.191410 ms; the candidate measured 23.785273, 23.624440 and 23.656778 ms. The median reduction is about 13%. These benchmarks are diagnostic, not end-to-end qualification. An alternative higher parallel-scan cutoff regressed performance and was discarded.

Validation includes the full Go race suite, adapter tests, 30 seconds of differential decoder fuzzing (427,327 executions), go vet, and staticcheck with all checks enabled. Independent code review found no actionable issues. End-to-end qualification remains outstanding.

The next acceptance experiment runs all three original four-client 1 MiB text repetitions, with the original warmup, request counts, matched bypass, correctness, latency and resource gates. Failed attempts remain preserved. Only after this workload passes will the remaining qualification scope be reassessed. Earlier results identify their original runtime and corpus; they must not be relabeled as results for this binary.
