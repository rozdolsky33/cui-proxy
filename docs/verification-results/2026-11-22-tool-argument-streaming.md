# Tool-argument streaming preflight

The replacement November 22 sustained campaign completed all four two-hour
phases, but it is **not accepted**. The timeline, provenance, sealed event
bindings and restart/reload checks passed. Cancellation-report omissions prevent
complete evaluation, and diagnostic decomposition identifies a real tool-string
buffering delay. No failed gate has been waived and the sealed campaign remains
unchanged.

The proxy phases completed 14,479 fixed-state and 14,284 churn operations with
zero recorded operation failures; each bypass phase replayed the same count.
All ten restarts and ten policy reloads completed. Recorded operation success
alone does not establish latency or full milestone acceptance.

## First reproducible bottleneck

Function-call argument deltas previously buffered each entire JSON string before
decoding, restoring and emitting it. A long command therefore withheld ordinary
argument text until its closing quote arrived. Diagnostic fixed/churn relay p99
was approximately 811/797 ms for W04; the unchanged target is 100 ms. Plain
message scenarios W01/W08 measured approximately 61 ms. First-byte latency stayed
within its separate limits.

Argument keys and ordinary string values now decode complete JSON units incrementally,
use the existing streaming restorer, and encode restored content safely. Raw
spellings are retained for unchanged content. Numeric tokens and string values
that may contain nested JSON retain complete-token processing. Split escapes,
UTF-8 sequences and surrogate pairs remain pending until they can be decoded.
Response-wide input limits, depth checks and final JSON validation remain in
place. A scan offset avoids repeatedly decoding long whitespace prefixes;
bounded decoder batches limit temporary spelling metadata.

## Reporting correction

Successful W12 cancellation verification checked the upstream receipt and release
but omitted its operation identifier and receipt-verification flag from the
returned report. Future reports retain both fields after verification succeeds.
The regression test fails with the old collector and passes with the correction.
This does not add missing fields to any retained sealed campaign.

## Validation and remaining work

All 25 Go packages passed race tests; vet and current staticcheck passed. All 818
native workflow tests passed. Adapter/restoration regressions cover early string
emission, unchanged escape spellings, restoration at every split and byte-sized
chunks, nested JSON, and bounded allocation for whitespace prefixes. The new
decoder and existing argument-JSON fuzz targets each passed a 20-second run.
Independent review's allocation findings were corrected and re-reviewed with no
remaining Critical or Important findings.

Tool timing now also associates corresponding decoded logical prefixes: equal
byte fractions of original and masked argument documents are not necessarily
corresponding events. An exact, unambiguous tool recipe identifies literal
prefixes and atomic substituted values. Client and backend derive the same
milestone IDs with separate raw offsets. The sender records when actual source
frames supply those milestones; its existing 32 raw deltas and pacing remain.
Qualified observers require matching IDs and valid ordered timing arrays.
Ordinary-text buffering still fails the unchanged 100 ms regression gate.
Independent review found no Critical or Important issues in this association.
The complete native workflow suite passed all 826 tests after this change.

The short native paired run then exposed a further collector integration gap:
several canonical milestones can become available in the same source frame.
The generic relay calculator rejected their identical positive-duration time
intervals and returned unavailable timing arrays. It now accepts an overlap
only when both timestamps exactly repeat the previous interval. Distinct
overlaps, backward times and negative durations remain rejected. A regression
through sender, metadata, observer and report retains samples when milestones
share a positive-duration source interval. Delayed ordinary text still exceeds
1,000 ms in the controlled test and fails the unchanged 100 ms target.
All 827 native workflow tests pass with this correction. No missing timing
samples in the retained native campaign have been filled retrospectively.

A subsequent native preflight found the remaining W03 delay in JSON keys:
all 72 measured proxy operations delivered each key's early and late prefixes
together, while bypass operations streamed them. Keys now use the same bounded
incremental decoder and plain-text restorer; nested-JSON classification remains
specific to string values. The new key-emission regression failed before this
correction and passes afterward.

Review also found that a separately computed frame-boundary formula disagreed
with the sender's ceiling cuts and understated some tool delays. Frame lookup
now uses cumulative lengths of the actual emitted deltas, verifies exact
argument reconstruction, and selects the first frame supplying each milestone.
Tests check 390 varied layouts against emitted data and reject inconsistent
layouts before writing. Those earlier native tool timings remain rejected
diagnostic evidence. With both corrections, all 25 Go packages pass race tests,
vet and staticcheck pass, both fuzz targets pass 20-second runs, and all 830
native workflow tests pass. Independent review found no Critical or Important
issues. Fresh native measurements are still required.

Run the focused native comparison and collect the required new acceptance
measurements for the frozen corrected candidate. Neither this preflight nor
prior artifact results qualifies a current release or establishes live-model
reasoning equivalence.
