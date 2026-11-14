# Task 11 direct JSON string output

The normal768KiB changing-body profile allocates about7.8MiB per request. Buffer growth accounts for41.53% of sampled allocated space. The current JSON walker encodes each changed string into a temporary bytes.Buffer, then copies that result into the final output buffer.

Keep encoding/json.Encoder with SetEscapeHTML(false), but write strings directly to the destination bytes.Buffer and remove only the newline added by that Encode call. Preserve quote's existing byte output for callers/tests by implementing it through the same helper. Do not add a custom JSON encoder, unsafe aliases, buffer pools, changed canonicalization, eager whole-body reservation or new caches. Unchanged raw nodes retain their original-byte path.

Require byte-for-byte equivalence with the existing encoding/json behavior for keys, values, control characters, invalid UTF-8, Unicode separators, HTML characters, existing buffer prefixes and subsequent output. Request and response transformation, depth limits, restoration and fail-closed policy remain unchanged.

Acceptance: exact differential tests/fuzz and affected race tests; eight alternating equal-work benchmark pairs using the same32changing768KiB bodies plus existing controls; at least10% lower request-walk allocated bytes without material (>10%) latency regression. Then three alternating short paired four-client768KiB mixed diagnostics, retaining every outcome and resource sample, with no material latency/resource regression. Stop early on a failed preflight rather than spend remaining runs. This targets allocation overhead; it cannot close the original150ms, throughput or four-hour soak gates by itself. Reject runtime changes if evidence does not support them.
