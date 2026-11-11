# Reference baseline acceptance reporting

This is the remaining report work within roadmap Task 04, governed by the
daily-use workload contract and concurrent-performance-baseline plan.

## Design

Use a read-only Python evaluator beside the existing campaign runner. It reads
a snapshot of the private manifest, checks each retained successful report
against its recorded SHA-256 and expected relative filename, and emits only
fixed metric names, numerical summaries, counts and fixed diagnostic codes.
It must not copy arbitrary report text, paths, environment strings or payloads
into public output. It never modifies campaign artifacts.

Each gate has one of four states: pass, fail, incomplete, invalid. Missing
evidence is not a numerical failure or a pass. Malformed, inconsistent,
nonfinite or tampered evidence is invalid. A failed numerical target remains
failed even if other required evidence is missing; overall completeness is
reported separately. Every repetition is evaluated independently. Normal
four-client results receive the contract thresholds; one/eight/sixteen-client
and stress results remain capacity observations. Smoke cannot qualify.

Recompute paired first-byte and semantic-prefix relay distributions from
individual operation records with the existing strict pairing functions,
rather than trusting saved quantiles. Check outcome and sample denominators,
warm-up and sample/window requirements, repetition coverage, source identity,
resource availability and reference observations. Throughput uses successful
completions within equal 600-second windows. Extraction uses complete stage
samples and separate image/PDF targets. Fairness keeps text and image gates
separate and requires both. Missing cancellation, stress/churn or profile
evidence stays explicit; the initial report must not claim overall performance
qualification merely because a campaign completed.

## Steps and evidence

1. Add failing tests for missing evidence, exact threshold boundaries, negative
   paired deltas, failed operations retained in denominators, invalid numeric
   values and incomplete repetitions. Implement pure metric evaluation.
2. Add filesystem tests for private report hashes, escaped/symlink paths,
   duplicate identities and partial/running campaigns. Implement snapshot
   loading and fixed-field output.
3. Verify against the retained six-job native smoke artifacts and collected
   reference reports. They must remain unqualified unless every required
   reference criterion is actually established.
4. Run captured workflow discovery in a measurement-free interval, review the
   whole branch once, fix material findings with regressions, then commit,
   push and merge after required CI. Keep main current.

## Execution constraints

The reference campaign uses frozen source f0c3f37. Never edit its worktree or
rebuild its binaries during the run. Avoid local benchmark/build/OCR jobs
while timing. Use the already-passing main CI as initial baseline evidence;
run this branch's complete local checks only between measurement campaigns.
Short pure-data unit checks must not start servers, extractors or benchmarks.
The running campaign and all earlier failed evidence are retained.
