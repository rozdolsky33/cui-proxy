# Task 12: isolated packaging, upgrade and rollback verification

The macOS arm64 upgrade/rollback check passed against clean source `fda2e88ff4f680fcf95a32372ca60fe45109c1c1`. This is functional release evidence, not performance qualification. No installed proxy, user configuration, vault or key was changed.

## Artifacts and identity

The baseline was the published `v0.6.0` macOS arm64 archive, verified against its published checksum. The candidate was built with the existing packaging script as `v0.6.1-qualification.fda2e88`; this is a private verification label, not a published release. Packaging completed for all four supported target combinations. Only the macOS arm64 package was executed in this check.

| Artifact | SHA-256 |
| --- | --- |
| Baseline archive | `16c5bf694d31bb5b17ebe55567d83747c89e597bb0130309659f11a2279ffae8` |
| Baseline executable | `1d8cc4b0ed4e68fe8635df187c1d83551038b10c00a8a8a1a2e9867f23733418` |
| Candidate archive | `e9a5460a8dfe013b4528baebff7f2d57868199c005c6af6ae498cda7caf8d540` |
| Candidate executable | `e30f94657d7f18e0e6095f95ad5ead619dc5ab0e39358d77a058459268b77e09` |
| Native verification report | `aeec2eb692d015fe77bef62bb8fc88039d8233f66c0157cbcd82aa79f4bbd904` |
| Private operator probe | `ea6af1f303f543d6208e020884fe12a4f9f53d5fb5dcab117eac5e9769f95e4e` |

The archive checksum and safe extraction paths were verified before execution. Each stage checked its running process executable path and binary hash, rather than trusting the shell PATH. Candidate version output matched preflight. Effective policy was checked privately at each stage.

The Go 1.26.11 toolchain did not embed a VCS revision when building from this linked Git worktree, including an explicit VCS-stamping diagnostic build. Candidate preflight therefore reported an unknown revision. Source identity was independently recorded from the clean checkout and tied to the package and executable hashes above; this check does not claim that preflight supplied the commit. Operators should retain independent source/build evidence when the embedded revision is unavailable.

## History and vault checks

The probe reused the existing correctness harness with private loopback processes, synthetic conversation histories, and one temporary file vault/key pair. Five semantic operations covered the following sequence:

1. The baseline created a history with two mappings and stopped gracefully.
2. The candidate reopened that vault, restored the baseline history, and created a second history. Independent decrypted inspection found four mappings and preserved the original mapping/token pairs.
3. The baseline binary reopened the candidate-written vault after rollback and restored both histories. All four mappings remained unchanged.

The key hash stayed unchanged throughout. The dashboard vault count is updated after a request; reopened population was therefore checked after history replay, not immediately after process startup. Persisted vault inspection independently checked mapping identity.

Candidate dependency preflight returned HTTP 200 and ready. Baseline and rollback `v0.6.0` returned HTTP 404 for that newer endpoint; this is recorded as an unavailable endpoint, not a passing baseline preflight. Running executable identity and effective policy were verified for all three stages.

All owned processes stopped and private runtime cleanup passed. Privacy verification checked nine retained files against eight protected values. Detailed reports, effective configurations, logs and mapping inspection artifacts remain private under `/private/tmp/cui-task12-release-standalone-2/native-4`.

## Retained failures and scope

Earlier private attempts remain retained: a packaging label rejected by the version-format check, an incorrect harness-root invocation, a premature assertion on the request-updated vault metric, and an assertion that assumed embedded VCS metadata was available in a linked-worktree build. The final run corrected those operator-probe assumptions and passed; no failed artifact was relabeled successful.

This proves compatibility for the tested baseline/candidate pair and synthetic histories on this host. It does not establish compatibility with every historical release, execute the other packaged platforms, exercise a paid model backend, or qualify the performance envelope. The separate 1 MiB fairness repetition failed its original 150 ms added first-byte p95 threshold at 153.052 ms; the remaining performance matrix and full soak remain gated pending investigation.
