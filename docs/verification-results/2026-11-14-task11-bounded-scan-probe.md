# Bounded detector-window investigation

Status: diagnostic evidence only; no production change and no qualification claim.

Baseline source: `9eea84d9fd653659655af672de4f7ab362c937dc`. Exact compiler: Go 1.26.11, Darwin arm64. Detector baseline tests passed.

The original full mixed pair remains failed at 152.847125 ms added first-byte p95 against the unchanged 150 ms limit. This experiment investigates one possible reduction in detector CPU work.

A standalone diagnostic merges overlapping digit-candidate windows, retaining one byte of left context and finite maximum width plus right context. It runs the original regexp on each merged window. Non-ASCII input falls back to the original whole-input scan. A coverage budget falls back when windows cover more than one eighth of a large input.

Six expressions covered bounded digit starts, capture groups, word boundaries, line and absolute anchors, and lazy repetition. Each run compared 72,000 generated ASCII cases against the original regexp; three repeated runs passed (216,000 comparisons, not 216,000 distinct cases). This does not yet validate a syntax classifier, integration, or Unicode behavior.

| Input | Original median ns/op | Window median ns/op |
| --- | ---: | ---: |
| sparse | 12347661 | 493040 |
| dense | 37086097 | 37163228 |

Three 500 ms microbenchmark repetitions; approximately 768 KiB sparse prose or 770 KiB dense digits. These are diagnostic inputs, not the canonical qualification workload. Initial unbudgeted windows showed a dense-input regression; that failed variant and both toolchain-selection failures remain in private evidence.

Next requirements: conservatively derive finite width and strictly digit-only first consumed characters; reject nullable, unbounded, unsupported, native-prefix and group-mode cases; keep original match indices/refiners; cover Unicode/invalid UTF-8 fallbacks and dense workloads with differential tests and fuzzing; measure complete detector and mixed proxy workloads before any performance claim.

Private artifacts (captured raw inputs/results are not published):
- `/private/tmp/cui-bounded-probe/probe_test.go` SHA-256 `3eb18c4362889a5505a41dbb6317698fe081af775f4b7f6c7bf43d12d1db8407`.
- `/private/tmp/cui-bounded-probe/results-exact.json` SHA-256 `0c4be012518a9b24b2af5f60a0a75ac7bb67582fd945c3c510f70f430575f0b2`.
- `/private/tmp/cui-bounded-probe/results-budget.json` SHA-256 `03d526334be108988b11b4781e50f88fe61160cebb6cc4fc41f0ce020375da28`.

## Production candidate correctness checkpoint

The subsequent production candidate uses conservative syntax metadata and rejects absolute/line anchors. Eligibility tests failed with eight expected assertions against a fallback stub, then passed after implementation. Raw-match differential tests failed against an empty-result stub, then passed. An intentionally incorrect capture-offset translation was detected and reverted. Built-in final-span comparisons preserve refinement behavior.

Go 1.26.11 verification: all 23 repository packages passed race tests; vet and all staticcheck checks passed; 30 seconds of differential fuzzing completed 1,187 executions without mismatch. Captured private logs use `/private/tmp/cui-bounded-` prefixes. This checkpoint establishes tested correctness only; complete-workload performance, independent final review and integration remain outstanding.
