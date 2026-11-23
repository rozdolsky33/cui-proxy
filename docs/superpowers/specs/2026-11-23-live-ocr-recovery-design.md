# Live OCR dependency recovery probe

The task 11 recovery requirement needs evidence from the exact c8 candidate
runtime, using a real OCR extractor and the existing canonical workflow oracle.
The current harness's synthetic OCR launcher is useful for deterministic tests
but cannot establish this live dependency behavior.

The parent approved this design on 2026-11-23. Preparation is static while the
eight-hour soak owns the reference machine lock. No tests, builds, extraction,
profiling, benchmarks, or live probe may run until the parent releases that lock.

## Approach

The probe reuses `performance.Environment`, including its inherited Harness
process and cleanup handling, its real backend and receipt API, and the
`extraction_performance.attachment_case` canonical image oracle. Text requests
use `performance.request` and `soak_boundary.verify_deferred_boundary`.

An isolated PATH contains three private regular-file launchers that execute the
resolved real Tesseract and Poppler tools. It never includes the host PATH.
Removing OCR renames only the private Tesseract launcher; neither actual tools
nor shared symlinks are modified. The launcher is restored in `finally`, including
when the operation fails. Private launcher contents and real tool hashes are
checked during cleanup.

Using the synthetic harness OCR launcher was rejected because it does not test
the native dependency. Renaming a real tool or changing the host PATH was rejected
because it would affect other work and introduce uncontrolled fallback paths.

## Required evidence sequence

1. Acquire the existing global reference lock nonblocking, without replacing its
   inode, before tool discovery, identity commands, or any child process.
2. Bind the binary SHA and clean embedded revision to
   `c8aaef5799c75bccb19a7121304198507d36649b`. Independently bind the clean producer
   commit and imported producer source files. The producer may be a later commit.
3. Select two image fixtures with distinct content hashes. Verify baseline image
   success against the canonical expected body and actual upstream receipt.
4. Remove the private OCR launcher and wait for the same running proxy's
   preflight API to report `missing_tool`. Its ten-second readiness cache is
   allowed to expire; the proxy is never restarted.
5. Start three text requests and observe all three active. While their measured
   intervals enclose the image failure, send the second, uncached image.
6. Require status 422, `attachment_image_missing_tool`, dependency recovery with
   `after_repair` and installation guidance, and no backend arrival for that
   operation. Require canonical semantics and boundary receipts for every text
   request.
7. Restore the private launcher, wait for the same proxy's readiness to recover,
   then require the previously rejected image to succeed canonically without a
   cache hit. Recheck that the failed operation never arrived upstream.
8. Validate all six decision records against the local operation identities.
   After child shutdown and captured log closure, scan every declared capture
   and log/stdout/stderr file against known corpus values, including their JSON
   encodings. Retain only safe capture labels, byte counts, digests, and privacy
   checks. The scan runs immediately before Harness directory cleanup, including
   final shutdown output; cleanup still runs if the scan fails.
9. Remove private environment data, verify real tools unchanged, and recheck
   runtime and producer identities. Missing final shutdown-log proof fails closed.

Any missing canonical, receipt, privacy, audit, cleanup, or identity evidence
fails closed. Reports contain only closed codes, hashes, counts, and booleans;
request IDs, fixture text, request bodies, replacements, raw errors, and raw audit
records are never copied into the report or standard output. On a failed probe
or privacy scan, final captured logs are preserved only in a new sibling
`<report>.private-failure-logs` directory, mode 0700, with mode 0600 files using
safe capture labels. Those private failure artifacts are never committed or
printed. Successful runs delete their raw captures after the final privacy scan.

## Execution interface

`scripts/workflow/ocr_recovery_probe.py` requires `--binary`,
`--binary-sha256`, `--binary-source`, `--seed-binary`, `--producer-source`,
`--producer-commit`, `--fixtures`, `--reference-lock`, and `--report`.
Optional `--tesseract`, `--pdftotext`, and `--pdftoppm` select exact real tools.
The report must be a new file outside the clean producer checkout. The existing
global lock file must be supplied explicitly. There is no fixture generation,
build, automatic test run, or automatic retry mode.

The new helper and tests remain unexecuted pending the parent-controlled lock
release, review, and sequential verification. They cannot yet be cited as passing
tests or live recovery evidence.
