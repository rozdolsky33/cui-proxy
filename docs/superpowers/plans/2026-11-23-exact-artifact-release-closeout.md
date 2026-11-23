# Exact Artifact Release Closeout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Verify archive identity and native private upgrade/rollback on exactly the retained release bytes selected for the long qualification run.

**Architecture:** Reuse the existing Harness protocol, canonical response/boundary oracles, private process lifecycle, and reviewed privacy/lock utilities. Separate bounded archive/provenance handling from the serial foreground service lifecycle; keep baseline, candidate, and producer identities distinct.

**Tech Stack:** Python standard library, existing workflow Harness/oracles, supplied native release binaries and local release assets.

**Spec:** `docs/superpowers/specs/2026-11-13-task12-release-evidence.md` and `docs/superpowers/specs/2026-11-23-exact-artifact-release-closeout-design.md`.

## Global Constraints

- No imports of workflow modules, tests, probes, builds, downloads, profiling, native escalation, commits or pushes while the ongoing timing lock is held.
- Do not alter frozen artifacts/sources, ongoing evidence, primary checkout, growth worktree or frozen OCR producer.
- Never use the installed service, global config, launchctl, install.sh, user vault/key, or real extraction-tool mutation.
- Do not rebuild the selected release when later Python/docs/evaluator changes land.
- Preserve an honest packaging scope: actual supplied release asset versus explicitly requested exact-byte verification archive; neither means this helper executed package.sh.
- Unsupported rollback vault format is failed health, never a pass waiver.
- Keep all raw fixtures/config/logs/requests/aliases locally captured and out of tool output and reports.

## Review Focus

- An archive member or extracted executable whose hash differs from the retained runtime must fail before execution.
- Path traversal, links, extra members and excessive archive sizes must never escape the private extraction root.
- Alias-only upgrade/rollback requests must contain no original that could recreate a mapping.
- Rollback must use the latest candidate-written vault, not the preupgrade backup.
- Missing canonical, provenance, audit, health or cleanup proof must prevent a passing report.

### Task 1: Archive and external baseline provenance

**Files:**
- Create: `scripts/workflow/release_archive.py`
- Test: `scripts/workflow/test_release_archive.py`

**Interfaces:**
- A bounded archive verifier/extractor accepts a supplied asset or an explicitly selected exact-byte fallback, expected candidate binary/archive hashes, and source snapshot.
- Baseline provenance verification accepts a supplied baseline binary plus local asset/checksum evidence; no download occurs in the helper.

- [x] Inventory actual stock CLI member layout and confirm package.sh rebuilds/version-stamps.
- [x] Obtain independent static review of this design before broad helper code.
- [x] Write tests for exact hashes/layout, explicit archive scope, traversal/link/duplicate/oversize rejection, and external baseline asset/checksum binding.
- [x] Preserve preimplementation source/test baseline and manifests without running it.
- [x] Prepare the minimal helper; all execution remains pending parent coordination.

### Task 2: Same-vault native foreground upgrade and rollback

**Files:**
- Create: `scripts/workflow/release_closeout.py`
- Test: `scripts/workflow/test_release_closeout.py`
- Modify: `scripts/workflow/harness.py` only for an opt-in upstream response capture API, default off.

**Interfaces:**
- Private environment uses existing Harness send/capture/canonical APIs with fixture metadata and `/v1/responses` routing.
- Lifecycle accepts generic expected candidate SHA/source commit/version/manifest, supplied baseline binary/provenance, clean producer identity, existing lock and new report.
- Separate stage functions check config, running preflight/PID identity, canonical requests, alias-only persistence, graceful stop, immutable backup, latest-vault rollback and final privacy/cleanup.
- Optional Harness response proof observes actual successful writes in bounded RAM only, retains metadata, then clears the buffer; alias-only requests exclude originals from the entire body and all headers/metadata, while decoded upstream responses prove aliases and exclude originals before restoration.

- [x] Inventory continuation/restart/canonical APIs and distinguish Harness transport from performance transport.
- [x] Write tests for originals-absent continuations, latest-vault enforcement, stage health failure, canonical/audit/privacy/cleanup fail-closed behavior and identity separation.
- [x] Preserve test-first baseline before preparing helper implementation.
- [x] Prepare native sequence statically; reuse reviewed global-lock and final captured-log utilities.
- [x] After parent stop confirmation, prepare optional generic expected-commit OCR API/CLI with legacy c8 default and regression tests in this new worktree only.
- [x] Independent static code review cleared Critical/Important findings; no execution was performed.
- [x] Independent static review cleared Critical/Important findings. Parent retained baseline red (three missing-feature import errors), corrected a test-only macOS path alias, and ran 16 targeted plus 867 full workflow tests (four skips).
- [ ] Run the real selected-artifact upgrade/rollback sequence under the global lock.

### Task 3: Reuse separate backend-delay observation

**Files:**
- Existing: `scripts/workflow/observation.py` and `scripts/workflow/harness.py`

- [x] Confirm source-backed injected 80 ms delay and at least 60 ms upstream attribution, separate from planned soak latency.
- [x] Parent executed the frozen F0 native harness against retained 0.7.4 bytes; the suite passed and the binary hash remained unchanged.
- [x] Native evidence is bound to the new release binary; historical c8 results are not transferred.
- [ ] Freeze and integrate producers only after tests/review; no qualification or release-complete claim before real execution evidence.
