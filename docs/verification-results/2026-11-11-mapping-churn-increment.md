# Mapping-growth measurement increment

Task 04 remains in progress. This runner introduces one deterministic fresh pair every 0.6 seconds into a normal-policy vault initially seeded with 10,000 pairs. Four in-flight requests bound offered load. Defaults are 600 seconds, three repetitions, 1 MiB bodies, and at least 60 seconds/100 successful warm-up operations using existing mappings. Proxy/bypass arm order alternates.

The proxy boundary oracle remains pending until a read-only, post-drain vault snapshot resolves each expected sanitized-body digest. Stream restoration is checked during the request. Pending or missing boundary evidence counts as failure; it cannot become a supported success. Unsent slots and failed attempts remain in reports. Observed retained mappings and fully verified new mappings are separate counts. These are exploratory growth measurements, not normal-tier performance qualification or the Task 11 soak.

The Python phase, CLI, schema, failure-preservation and deterministic workload checks pass 45 focused regressions with the existing statistics/workload suites. Native helper verification, live smoke, review and full CI are pending. No growth reference measurements have been collected yet. Local builds and native workloads remain deferred while the frozen text reference campaign runs.

Private execution (after the native helper is complete and verified):

```sh
python3 scripts/workflow/performance_churn.py --binary "$PRIVATE_PROXY" --seed-binary "$PRIVATE_SEED" --report "$PRIVATE_REPORT"
```

The command refuses existing reports, keeps captured data private and always reports performance qualification as false. Do not commit raw reports, boundary digests, mapping tables or profiles.
