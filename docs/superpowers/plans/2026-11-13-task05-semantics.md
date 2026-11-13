# Task 05 implementation plan

Spec: `docs/superpowers/specs/2026-11-13-task05-semantics.md`

## Global Constraints

Work in the isolated task branch. Preserve all security gates and existing
protocol boundaries. Capture tests and fixtures privately; print counts/status
only. Existing standing authorization covers commits, PR and merge after checks.
Characterization tests may pass initially; prove their failure sensitivity with
controlled mutations rather than inventing production changes.

## Task 1: Session and effect contracts

- [ ] Add `internal/adapter/codex/semantics_test.go`.
- [ ] Test repeated histories, compaction/delegation and interrupted/new streams.
- [ ] Test restored argv, file contents and configuration values with fixed local
  execution and independent expected outcomes. Cover quoting and numeric forms.
- [ ] Run adapter tests; expected all pass. Check representative mutations fail.
- [ ] Fix any observed production failures with a failing regression first.

Interfaces: existing SanitizeRequest, NewResponseRewriter, Event, Flush and vault.
No new production API. Tests: TestSemanticsRepeatedHistory,
TestSemanticsCompactionDelegation, TestSemanticsReconnect,
TestSemanticsCommandEffects, TestSemanticsPatchEffects, TestSemanticsStreamVariants.

## Task 2: Paired live evidence accounting

- [ ] Add `scripts/workflow/test_live_semantics.py` before implementation.
- [ ] Verify incomplete/duplicate pairs, missing arms, unsafe free text, model
  mismatches, model variance and proxy failures are handled distinctly.
- [ ] Implement `scripts/workflow/live_semantics.py` as an offline validator and
  aggregate reporter; no provider calls or credentials.
- [ ] Run Python suite; expected pass. Record live quality as unmeasured.

Interfaces: versioned JSON records with pair ID, case ID, model, arm, status and
boolean semantic outcome. Aggregate output contains counts only, not inputs.

## Task 3: Matrix and integration

- [ ] Add `docs/protocol-support.md`, link from Codex documentation, update roadmap.
- [ ] Record deterministic verification and conditional live limitations.
- [ ] Run Go race suite, Python suite, vet and full staticcheck; expected pass.
- [ ] Fresh whole-branch review, fix material findings, commit/push/PR.
- [ ] Wait for required CI, merge, synchronize clean primary main.

Interfaces: matrix links to actual tests; no assertion of unknown protocol support.

## Review Focus

1. Opaque protocol fields must not be mistaken for restored semantic content.
2. Interrupted streams must not leak buffered state into a new response.
3. Tests must inspect command/file effects rather than only serialized equality.
4. Live failure denominators must not hide missing pairs or proxy failures.
5. Matrix claims must match existing transport and attachment rejection behavior.
