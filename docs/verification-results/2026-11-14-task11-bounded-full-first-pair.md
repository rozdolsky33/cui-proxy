# Task 11 bounded scanner: first full fairness pair

The frozen candidate at source commit `5d7112e47063a65efee62f2f326c7060e5df9cf6`
completed the original 768 KiB, four-client, repetition-0 bypass-first fairness
pair. This is one passing pair, not full performance qualification. Remaining
sizes/repetitions and the other original release gates remain open.

Both halves used at least 60 seconds and 100 successful warmup operations,
then 1,000 measured text operations alongside real image extraction. All
7,620 measured supported operations succeeded (3,950 bypass; 3,670 proxy).
No stage polling errors occurred, and stage coverage was complete.

| Criterion | Observed | Original limit |
| --- | ---: | ---: |
| Added text first-byte p95 | 142.807 ms | 150 ms |
| Added text first-byte p99 | 155.356 ms | 500 ms |
| Added matched logical-prefix relay p99 | 61.586 ms | 100 ms |
| Image extraction run p95 | 238.248 ms | 5,000 ms |
| Extraction queue wait p95 | 109.946 ms | 2,000 ms |
| Maximum extraction phase | 819.623 ms | 30,000 ms |
| Peak proxy RSS | 71,565,312 bytes | 536,870,912 bytes |
| Peak extraction child RSS | 159,121,408 bytes | 1,073,741,824 bytes |

## Controller correction and retained evidence

The private orchestration controller initially stopped with `pair_failed` because
it tested the evaluator's intentionally false aggregate `performance_qualified`
field. The per-pair sampling, correctness, latency, extraction, and resource gate
states all passed. That aggregate field remains false until the complete
campaign is qualified; it is not the individual pair decision.

A regression test reproduced the false stop. The controller now requires all ten
explicit original gate states to be `pass`, complete stage coverage, zero polling
errors, and completed measurement. Tests reject every missing/failed gate and
incomplete coverage, polling, or completion. Three tests passed after the fix.
The evaluator's full-campaign qualification fields remain false, and no numeric
limit changed.

The original measurement, original failed evaluation and controller status are
retained privately. A separate `.reevaluation.json` records the corrected passing
pair decision, without rerunning or replacing the measured evidence. The next
controller must resume from the remaining pairs only after validating this first
pair's frozen build/provenance identity; no duplicate first-pair run is needed.
