# Extraction stage retention for long runs

Extraction measurement phases now collect stage records continuously while requests run. Previously, reading only the final recent-history snapshot could lose early timings after the server's bounded history expired. The shared collector retains observed records, excludes warmup sessions, and still rejects missing or extra measured records. Polling error counts are included in the metadata report; no absent timing is treated as zero.

Collection cleanup failures preserve completed request records. A regression supplies retained stages after the recent-history snapshot no longer contains them, while separate collector tests cover rotation, deduplication and polling errors. Native image/PDF checks validate the integrated path.

The extraction CLI now checks the declared rendering font before measurement, matching full fixture validation. Unexpected or missing font metadata is rejected; the supported fixture corpus continues to use Menlo-Regular.

This removes a diagnostic limitation before full reference runs. It does not qualify performance, satisfy repetition requirements, or complete roadmap Task 04. All reports remain explicitly unqualified until the consolidated reference evaluation is available.

Verification completed successfully: 110 workflow tests discovered (106 passed, four native opt-in skips), 14 native extraction tests passed, and diff checks passed. The expiry and font-check regressions were observed failing before their respective fixes.
