# Capacity and overload recovery

The proxy admits a bounded amount of work across all API handlers in one process. These limits apply before request metadata scanning or body reads and remain charged until the response finishes or is cancelled.

| Resource | Bound |
|---|---:|
| Active API requests | 8 |
| Waiting API requests | 8 |
| Reserved input bytes | 64 MiB |
| Maximum admission wait | 5 seconds |
| Active extraction helpers | 2 |
| Waiting extraction helpers | 2 |
| Extraction request deadline | 30 seconds (unchanged) |

A request with a bounded body of at most 2 MiB is small. Bulk requests can occupy at most six slots and 56 MiB, preserving two slots and 8 MiB for small work. Small requests borrow unused general capacity, but borrowing stops while a bulk request waits so a continuous stream of small arrivals cannot starve it. Each lane is FIFO and eligible lane heads alternate. Classification uses the enforced body bound, never a client or agent label.

Known Content-Length reserves that many input bytes. Unknown-length bodies reserve the policy maximum, so chunked requests can have lower concurrency. Body reads cannot exceed the reservation or policy limit. A reservation above 64 MiB (or above the bulk share of 56 MiB) cannot fit and is refused immediately. Send an accurate Content-Length or reduce the configured body allowance/request size when conservative unknown-length reservations are the cause. Raising the policy body limit does not increase process capacity.

The input-byte reservation is an estimate of working memory, **not an RSS limit**. A fourfold copy allowance for 64 MiB is 256 MiB; runtime state, mappings, restored output and extraction buffers also consume memory. Keep monitoring actual process and child RSS against the workload contract.

Full queues, admission timeouts and impossible reservations return HTTP **503**, error code `overload`, `Retry-After: 1`, a request ID and backoff guidance. Retry with bounded exponential backoff; avoid synchronized retry loops. A cancelled queued request releases its queue position promptly. A response stream holds its slot until completion or cancellation. Model-list requests share the bound; the local dashboard/preflight endpoints remain separate.

Extraction saturation retains the existing `attachment_*_busy` diagnostic and backoff guidance. The two helper slots and two wait positions are independent of text processing. Unsupported or oversized attachments retain their existing refusal behavior. Neither admission overload nor extraction saturation forwards original content, including when sanitization error passthrough is configured.

`/api/metrics` exposes `admission.active`, `admission.queued`, and `admission.reserved_input_bytes`. The `admission_wait` timing stage includes admission acquisition, with the same fixed-cardinality summaries as other stages. Existing extraction active/queued gauges and wait timing remain available. No request bodies, client labels or sensitive values are added to metrics or logs.

These fixed defaults target the four-agent workload. They bound admitted requests and waiting work, not network connection counts or all operating-system allocations. Sustained throughput and the mixed-traffic latency budget remain subject to the roadmap's measured acceptance gate.
