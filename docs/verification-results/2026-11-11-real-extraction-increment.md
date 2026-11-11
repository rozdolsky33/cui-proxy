# Real extraction diagnostic increment — 2026-11-11

This is correctness and measurement-plumbing evidence, not performance
qualification. Task 04 remains in progress. No numerical gate is relaxed.

The version-1 native corpus passed independent Tesseract/Poppler validation for
all 802 generated attachments: 800 images and two PDFs. Version 2 adds complete
recipe coverage checks, resolved-font enforcement, exact tool versions and the
English traineddata hash. Explicit native fixture tests pass all eight tests.

The isolated proxy smoke matrix covers four PNG/JPEG templates at 1024x768 and
1920x1080, the two-page text PDF and scanned PDF, each at one and four clients,
and an additional repeated-image phase. All 13 phases completed; 52 of 52
measured operations succeeded, with 52 extraction-run and 52 queue-wait samples.
Each phase also completed four warm-up operations. Upstream request structure
and declared-text content were checked independently of extracted output;
existing response/restoration checks remained enabled. No live backend was used.

These runs used an optimized Go 1.26.11 proxy built from security commit
`bb1ee61ec7df6ef51af5a719e070409dc7a071aa`, with the normal seeded workload.
The diagnostic runner was under development, not a clean qualification revision.
Generator/renderer/runner hashes remained constant across all 13 successful
phases. The preceding attempt failed the metadata report guard and was retained;
it was not counted as a successful phase. Quiet-machine conditions were not
asserted. Four samples per phase cannot establish tail-latency guarantees.

Portable discovery reports 81 tests: 79 passed and two native opt-in tests
skipped.
The explicit real-proxy suite passes all 12 tests, including real image/PDF
extraction and metadata report serialization. Review regressions demonstrate
that post-load collection failures retain measured denominators, cleanup errors
preserve warm-up timeout evidence, nonfinite durations are rejected, existing
report files/symlinks are not overwritten at startup, and failed writes preserve
the previous completed report. Reports remain private and value-free.

Remaining limitations: the diagnostic stage collector uses the existing bounded
recent-request history (256 entries, 15-minute retention). A slow phase that
loses samples is invalidated, with operation outcomes retained. CLI preflight
does not repeat the stored font-name check; generation and full fixture
validation do enforce it. Full mixed user/tool, fairness, overload, actual
upstream-send timing, and repeated reference qualification remain outstanding.
