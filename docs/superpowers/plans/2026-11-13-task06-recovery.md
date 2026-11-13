# Task 06 Recovery Implementation Plan

> For agentic workers: use superpowers:executing-plans for inline implementation.

Goal: actionable refusals and dependency checks in the running service.
Architecture: additive recovery metadata; bounded per-handler recurrence; guarded
service preflight consumed by CLI/dashboard. Tech stack: Go, existing JS dashboard.
Spec: docs/superpowers/specs/2026-11-13-task06-recovery.md

## Global Constraints

Preserve HTTP statuses and existing error codes/messages. No policy weakening,
context deletion, raw content persistence or content fingerprints. Capture test
output privately. Use standing authorization for review, commits, PR and merge.

## Review Focus

1. Concurrent clients must not create unbounded recurrence state or claim identity.
2. Unknown diagnostic input must not echo arbitrary values in guidance.
3. Service and shell PATH/language differences must not produce false readiness.
4. Dependency probes must terminate on cancellation and cap output/concurrency.
5. Existing error fields and security guards must survive additive metadata.

### Task 1: Recovery guidance and recurrence

Files: internal/recovery/recovery.go and tests; internal/proxy/recovery_test.go;
internal/proxy/proxy.go; cmd/cui-proxy/auditcmd.go; attachment/errors.go, extract.go.
Interfaces: recovery.Lookup(reason,kind,detail string) Guidance;
Tracker.Record(code string,now time.Time) uint32, closed deterministic codes only.
- [ ] Write tests for category/retry/remedy, unknown input, recurrence expiry and
  concurrent bounds, repeated unsupported images with zero upstream calls.
- [ ] Observe RED; implement additive error.recovery and recovery headers while
  retaining existing message/code/type/status. Distinguish extraction queue timeout.
- [ ] Add explain guidance using persisted reason/detail, never infer recurrence.
- [ ] Run affected Go packages; expected PASS; commit.

### Task 2: Running-service preflight

Files: internal/attachment/preflight.go and tests; internal/preflight/preflight.go
and tests; internal/ui/preflight_test.go; internal/ui/ui.go;
cmd/cui-proxy/preflight.go and tests; cmd/cui-proxy/main.go.
Interfaces: attachment.CheckTools(ctx) []ToolCheck; preflight.Report;
preflight.New(version string) *Checker; Checker.Check(ctx,policy) Report.
- [ ] Tests: missing executable, missing English, bounded output/time, safe output,
  effective policy, concurrent cached probes, loopback guard and CLI service result.
- [ ] Observe RED then implement probes with the extractor environment, two-second
  deadline and bounded output; cache results ten seconds with one probe at a time.
- [ ] CLI queries service, never falls back to local discovery or follows redirects.
- [ ] Run affected packages; expected PASS; commit.

### Task 3: Dashboard, recovery documentation and integration

Files: internal/ui/static/index.html, app.js; docs/block-diagnostics.md,
docs/attachments.md; roadmap and verification results; bundled manual.
- [ ] Add preflight action/status using textContent and fixed field presentation.
- [ ] Document exact recovery actions, advisory retry metadata and client limits.
- [ ] Verify UI, regenerate manual, full Go race/Python/vet/staticcheck checks.
- [ ] Fresh whole-branch review, material fix pass, PR checks and merge.
- [ ] Synchronize clean primary main. Expected all required gates PASS.
