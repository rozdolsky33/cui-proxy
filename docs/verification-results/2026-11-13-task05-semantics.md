# Task 05 semantic compatibility evidence — 2026-11-13

Scope: existing Codex adapter behavior, deterministic local task effects and
optional paired-live outcome accounting. No production protocol acceptance or
policy rule changed. See the [support matrix](../protocol-support.md).

## Evidence

- Six new top-level Go semantic tests cover 64 repeated-history turns,
  compaction/delegation, fresh-stream reconnect, command/configuration effects,
  patch application and four restored text stream variants across four terminal
  events. Function arguments are tested at three chunk sizes.
- The adapter/transform run passed 231 tests/subtests before final verification.
- Two controlled production mutations were detected: bypassing request scanning
  failed the semantic boundary tests; removing nested string restoration failed
  the configuration effect test. Production files were restored immediately.
- The deterministic HTTP workflow corpus passed all 125 result entries against
  the Task 05 build. No live provider was contacted.
- The full Python suite passed 266 tests with four existing opt-in skips. Eight
  new tests cover paired outcomes, missing/duplicate/mismatched pairs, invalid
  records, failure denominators and value-free CLI errors.
- Final Go race verification passed all 20 packages; vet and full staticcheck
  passed, and the strict manual build passed. The first race run rejected a stale bundled
  manual; it was rebuilt after registering the support page in navigation.
  A redundant test-only rune conversion reported by staticcheck was removed.

## Interpretation

The effect oracles run fixed local commands and apply fixed diffs in temporary
directories. They inspect resulting file bytes and JSON values, including exact
large-integer and exponent spellings. They do not execute model-generated code.

Unknown reasoning/delta events remain opaque, successful non-SSE output remains
blocked, and encrypted compaction is not decrypted or qualified semantically.
Reconnect evidence covers a new complete response stream, not suffix replay.
Sensitive numeric literals are subject to the existing policy and representation
limits described in the matrix.

No live model-quality samples were collected. The offline paired summarizer and
five-case procedure support future authorized trials; they cannot establish
causality, statistical equivalence or model-quality qualification without them.
Task 04 performance failures remain unchanged.

## Decisions

- Continue inline under standing authorization; routine implementation choices
  remain delegated to the agent.
- Qualify existing behavior using mutation checks rather than unnecessary
  production edits. The mutation sample is limited, not exhaustive.
- Keep live execution conditional and unmeasured, avoiding unauthorized provider
  spending; no conclusion about model reasoning quality follows.
