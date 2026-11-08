---
hide:
  - toc
---

<div class="hero" markdown>

<p class="eyebrow">The cui-proxy operator guide</p>

# Real systems. Less exposure.

<p class="lead">A local, fail-closed proxy for Codex CLI that replaces sensitive data with synthetic values before it reaches the model and restores the originals in streamed responses, so your coding agent's tool calls still run against real systems.</p>

[Get started](quickstart.md){ .md-button .md-button--primary }
[Follow a request](architecture.md){ .md-button }

</div>

## Find your next step

<div class="grid cards" markdown>

- **Set up your first session**

    Install the binary, choose how to run it, connect Codex, and verify the route.

    [Quick start →](quickstart.md)

- **Work through a complete example**

    Use a fictional service to learn dictionaries, masking, strict blocking, and reloads.

    [End-to-end tutorial →](tutorial.md)

- **Configure for your use case**

    Download complete YAML files for everyday masking, selective blocking, strict inspection, and persistent mappings.

    [Configuration recipes →](recipes.md)

- **Inspect PDFs and images locally**

    Convert supported inline attachments to sanitized text and understand file-block diagnostics. Available from v0.4.0.

    [PDF and image inspection →](attachments.md)

- **Understand the dashboard**

    Read the counters, interpret a blocked request, and use Test a string to refine your rules locally.

    [Dashboard walkthrough →](dashboard.md)

- **Understand what each rule means**

    Learn `type`, `pattern`, `synth`, and `action`, and see exactly what the built-in bundle detects.

    [Rules and synthetic values →](rules.md)

- **See the technical design**

    Follow a request through guards, parsing, detection, the vault, audit, upstream forwarding, and response restoration.

    [Life of a request →](architecture.md)

</div>

## What the proxy does

Codex sends supported model requests to a loopback listener on your machine.
With the default `sanitize` policy, matching values become synthetic stand-ins:
an address becomes a documentation-range address, a dictionary hostname becomes
an `.example` name, and an opaque token becomes a `REDACTED-…` value. A local
vault remembers the mapping. Supported streamed response text and tool arguments
are restored before Codex uses them.

With `strict`, a detected value stops the entire request. You can also mask most
values while blocking selected rules. Neither policy improves what the detectors
can recognize; your dictionaries and regex rules extend their coverage.

| Component | Job | Default location |
|---|---|---|
| Codex | Sends requests and executes approved local tools | Your client |
| Proxy | Inspects, masks or blocks; restores streamed responses | Loopback port `8787` |
| Dashboard | Shows activity/configuration and tests text locally | Loopback port `8788` |
| YAML and dictionaries | Define your policy and sensitive-name coverage | `~/.config/cui-proxy/` |
| Vault | Stores real ↔ synthetic mappings | Memory by default; optional encrypted file |
| Decision log | Records decisions and rule counts without request content | `~/.local/state/cui-proxy/decisions.jsonl` |

!!! warning "Understand the boundary"
    Detection is pattern- and dictionary-based. Unmatched content can leave the
    machine. Credentials pass through, some protocol fields have inspection
    exceptions, and unsupported payloads are refused. This is not a DLP product
    or a compliance approval. Read [capabilities and limitations](limitations.md)
    before using it with sensitive material; CUI use still requires the relevant
    GOVSEC / ISSM approval.

## About this manual

These guides describe **v0.5.0**, including [PDF/image text extraction](attachments.md)
and the menu bar's [offline manual](menubar.md#offline-documentation).
Use the version selector in the header to read the guide for a different
release: `latest` is the newest release and `dev` follows the main branch. The
menu-bar app's **Online Documentation** item opens the pages for the version
you installed. Commands use fabricated values and the default configuration
directory unless stated otherwise. Downloadable recipes are checked by the real CLI on every
documentation build. [Dated backend verification](verification.md) records the
specific client and model tested; repeat the checks after upgrades.

For a question or reproducible bug, use [GitHub issues](https://github.com/rozdolsky33/cui-proxy/issues)
with fabricated input. Report bypasses privately using the
[security policy](https://github.com/rozdolsky33/cui-proxy/security/policy).
