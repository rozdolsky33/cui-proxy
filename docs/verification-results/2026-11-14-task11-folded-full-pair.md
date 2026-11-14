# Task 11 folded-prefilter full fairness pair: failed

The first full 768 KiB mixed fairness pair on candidate `600c803c198f64642dc41d4b98a3c1ca5569cc91` completed but did **not** qualify. This candidate's runtime tree is equivalent to merged `main` at `9eea84d`; the local equivalence and build manifests retain the exact identities. The remaining eight pairs were not released.

| Check | Observed | Required/result |
| --- | ---: | --- |
| Added text first-byte p95 | 152.847125 ms | <=150 ms: **fail** |
| Added text first-byte p99 | 167.214251 ms | <=500 ms: pass |
| Added logical-prefix relay p99 | 63.240084 ms | <=100 ms: pass |
| Proxy text correctness | 1,000/1,000 | pass |
| Proxy image correctness | 2,679/2,679 | pass |
| Bypass text correctness | 1,000/1,000 | pass |
| Bypass image correctness | 2,961/2,961 | pass |
| Image extraction run p95 | 238.145333 ms | individual timing check passed |
| Image queue p95 | 114.881625 ms | individual timing check passed |
| Maximum image extraction budget | 397.659416 ms | individual budget check passed |
| Peak proxy RSS | 71,385,088 bytes | below 512 MiB |
| Peak child RSS | 159,449,088 bytes | below 1 GiB |

Bypass ran first. Warmup completed 114 requests over 60.928191 seconds for bypass and 106 over 60.505676 seconds for proxy. Stage attribution coverage is complete with zero poll errors. All attempts remain in the evidence; no failed run was discarded or converted to a pass. Individual passing checks do not establish full image, fairness, throughput, or soak qualification.

The controller stopped after the failed pair. Its execution handle terminated with exit 1 (child pair exit 2); a captured process inventory found no remaining matching reference controller/proxy processes before broader workflow tests began. No threshold was changed and no later pair was started.

## Retained local evidence

- `/private/tmp/cui-task11-folded-qualified-786432-0.json`
- `/private/tmp/cui-task11-folded-qualified-786432-0.provenance.json`
- `/private/tmp/cui-task11-folded-qualified-786432-0.evaluation.json`
- `/private/tmp/cui-task11-folded-qualified-build.json`
- `/private/tmp/cui-task11-folded-merge-equivalence.json`
- `/private/tmp/cui-task11-folded-qualification-status.json`
- `/private/tmp/cui-task11-post-pair-process-check.json`

These private artifacts must be retained for further analysis. The roadmap remains open: investigate the first-byte failure, validate the sustained harness against a real proxy, and complete all original qualification requirements before release sign-off.
