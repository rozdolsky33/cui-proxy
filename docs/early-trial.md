# Text-only use: v0.7.0

v0.7.1 is the current patch release of v0.7.0. It fixes three defects and changes no setting. This release is available on the stable installation channel for supervised text and code workflows. It is not a production-readiness claim, a supported four-agent operating envelope, or a promise of equivalent model reasoning. Multi-agent performance qualification continues separately.

## Install with Homebrew or a pinned installer

Install or upgrade through Homebrew:

```sh
brew install rozdolsky33/tap/cui-proxy
# For an existing Homebrew installation:
brew update && brew upgrade cui-proxy
cui-proxy version
```

Confirm the installed version is `v0.7.1`. If an older proxy process is still running, drain its active requests and restart it with the intended binary and preserved configuration/vault/key; upgrading a formula does not necessarily replace a running process.

Alternatively, download the pinned installer, inspect it, and install into a separate directory so the current installation remains available:

```sh
curl -fsSL https://raw.githubusercontent.com/rozdolsky33/cui-proxy/v0.7.1/install.sh -o cui-proxy-trial-install.sh
CUI_PROXY_VERSION=v0.7.1 INSTALL_DIR="$HOME/.local/cui-proxy-trial" sh cui-proxy-trial-install.sh
"$HOME/.local/cui-proxy-trial/cui-proxy" version
```

Read the downloaded script before running it. The installer verifies the selected archive against the release checksums. Archives cover macOS and Linux on amd64 and arm64; this does not mean every operating system has completed a native trial. The installed binary also contains the offline manual.

Do not start a second proxy against a vault already owned by a running process. For an existing installation, follow [upgrade and rollback](release-readiness.md#upgrade-and-rollback-with-active-histories). Preserve a consistent vault and key together before changing versions. A separate installation directory does not create a separate vault or configuration automatically.

## Required trial policy

In the configuration you intend to use, set:

```yaml
policy:
  attachments: block
  on_error: block
```

The distributed example already uses these values. An upgrade does not overwrite an existing configuration: check your effective settings explicitly. Keep your existing detection rules and dictionaries; do not replace your entire configuration with this policy fragment.

Validate the complete configuration with `cui-proxy check-config -config /path/to/config.yaml`, using the trial binary's absolute path if another version is on PATH. A valid configuration is not proof of complete protection or performance qualification.

Requests carrying recognized image or file attachments are refused rather than sent without those attachments. Screenshots, image-producing tools and attachment-dependent agent steps can therefore fail closed. Plain text tool calls and tool results remain subject to the normal protocol and policy checks. This mode does not provide image understanding, OCR or PDF extraction, and does not make every possible tool compatible.

## Start with a bounded trial

1. Begin with one cooperating agent on reversible text/code tasks; inspect failures before expanding usage.
2. Use synthetic checks to verify sanitization, restoration and expected attachment refusal. Do not paste real protected data or raw fixture output into assistant sessions or issue reports.
3. Keep request/audit summaries value-free and preserve request IDs locally for diagnosis. A blocked request is not permission to disable the blocking rule or switch to forwarding on error.
4. Stop and investigate unexpected restoration, persistence or policy behavior. Preserve the original vault/key and failed evidence; do not delete state to force startup.

## What passed, and what remains open

Repository correctness checks, race tests, differential decoder fuzzing, static analysis, independent review and PR CI passed for the merged decoder change. Its original artifact passed all 36 scheduled text tests. A standalone rebuild corrected a Go VCS-stamping mismatch without changing the application source, and passed native cancellation/cleanup verification.

However, the corrected artifact's first four-client 1 MiB text repetition measured **151.50 ms p95 added first-byte latency**, above the unchanged **150 ms** target. This is a text-only performance failure: blocking extraction does not resolve it. The successful and failed runs remain distinct; the release is not advertised as performance-qualified. The release-packaged binaries also have their own build identities, rather than inheriting benchmark results by name.

The sustained paired soak has not passed. Continuous unattended operation and four-agent efficiency remain unqualified; eight/sixteen-client measurements are capacity exploration only. Live-model task quality remains unmeasured. See [release readiness](release-readiness.md) and [protocol support](protocol-support.md) for the full boundaries.

## Upgrade, rollback and feedback

Drain active requests before switching executables. Confirm which binary the actual running process uses, especially with the menu-bar helper. Rollback requires compatible configuration and a vault/key containing all mappings needed by active histories; a stale backup can lose newer mappings. See [vault recovery](vault-recovery.md).

For ordinary bug reports, provide version, OS/architecture, a short synthetic reproduction, rule names/counts and error class. Exclude payloads, extracted text, replacements, keys and raw session identifiers. Report suspected security bypasses through the repository's private vulnerability-reporting process described in SECURITY.md.
