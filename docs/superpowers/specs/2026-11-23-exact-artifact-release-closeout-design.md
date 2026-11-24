# Exact-artifact native release closeout

The remaining release evidence must exercise the same proper versioned artifact
as the long qualification run. A development c8 build, a source-equivalent
rebuild, historical Task 12 evidence, and stock packaging CI cannot substitute
for executing those retained release bytes. Candidate identity is therefore an
explicit expected SHA-256, source commit, version, build-manifest digest, and
archive digest. It is not hardcoded to c8.

The parent selected a proper v0.7.4 release artifact before the remaining matrix.
The interrupted/development runs remain partial and unqualified. Future Python,
documentation, or evaluator commits do not authorize rebuilding the selected
artifact. The parent supplies the frozen candidate manifest and retained paths.

## Scope and execution constraints

This probe complements existing stock packaging CI and historical upgrade and
rollback evidence. It establishes archive/extracted-byte identity and a native
private foreground upgrade/rollback sequence. It does not run `package.sh`,
`install.sh`, launchctl, the installed service, or any global configuration.
Neither real extraction tools nor user vaults/keys are modified.

All work is statically prepared in the isolated release-closeout worktree.
During the ongoing qualification lock, do not import workflow modules, execute
tests, probes, builds, downloads, profiling, native escalation, commits, or pushes.
Tests and a preimplementation baseline are preserved for parent-controlled
execution after timing ends and the reference lock is released.

## Archive scope

The stock CLI member layout is `cui-proxy`, `LICENSE`, `README.md`, and an
`examples` directory containing `config.yaml` and `hosts.txt`. The stock script
rebuilds the binary and supplies version flags, so copying an existing binary
into that layout must never be labelled execution of the stock builder.

If the parent supplies the actual versioned release archive, the probe verifies
and extracts that archive unchanged. The extracted binary must match the frozen
runtime binary's expected digest. Nonbinary members are checked against the
declared candidate source snapshot. Archive and both binary digests are checked
again after native execution.

A separately requested fallback may create an **exact-byte verification
archive** from retained binary bytes and the stock member layout. Its report
explicitly says the stock package builder was not run. The fallback is never
silently substituted for a supplied release asset.

Archive reading is bounded and rejects duplicate members, traversal, absolute
paths, links, devices, unknown members, and excessive sizes. Extraction writes
only a newly owned private directory and never modifies source archive members.

## Baseline provenance

`--baseline-binary` and `--baseline-provenance` identify an external prior release.
The parent has confirmed metadata for the official v0.7.3 Darwin ARM64 asset;
availability metadata is not a download or native verification result.

The provenance record binds the baseline binary, source attribution when
available, release version, actual local release asset, and captured checksums
file. The helper verifies the local asset/checksum digests and that its CLI
member matches the supplied baseline binary. Download origin and release
attribution are supplied provenance, not a fabricated cryptographic signature.
Candidate, baseline, and probe-producer identities remain separate in output.

## Private native sequence

The probe reuses `harness.Environment` for real subprocess/backend handling,
`Environment.send` for its fixture-specific envelope and `/v1/responses` route,
`capture`, `decode_stream`, `check_operation`, `check_boundary`, and `vault_count`.
It does not invent a raw HTTP transport. Generic controlled fixtures are obtained
from the existing W01 recipe without printing their values. Existing upstream
captures contain request body and headers, but not response proof. A small
opt-in Harness response-proof hook is therefore required. It defaults off and
observes only bytes actually accepted by successful upstream body writes, in a
bounded memory-only buffer. After successful flush, it uses the existing stream
decoder and semantic oracle to compute a digest, then immediately clears the
buffer. Only completion, byte count, and digest metadata are retained, exposed
through `Environment.upstream_response_proof(ident)`. Raw upstream response bytes
are never written to files, logs, or reports. Partial writes, overflow, decode
failure, or flush failure produce no qualified proof and clear the buffer.

1. Acquire the existing global reference lock without replacing its inode. Bind
   candidate, baseline, archive, source, manifest, and clean producer identities
   before starting a private service.
2. Start the baseline binary in a new private environment: loopback ports,
   foreground proxy, config, key, and initially empty persistent vault. Use a
   controlled fail-closed text policy; extraction is blocked and no real OCR or
   PDF tool is invoked.
3. Run `check-config` and the running-service preflight, validating effective
   policy metadata, same child PID, and the expected running binary identity.
4. Send a real request introducing original A. Canonical response and captured
   upstream boundary checks establish sanitized alias A and vault population 1.
5. Gracefully stop the baseline with a successful exit. Copy a consistent
   private vault/key/config backup and record only hashes. Keep that backup
   unchanged throughout the upgrade and rollback.
6. Start the extracted candidate using the **same live vault and key paths**.
   Check config, preflight, PID and release identity. Submit only alias-A history,
   with no original A anywhere in its complete serialized body or headers,
   including fixture metadata, and require restoration to A with population
   still 1. Independently verify the upstream response contains alias A and no
   original A before client restoration, using the digest of the actual
   memory-only observed response. Introduce original B via a separate real request, capture
   alias B, and require population 2.
7. Gracefully stop the candidate. Record the latest postcandidate vault digest.
   Never copy the old backup over this vault.
8. Start the baseline against that exact latest vault/key. Before startup, verify
   the live vault matches the postcandidate digest. Require healthy preflight,
   then send **only aliases A and B**, with neither original in the request.
   Require canonical restoration of both and population still 2. An unsupported
   vault-format refusal is a failed rollback health result, never a passing
   safety exception.
