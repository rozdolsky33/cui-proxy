# Task 11 direct JSON string output

The normal768KiB changing-body profile allocates about7.8MiB per request. Buffer growth accounts for41.53% of sampled allocated space. The current JSON walker encodes each changed string into a temporary bytes.Buffer, then copies that result into the final output buffer.

Keep encoding/json.Encoder with SetEscapeHTML(false), but write strings directly to the destination bytes.Buffer and remove only the newline added by that Encode call. Preserve quote's existing byte output for callers/tests by implementing it through the same helper. Do not add a custom JSON encoder, unsafe aliases, buffer pools, changed canonicalization, input-sized output reservation or new caches. Unchanged raw nodes retain their original-byte path.

Require byte-for-byte equivalence with the existing encoding/json behavior for keys, values, control characters, invalid UTF-8, Unicode separators, HTML characters, existing buffer prefixes and subsequent output. Request and response transformation, depth limits, restoration and fail-closed policy remain unchanged.

Acceptance: exact differential tests/fuzz and affected race tests; eight alternating equal-work benchmark pairs using the same32changing768KiB bodies plus existing controls; at least10% lower request-walk allocated bytes without material (>10%) latency regression. Then three alternating short paired four-client768KiB mixed diagnostics, retaining every outcome and resource sample, with no material latency/resource regression. Stop early on a failed preflight rather than spend remaining runs. This targets allocation overhead; it cannot close the original150ms, throughput or four-hour soak gates by itself. Reject runtime changes if evidence does not support them.


## Measured revisions

The first eight-pair result reduced changing-request allocations 9.797%, narrowly missing 10%, with latency +0.192%. Preserve that failed preflight. Two bounded character-counting revisions reduced allocation but regressed encoding controls (normal +84%, then Unicode +89%); neither qualifies. Their private evidence is retained.

The next experiment prepares actual standard-encoded fragments, then allocates the final owned output exactly once. One serializer traversal supports direct output and fragment output. Fragment planning applies only to rewritten/canonical root containers with original size 4KiB–1MiB and 16–4095 output parts; a saturating tree-only count bounds metadata without scanning string contents. Each quoted fragment owns its bytes; raw fragments reference the still-live input only until the final copy. No custom escaping, persistent caches, pools, unsafe aliases or reservation based on discarded image bytes. Ineligible trees retain direct output; unchanged noncanonical nodes return an owned raw-byte copy without interface dispatch. Check integer overflow before accumulating output length. Repeat the unchanged acceptance gates.
