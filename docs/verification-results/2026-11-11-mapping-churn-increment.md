# Mapping-growth measurement increment

Task 04 remains in progress. This runner introduces one deterministic fresh pair every 0.6 seconds into a normal-policy vault initially seeded with 10,000 pairs. Four in-flight requests bound offered load. Defaults are 600 seconds, three repetitions, 1 MiB bodies, and at least 60 seconds/100 successful warm-up operations using existing mappings. Proxy/bypass arm order alternates.

The proxy boundary oracle remains pending until a read-only, post-drain vault snapshot resolves each expected sanitized-body digest. After traffic and resource collection finish, the runner records the live vault count, gracefully stops its own proxy and requires successful shutdown persistence before inspection. The persisted count must match the live count; shutdown time is outside request latency samples. This avoids reading the prior snapshot between the proxy’s 30-second periodic saves. Stream restoration is checked during the request. Pending or missing boundary evidence counts as failure; it cannot become a supported success. Unsent slots and failed attempts remain in reports. Observed retained mappings and fully verified new mappings are separate counts. The runner enforces the 512 MiB proxy and 1 GiB child peak budgets; missing measured resource observations are incomplete, and known excesses remain failures. Peaks include warm-up and measured load, while sampled active requests and achieved client concurrency describe measured traffic only. Warm-up resource samples are retained separately. These are exploratory growth measurements, not normal-tier performance qualification or the Task 11 soak.

The focused Python regression set includes 56 checks with the existing statistics/workload suites. CI exercises the read-only Go helper and, within the existing Linux job, a bounded real-binary smoke. The smoke requires one new retained and verified mapping, exact live/persisted population agreement, both arm denominators and complete streams. It adds no CI job and remains subject to matrix fail-fast and packaging dependencies. [PR 37](https://github.com/rozdolsky33/cui-proxy/pull/37) records per-head CI and review evidence. No growth reference measurements have been collected yet; a successful two-operation smoke cannot qualify performance. Local builds and native workloads remain deferred while the frozen text reference campaign runs.

Private execution (only on an otherwise idle measurement host):

```sh
python3 scripts/workflow/performance_churn.py --binary "$PRIVATE_PROXY" --seed-binary "$PRIVATE_SEED" --report "$PRIVATE_REPORT"
```

The command refuses existing reports, keeps captured data private and always reports performance qualification as false. Do not commit raw reports, boundary digests, mapping tables or profiles.
