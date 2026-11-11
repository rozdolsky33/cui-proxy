# Durable audit component profiling

`BenchmarkAuditDurability` measures the production decision log's record
validation, hash-chain encoding, serialized append and per-record fsync.
Serial and parallel cases retain the actual durability path. Use `-cpu` to
select parallel worker counts; the benchmark reports workers explicitly.
Setup, close and full hash-chain verification are outside the timed region.
The benchmark fails unless every attempted record is verified with no chain
breaks, gaps or unverified records. Temporary logs contain fabricated metadata
and are removed by Go's test cleanup.

The isolated append profile uses a large rotation limit to separate ordinary
durable writes from rotation. Rotation/retention costs are not established
by this benchmark. The end-to-end runner continues to use its configured
production audit semantics.

CI and `make test` run a four-operation smoke of both cases within the existing
race-test step. This adds no job and keeps the earlier fail-fast ordering.
Those race-instrumented results validate execution and verified record counts;
they are not performance evidence. Optimized reference profiling runs are
separate and must execute only when other measurement workloads are idle.

For the reference profile, capture output and profiles privately per AGENTS.md:

```sh
go test -run '^$' -bench '^BenchmarkAuditDurability$' -benchtime=3s \
  -cpu=1,4 -count=1 -o "$PRIVATE_TEST_BINARY" -cpuprofile "$PRIVATE_CPU_PROFILE" \
  -memprofile "$PRIVATE_HEAP_PROFILE" -mutexprofile "$PRIVATE_MUTEX_PROFILE" \
  -blockprofile "$PRIVATE_BLOCK_PROFILE" -mutexprofilefraction=1 \
  -blockprofilerate=1 ./internal/benchmark
```

Record the clean source, Go toolchain, flags, host conditions and artifact
hashes. Interpret CPU/allocation/heap/lock profiles as attribution, not as
end-to-end release gates. Actual optimized measurements remain pending.
