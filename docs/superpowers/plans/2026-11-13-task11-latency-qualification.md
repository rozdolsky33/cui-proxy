# Task 11 full-sample fairness latency plan

Use executing-plans inline; standing authorization opts out of repeated design/merge questions.
Base:40e312227e368bc085cce69c29b4f78a65f8a726 (PR47).
Spec:docs/superpowers/specs/2026-11-13-task11-latency-qualification.md.

## Review Focus
1. Full warmup, sample counts, matched identity/order and nearest-rank comparisons satisfy the existing contract.
2. Failed, cancelled and missing observations stay visible; fail-fast means stopping subsequent repetitions, never discarding a failed one.
3. Exact stage/resource evidence is available; no short-run or subset result closes unrelated gates.
4. The tested binary matches merged runtime source and no competing build/test contaminates timing.

## Task 1: Prepare and measure
- [ ] Record PR47 integration and update prior plan status.
- [ ] Build immutable current-main binary and capture source/binary manifest.
- [ ] Execute first768KiB full-warmup/full-sample pair with captured outputs.
- [ ] Evaluate every scoped criterion; stop on failure or missing evidence.
- [ ] Only after passing, complete remaining repetitions and sizes with alternating order.

## Task 2: Record and integrate
- [ ] Record numeric evidence, exact limitations and next concrete action.
- [ ] Fix any demonstrated harness issue with RED/GREEN tests; otherwise avoid unrelated runtime changes.
- [ ] Verify documentation and relevant changed paths; independent whole-branch review.
- [ ] Commit/push/PR; merge after all exact-head checks; synchronize clean primary main.
- [ ] Continue the highest-impact unmet original gate.
