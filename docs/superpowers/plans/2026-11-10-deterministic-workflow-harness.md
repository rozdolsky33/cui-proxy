# Deterministic workflow harness implementation plan

**Goal:** Complete roadmap Task 02 using the version 1 workload contract.
**Spec:** ../specs/2026-11-10-daily-use-workload-contract.md
**Architecture:** A Python standard-library runner launches the built Go proxy and a separate scripted fake upstream. The upstream has no oracle; client-side assertions verify sanitized boundaries and restored semantic effects. Generated config and fixtures stay in a private temporary directory.
**Scope:** Deterministic correctness at one and four logical clients. Performance measurement, real OCR quality, compaction and restart qualification remain later tasks.

## Tasks

- [x] Write failing oracle tests detecting leaked originals, wrong restoration, forwarded negatives and misclassified failures.
- [x] Implement the runner, scenario manifest, separate upstream process, deterministic OCR subprocess, lifecycle cleanup and captured reporting.
- [x] Exercise W01–W08/W11/W12 through the actual built proxy at one and four clients, plus bypass controls.
- [x] Verify local mutation checks reject missing sanitization, swapped restoration and forbidden forwarding.
- [x] Run unit tests, full Go race suite and repeatable harness checks; independently review the harness.
- [x] Update roadmap and provide invocation/limitations in a developer-facing guide.

## Constraints

Preserve production behavior. No external models, real infrastructure, user config changes, raw fixture output, or performance claims. All subprocesses have bounded shutdown. Failure and skipped coverage cannot report success. stdout contains counts/status only; local JSON reports contain metadata and safe assertion names, not bodies or values.

## Execution record

- Initial oracle tests failed before the runner existed; subsequent behavior regressions were observed failing before their fixes.
- Final local suite: 125 operations across one/four clients and bypass controls, zero failures; three actual-boundary fault injections rejected.
- Oracle/lifecycle unit tests: 13 passed. Full Go race suite: 17 packages passed.
- Review findings addressed: error/cancellation boundary checks, escaped-original detection, and cleanup of descendants of exited parents.
- Scope remains deterministic correctness. Resource/stage instrumentation, benchmark-tier populations, real OCR quality, W09/W10 and soak qualification are deferred explicitly in the manifest/report.