9. Gracefully stop, validate serial audit decisions and metadata-only captures,
   scan final closed logs, verify source/archive/binary/key identities unchanged
   where required, stop all children, and remove private config/key/vault/backups.

The backend is a real local echo process. It cannot manufacture the expected
originals for alias-only requests. Canonical request/response and boundary
checks therefore prove old mappings persisted; originals are never reseeded on
restart or rollback. Native stages are serial, enabling exact audit-record
counts/order. Original introductions should be sanitized; alias-only
continuations should be forwarded with empty failure reason/detail.

## Interfaces and reports

All artifact interfaces are generic: require an expected candidate SHA-256,
source commit, version, source snapshot and build manifest. A supplied candidate
archive also requires its expected asset digest and is tested unchanged. The
retained candidate binary and extracted archive member must match exactly.
Future Python, documentation or evaluator commits do not trigger an artifact
rebuild or change the selected bytes.

For alias-only upgrade and rollback requests, both originals must be absent
from the entire serialized request body and every header, including fixture
metadata. The digest of actual memory-only observed upstream response bytes must
match the canonical expected aliases-only semantics, proving the relevant aliases
and neither original were emitted before client restoration. Expected originals
remain private local oracle inputs and never cross the upstream wire. No raw
response retention, even in private disk artifacts, is allowed.

The existing OCR identity API needs an optional `expected_binary_commit`
keyword and a `--binary-commit` CLI option, both defaulting to legacy c8 for
backward compatibility. Any preparation of that change occurs only in this new
producer worktree after stop confirmation and review; no frozen OCR producer is
edited and the selected release artifact is not rebuilt.

Planned modules are `release_archive.py` for bounded archive/provenance checks
and `release_closeout.py` for the private native lifecycle and CLI. Inputs include
retained candidate binary/archive/manifest, expected binary/archive digests,
candidate source commit/version/snapshot, external baseline binary/provenance,
clean producer source/commit, reference-lock path, and a new report path.

The report contains only closed stages/statuses, versions, counts, digests,
declared versus verified provenance, effective policy metadata, archive scope,
canonical/boundary/persistence results, and cleanup. It never includes fixture
values, aliases, raw request IDs, keys, config contents, raw responses or logs.
Failed private logs may be retained with the already reviewed OCR utility's
private failure-log policy; no raw key or vault is retained as public evidence.

The OCR identity API may gain an optional expected-binary-commit argument,
defaulting to legacy c8, plus a matching optional CLI flag. That enables a later
producer revision to check the new release without rebuilding it. No frozen OCR
producer or active source is modified during static preparation.

## Deferred execution inputs

Required closeout CLI options are `--candidate-binary`, `--candidate-sha256`,
`--candidate-source`, `--candidate-commit`, `--candidate-version`,
`--candidate-manifest`, `--baseline-binary`, `--baseline-provenance`,
`--producer-source`, `--producer-commit`, `--reference-lock`, and `--report`.
Select either `--candidate-archive` with `--candidate-archive-sha256`, or the
explicit fallback `--verification-archive` output; there is no silent fallback.
All candidate identities are explicit. The generic OCR flag is optional
`--binary-commit`, defaulting to its legacy c8 revision.

The baseline provenance is strict JSON with exactly `schema_version` (integer
1), `kind` (`operator_supplied_release_asset`), `version` (numeric semantic
version without a leading v), `release_tag` (v plus that version),
`source_commit` (full declared commit or null), `binary_sha256`, `asset`, and
`checksums`. The final two fields each have exactly `path`, `sha256`, and
`bytes`; paths are relative to the provenance file. The local checksums must
contain exactly one matching asset basename/hash. These are supplied release
attribution and verified local-byte bindings, not proof of a remote signature.

After parent coordination, run the three new unit modules with captured output
under the existing exclusive reference lock. The intended invocation is
`python3 -m unittest test_release_archive test_release_closeout test_upstream_response_proof`
from the producer's `scripts/workflow` directory. No such invocation has run
during static preparation. The live helper acquires the existing lock itself;
the parent must delegate ownership rather than hold a second conflicting lock
around its subprocess.

## Separate injected backend-delay evidence



Upstream-response proof is memory-only: raw response bytes must never be saved
to files, logs, reports or failure artifacts. The optional observer is bounded,
records actual successful body writes, requires successful flush and complete
stream decoding, emits only a semantic digest/byte count/completion metadata,
and clears its buffer on success, overflow and every failure path. Parent-side
comparison against alias-only expected semantics proves alias presence and
original absence without sending the originals to the backend. Missing or
overflowed proof fails the closeout. No new public HTTP protocol is introduced.

Source inventory found an existing real fault path, separate from the soak's
planned one-second backend latency. `observation.DelayedEnvironment.send`
injects `X-Harness-Delay-Ms: 80`; the Harness backend honors it with a controlled
sleep. `observation.run(binary)` verifies canonical W01 behavior, upstream-wait
attribution of at least 60 ms, and cleared gauges. It also covers separate
synthetic extraction-delay attribution. Normal `harness.main` invokes it.

Run that existing native harness on the selected retained release bytes and bind
its report to the actual binary and producer identities. Historical results do
not establish this new artifact's coverage. Do not present the synthetic
extraction attribution as native OCR recovery; that has its own live probe.
