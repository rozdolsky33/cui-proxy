# Mapping churn measurement increment

Continue Task 04 under the daily-use workload contract. This is diagnostic capacity evidence; normal-tier qualification and the Task 11 soak remain separate.

## Required behavior

Start with the normal policy and 10,000 prepopulated original/class pairs. Offer one new deterministic pair every 0.6 seconds (100 per minute), bounded to four in-flight requests, for 600 seconds and three repetitions. Each 1 MiB request retains the normal request shapes and sensitive-value population, replacing one selected original/class pair at its existing repeated occurrences with a fresh pair of the same class. Warm up with existing pairs only for at least 60 seconds and 100 successful operations. Record scheduled, sent, unsent, completed and supported-success counts without dropping failures. Never count a scheduled but unsent pair as an actual new mapping.

Use the normal corpus prefix of the vault-tier generator for initial state and its disjoint suffix for fresh values; verify policy equality, prefix identity, uniqueness, class identity and body-size limits. Preserve all negative controls and semantic stream checks.

## Boundary oracle

New mappings are unknown before the request. Do not seed future pairs into the live proxy vault or waive the upstream boundary oracle. Add an explicit deferred boundary-check path that captures only fixed-length upstream body/layout digests in private evidence; it must remain pending, never successful, until resolved. After traffic drains, record live metrics/counts, stop resource collection, gracefully stop the owned proxy to flush its periodic vault state, and require saved/live population agreement. Use a read-only helper on the isolated vault to resolve the actual mappings, construct the independently expected sanitized bodies and compare every digest. Missing mappings, incomplete streams, mismatches or unavailable final vault evidence are failures/incomplete evidence. Retain the original restoration oracle throughout.

Run a matched bypass arm using the same offered schedule and original bodies. As in the existing text runner, the bypass boundary oracle is the original body digest; only the proxy arm needs the observed mappings. Alternate arm order across repetitions. These runs do not contribute to the normal paired qualification gate. No real values or mapping tables enter committed reports.

## Verification and execution

1. Add pure-data regressions for disjoint deterministic populations, one fresh pair per request, exact 100/minute scheduling, unsent accounting and unchanged initial vault population.
2. Add read-only oracle-helper tests and deferred-boundary regressions proving pending checks cannot count as success, including missing mappings, digest mismatch, malformed evidence and failed restoration.
3. Add isolated phase orchestration and failure-evidence tests. Capture every runner/test output privately.
4. Verify native smoke after the running reference text campaign stops cleanly. No local workload/build runs during its timed measurements.
5. Independently review, pass full CI and merge the infrastructure before freezing its source/build identity for the three reference repetitions.
6. Report actual new mapping count, vault population, peak RSS/heap, bytes per new mapping where observations support it, outcome counts and latency. Enforce peak budgets; do not apply fixed-state leak percentages to intentional growth.

The existing reference campaign and its artifacts remain untouched. The current primary checkout stays clean on main.
