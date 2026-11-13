# Task 06: refusal recovery and service preflight

Preserve HTTP statuses and existing error codes/messages. Add structured recovery
metadata and fixed headers distinguishing policy, unsupported representation,
dependency, exhausted budget and transient failures. Every code carries fixed
remedy text and retry classification: after_change, after_repair or backoff.
No policy weakening or automatic history deletion is a remedy.

Track recurring deterministic failure categories in per-handler memory only,
using closed codes, a one-minute window and bounded counts. Counts describe
aggregate categories, not identical bodies or a particular client. Store no
bodies, content digests, rule values or reusable content fingerprints.

Add a guarded GET /api/preflight dashboard endpoint and a `preflight -config`
CLI command querying it. Report the running process build/version and effective
policy enums, tool availability and English OCR language availability. Check
executables with the service PATH and the same restricted child environment as
extraction. Bound execution, output, concurrent probes and cache lifetime. Do not
read image content, send upstream requests, expose tool output or alter policy.
Missing dependencies are actionable findings, not an opaque extraction error.

The dashboard offers an explicit preflight action and runs it on initial load.
Documentation explains how to run preflight before attachment workflows and how
to recover when a client retries deterministically rejected history. Metadata
is advisory: do not promise control of third-party client retries.

## Approach

Extend existing errors additively instead of changing status codes. Share a small
recovery vocabulary between HTTP and explain. Keep dependency checks in the
attachment package and reporting in a preflight package. Query the running
service rather than testing a shell environment that can differ from launchd.

## Acceptance

Repeated unsupported image requests yield fixed recovery instructions and an
aggregate recurrence count without upstream calls. Missing OCR and English data
are identified through service preflight. Existing security gates remain intact.
