# Baseline acceptance evidence evaluator

`baseline_acceptance.py` reads an existing private campaign and writes a new
private summary. It does not start load, modify measurements, or qualify the
proxy. Use a new output filename each time:

```sh
python3 scripts/workflow/baseline_acceptance.py "$CAMPAIGN_DIR" \
  --output "$PRIVATE_SUMMARY_PATH"
```

The reader snapshots the manifest, rejects duplicate JSON/job identities,
checks its identity and job hashes, verifies retained report hashes, and
refuses escaped paths, symlinks and nonregular files. A running attempt is
not successful evidence. Reports are evaluated one at a time. Hashes check
consistency; they do not authenticate a manifest supplied by another party.

Output contains fixed field names, numerical observations, counts and fixed
diagnostic codes. Arbitrary input strings and filesystem paths are not copied
into the summary. The CLI publishes a mode-0600 file atomically and refuses an
existing destination. A zero exit status means the summary was written; it
does not mean numerical gates or qualification passed.

## Observations

The evaluator recomputes paired first-byte and semantic-prefix relay
quantiles from operation records, preserving negative differences and sample
identity. It retains unsuccessful operations in outcome counts. Throughput
counts successful completions within equal 600-second windows; completions
after a window do not improve its rate. Four-client normal text receives the
version-1 thresholds; other text concurrency levels remain capacity evidence.

Images and PDFs have separate run-time targets. Queue/run samples must cover
the retained operations, and their combined durations expose request-budget
exceedances. Fairness keeps text and image outcomes separate and validates
their partition. Overload keeps scheduled, sent and client-unsent work visible
and checks the 45-second observed request bound. RSS peaks are recomputed
from samples when available; extraction/mixed summaries are labeled as
summary evidence and require recorded observations. A known peak exceedance
is not hidden by a missing sample elsewhere.

Each gate is pass, fail, incomplete or invalid. Passing numerical observations
are conditional on their recorded evidence, not a complete release decision.
Campaign coverage and before/after reference observations are separate checks.
Incomplete repetitions, unavailable data, smoke runs and inconsistent evidence
cannot become a qualified baseline. Consistent dirty-source smoke evidence
remains diagnostic rather than being mistaken for clean reference evidence.

## Remaining qualification evidence

`performance_qualified` remains false. Stress/churn, component profiles,
cancellation evidence, complete reference coverage and cross-workload review
remain separate requirements. Extraction and fairness reports do not retain
an independent elapsed warm-up duration in every phase. Extraction observations
use the runner's declared warm-up duration; fairness text sampling remains
incomplete where that duration is absent. Neither substitutes for full
qualification evidence. Full soak qualification belongs to roadmap Task 11.

## Verification in progress

The focused evaluator suite passes 54 tests. Tests first failed for missing
behaviors, then passed for threshold boundaries, unsuccessful denominators,
nonfinite samples, request pairing, incomplete windows, tampering, path safety,
partial campaigns, output privacy and native-evidence schema differences.
The retained six-job native smoke campaign remains unqualified. The first
reference report is recognized as a first-byte p95 target failure while its
stream and RSS observations pass; full campaign coverage remains incomplete.

The full workflow suite and review are pending remote CI so local server/build
work does not interfere with the running reference campaign. Task 04 remains
in progress.
