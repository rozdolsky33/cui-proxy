# Task 11 Regexp Preflight Implementation Plan

> Use superpowers:executing-plans inline with one fresh whole-branch review.

**Goal:** remove unnecessary whole-input regexp searches without changing detection behavior, then reassess the known mixed-text latency gap.
**Architecture:** optional immutable anchored validators plus bounded literal-prefix candidates for eligible custom regexes; reference fallback for every unsupported condition.
**Tech Stack:** Go 1.26 regexp/syntax; existing captured-output benchmarks and HTTP runners.
**Spec:** docs/superpowers/specs/2026-11-13-task11-regexp-preflight.md

## Global Constraints
Pattern <=4 KiB; syntax nodes <=128; compiled instructions <=4,096; finite width <=256 bytes; ASCII inputs >=4 KiB; at most32 candidate starts. No anchors, unbounded patterns, group detectors or non-ASCII optimized input. No input cache, policy/audit/durability changes or gate waivers. Capture fixtures/runner/report output privately. Main stays clean/current. Preserve Task 08 and sustained qualification status until original acceptance evidence exists.

## Review Focus
1. Candidate rejection or overlap cannot hide a later reference match or alter leftmost non-overlap behavior.
2. Boundary context at beginning/end/newline/NUL cannot create or drop matches; unsupported anchors and Unicode fall back.
3. Captures, validators, blocking wrappers and stable overlap resolution retain exact original offsets and policy tags.
4. Dense/adversarial prefixes and large patterns stay within explicit work/metadata limits and use reference fallback safely.
5. Evidence distinguishes a short optimization preflight from full latency/throughput qualification and the four-hour soak.

### Task 1: Bounded matcher and semantic proof
Files: internal/detect/bounded.go, bounded_test.go, regex.go; existing benchmark diagnostic.
Interfaces: newBoundedSearch(re *regexp.Regexp) *boundedSearch returns nil when ineligible; (*boundedSearch).find(s string) (matches [][]int, used bool), where used=false requires unchanged reference search. regexDetector stores optional helper only for custom patterns.
- [x] Add eligibility, boundary/capture/validator and dense-fallback tests against referenceFind; observe RED.
- [x] Implement conservative syntax width/anchor/node/program checks and immutable anchored validators; use fixed32-position candidate storage and ASCII/size checks.
- [x] Integrate non-group custom Find with reference fallback. Run captured go test -race ./internal/detect ./internal/transform ./internal/adapter/codex ./internal/proxy; expected exit zero.
- [x] Add/run differential fuzzing over forced-ASCII optimized input and separate Unicode/invalid-input fallback; expected no mismatches. Commit.

### Task 2: Paired performance acceptance
Files: benchmark controls, existing workflow runners; private evidence and aggregate report.
Interfaces: reference binary/source main62445ed, candidate Task1; identical fixture and workload seeds.
- [ ] Run alternating paired detection/request-walk benchmarks, eight repetitions, including dense/short/unsupported/non-ASCII controls; report variation and allocation.
- [ ] Run three paired normal text/fairness diagnostics at256/768/1024KiB with four clients and20 measured text requests per arm/size/repetition. Preserve outcomes, stage/resource data and unmet gates.
- [ ] Accept only evidence-backed improvement without material regression; otherwise retain reference implementation and record why. No full qualification claim from these sample counts.
- [ ] Run semantic125-operation HTTP corpus and affected race tests; expected all zero. Commit safe evidence.

### Task 3: Review, integration and next acceptance phase
Files: roadmap, verification report, Task10 integration record and pending Task12 operator/CI follow-ups.
- [ ] Record final behavior, measurements and limitations; update Task10 as integrated PR44, retain its deferred startup-message wording and unconfirmed CI interruption-cleanup incident for Task12.
- [ ] Run full Go race, Python workflow, vet/staticcheck and manual checks with captured output; expected all zero.
- [ ] Fresh whole-branch review, one material-fix pass with RED/GREEN; archive rulings and defer minors.
- [ ] Push/PR, require exact-head CI/security/packaging, merge and sync clean main. Continue with full Task11 workload/soak implementation and acceptance; do not mark Task11 done here.
