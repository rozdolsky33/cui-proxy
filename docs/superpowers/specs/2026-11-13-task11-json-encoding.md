# Task 11 direct JSON string output

The normal768KiB changing-body profile allocates about7.8MiB per request. Buffer growth accounts for41.53% of sampled allocated space. The current JSON walker encodes each changed string into a temporary bytes.Buffer, then copies that result into the final output buffer.

Keep encoding/json.Encoder with SetEscapeHTML(false), but write strings directly to the destination bytes.Buffer and remove only the newline added by that Encode call. Preserve quote's existing byte output for callers/tests by implementing it through the same helper. Do not add a custom JSON encoder, unsafe aliases, buffer pools, changed canonicalization, input-sized output reservation or new caches. Unchanged raw nodes retain their original-byte path.

Require byte-for-byte equivalence with the existing encoding/json behavior for keys, values, control characters, invalid UTF-8, Unicode separators, HTML characters, existing buffer prefixes and subsequent output. Request and response transformation, depth limits, restoration and fail-closed policy remain unchanged.

Acceptance: exact differential tests/fuzz and affected race tests; eight alternating equal-work benchmark pairs using the same32changing768KiB bodies plus existing controls; at least10% lower request-walk allocated bytes without material (>10%) latency regression. Then three alternating short paired four-client768KiB mixed diagnostics, retaining every outcome and resource sample, with no material latency/resource regression. Stop early on a failed preflight rather than spend remaining runs. This targets allocation overhead; it cannot close the original150ms, throughput or four-hour soak gates by itself. Reject runtime changes if evidence does not support them.


## Measured revision

The first eight-pair result reduced changing-request allocations9.797%, narrowly missing10%, with latency+0.192%. Preserve that failed preflight. Add a bounded output-tree exact-size capacity hint, capped at1MiB. It must never exceed actual serialized size or use the old raw input size for rewritten nodes; extracted large images that become small text must reserve only their small output shape. Saturating arithmetic and the existing depth bound constrain work. Count the standard encoder's escaping without allocating; encoding/json still produces every output byte. Reserve only for rewritten/canonical containers whose original size is4KiB–1MiB and whose exact output count remains below1MiB; otherwise retain normal buffer growth. Unchanged raw nodes and tiny/very large inputs avoid the counting pass. Repeat the same gates without lowering thresholds.
