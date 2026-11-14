# Task 11 sustained acceptance implementation plan

Base: main9eea84d. Spec: ../specs/2026-11-14-task11-soak-harness.md.
Execute inline under standing authorization. Keep primary main clean and the
active fairness worktree/binary frozen. No local builds or heavy tests while
reference measurements run; use the existing controller's quiet gap after its
first complete pair. Do not stop the reference process merely to run tests.

## Task 1: Deterministic schedule and churn assignment

Write invariant tests for exact complete-cycle category/scenario counts,
equal normal-size weighting, stable unique identities, invalid input rejection,
100/minute due indices, finite values, bounded capacity and retained backlog.
Run only this pure in-process metadata unit module during reference timing,
with captured duration; it starts no build, proxy or extraction workload. Observe
RED before implementation, then implement `soak_schedule.py` and run GREEN.
Run the full workflow suite during the next quiet gap. Expected: all tests pass without wall-clock sleeps or subprocesses.
This metadata layer does not establish semantic completion or soak acceptance.

## Task 2: Normal-policy semantic fixtures and backend scripts

Inspect existing fixture/backend/oracle interfaces locally without dumping
source or data into the conversation. Extend normal-pacing support for real
structured function arguments and W01–W12 workflows with independent client
oracles. Keep exactly the normal policy and payload contracts. RED/GREEN
mutation tests must detect missing sanitization, wrong restored values,
incorrect tool effects and forwarded negatives. Expected: all supported and
predeclared negative cases pass at one/four clients, with no raw output.

## Task 3: Persistent phases and matched replay

Implement the four-client schedule executor, deterministic trace replay,
persistent fixed/churn state, bounded cancellation and request outcomes,
real extraction and observed resource/GC samples. Add controlled under-load
restart/reload histories with exact restoration and observable policy coherence.
Test lifecycle cleanup and failure retention with fake clocks/process boundaries
before short real smoke runs. Expected: incomplete duration/sample runs remain
explicitly unqualified and every outcome reconciles.

## Task 4: Fail-closed acceptance evaluator

Require original durations, window gates, resource availability, identity and
trace correspondence. Test rejection of missing/duplicate/non-finite samples,
wrong mix, stale binaries, incorrect churn accounting, incomplete recovery and
missing semantic evidence. Preserve fault windows separately. Investigate any
sustained growth before completion. Expected: only a complete internally
consistent evidence set can satisfy the original gates.

## Task 5: Execute and integrate the full remaining contract

After harness correctness and original fairness prerequisites pass, execute
four-hour proxy soak and matched bypass trace, remaining throughput/extraction/
cancellation/stress measurements and the required repetitions. Stop subsequent
expensive jobs on a failed criterion; preserve evidence and fix the cause.
No successful subset can close Task11. Record exact passing/unmet gates, run
required checks, one fresh whole-branch review, fix material findings once with
RED/GREEN evidence, then merge passing PRs and synchronize main. Complete final
Task12 envelope publication only when the full requirement audit passes.

## Review Focus

No substituted smaller policy or text-only oracle may claim semantic coverage.
Request denominators and replay identities must preserve the prescribed mix,
churn rate and injected outcomes. Timing, warmup and resource gaps remain
visible. Restart/reload checks must prove historical values and coherent policy,
not merely healthy processes. Raw fixture material remains captured privately.
