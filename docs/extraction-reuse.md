# Repeated image extraction

When attachment policy is `extract_text`, validated inline PNG and JPEG images can reuse successful OCR output for a short time. Every request still validates the image, charges its attachment count/decoded bytes/text budget, and sanitizes extracted text with the request's current rules. A policy reload can therefore block text from a previously successful cache entry. PDF extraction remains uncached.

The process-local cache retains at most **32 entries and 4 MiB of OCR output**, with **30-second expiry**. Hits do not refresh expiry. Least-recently-used entries are evicted when either bound is reached. Entries are removed by expiry timers even without new requests, subject to runtime scheduling. Removed output buffers are cleared; this is not a guarantee of secure memory erasure or a total-RSS limit.

Cached OCR output can contain sensitive plaintext until eviction or expiry. Images are not retained in the cache. Nothing is written to disk, and cache fingerprints/content are not exposed in metrics or logs. Restarting the process clears reuse state. Empty, invalid, oversized, failed and cancelled extraction results are not cached.

Reuse identity includes image content and format, the executable's exact contents and path, OCR options, the fixed helper environment, and the cache algorithm version. The current helper must still be discoverable. A missing, replaced or unreadable helper cannot reuse an old identity; if an executable cannot be fingerprinted safely or exceeds the 32 MiB fingerprint-read bound, extraction runs uncached. Library or language-data changes require a proxy restart for immediate invalidation; otherwise existing entries expire within their normal validity period.

Concurrent cold requests execute independently through the existing two-active/two-waiting helper gate. Cancelling one does not cancel another or cache a failure. This preserves bounded waiting and avoids a shared cancellation dependency; simultaneous first uses can still repeat OCR work.

Cache hits check the raw OCR-output byte count before trimming text, so reuse does not weaken the remaining per-request output allowance. Count, decoded-byte, pixel and text limits continue to apply on every hit. Reuse never substitutes for current sanitization or permits original images to be forwarded.

Performance evidence must distinguish repeated images from unique images. Short-lived reuse can help retries and repeated histories; it does not promise improvement for a stream of unique images or establish sustained workload qualification.

`extraction_cache_hits` is an integer aggregate in metrics and, when request correlation is enabled, in recent-request metadata. It counts retrieved cache entries, including requests subsequently refused by policy or output limits. A hit records known zero helper-wait time; extraction timing still covers validation, fingerprinting, lookup and output handling. No image identities are exposed.
