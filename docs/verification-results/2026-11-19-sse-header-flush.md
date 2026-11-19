# Event-stream headers flushed before the first restored event

Roadmap milestone 1 investigation of the failed four-client 1 MiB text gate
(151.48 ms added first-byte p95 against the 150 ms limit).

## Finding

The proxy wrote the response status and headers for an event stream but did
not flush them. They reached the client only with the first restored event.
In the performance fixture the first events carry a substitution split across
events, which restoration correctly holds until the synthetic's right boundary
is known. The client's first byte therefore waited for that holdback as well
as for request processing.

In a diagnostic run before the change, the median gap between the upstream
sending its first event and the client's first byte was 47.66 ms through the
proxy and 0.09 ms in bypass. Request-side stages accounted for the rest of the
added delay (median body inspection 28.6 ms, audit write 7.1 ms).

## Change

The stream path now flushes the status and headers as soon as the upstream
response passes the response guard. Restoration, holdback, policy and
validation are unchanged. One behavior differs: if the stream fails before
any event is released, the client now sees a started response that is then
aborted, where it previously saw a connection closed with no response. This
already happened for any failure after the first released event.

## Diagnostic comparison

One repetition each, four clients, 1 MiB normal tier, 240 paired requests,
10-second warm-up. All requests in every arm succeeded and all pairs were
valid.

| Metric (ms) | Before p50 / p95 / p99 | After p50 / p95 / p99 |
|---|---|---|
| Added first byte | 85.6 / 112.1 / 120.3 | 38.2 / 59.9 / 69.9 |
| Added logical-prefix delay | 41.8 / 86.8 / 253.3 | 41.2 / 81.8 / 95.8 |
| Added per-event relay delay | 0.1 / 45.3 / 49.7 | 0.1 / 44.8 / 49.7 |

The first-byte improvement comes from headers arriving earlier. The delay
before the first restored text is unchanged, as the prefix lag by event shows:
47.9 ms before and 47.4 ms after for the first event. The stream gate, not the
first-byte gate, covers that delay.

## What this does not establish

These runs are diagnostic and do not qualify anything. The machine was a
Mac16,5 with 16 logical CPUs, not the contract's reference machine; one-minute
load average was between 4.9 and 23.5 during the runs; the warm-up and sample
counts were below the contract's; and only one repetition was taken. The
before-and-after tail of the prefix delay differs for reasons this run cannot
separate from machine noise.

The four-client gate remains open until all three 1 MiB repetitions, and the
rest of the normal tier, pass on the reference machine under the contract's
protocol with this binary. The earlier failed result stays as recorded.
