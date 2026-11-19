# Isolate Codex to a project

Use Codex's filesystem sandbox to limit local file access alongside cui-proxy's
inspection of model traffic. Changing the working directory, opening a folder,
or writing an `AGENTS.md` instruction does not enforce a filesystem boundary.
The proxy does not prevent local file reads or sandbox escapes.

## Basic workspace write protection

For ordinary development with the Codex CLI on macOS:

```sh
cd ~/projects/my-app
codex --sandbox workspace-write --ask-for-approval on-request
```

This permits edits in the workspace and traversal of nested directories, with
some protected paths and temporary-directory exceptions. **It restricts writes;
it does not restrict all reads outside the project.** Commands requiring broader
access can request approval. Check `/status` and `/permissions` in the CLI.
See OpenAI's [approval and sandbox guide](https://learn.chatgpt.com/docs/agent-approvals-security).

Keep the provider setup from [Connect Codex](codex.md): sandbox options do not
select cui-proxy or change its sanitization policy.

## Restrict reads as well as writes

For a project boundary, use a custom permission profile on a client that supports
it. Permission profiles are currently beta. Back up the **user-level**
`~/.codex/config.toml` (or `$CODEX_HOME/config.toml`) and merge this partial
configuration, retaining your existing provider and model settings:

```toml title="Codex config.toml — partial configuration"
# Top-level keys must precede every table header.
default_permissions = "project-only"
approval_policy = "on-request"

[permissions.project-only]
extends = ":workspace"

[permissions.project-only.filesystem]
":root" = "deny"
":minimal" = "read"
":tmpdir" = "deny"
":slash_tmp" = "deny"

[permissions.project-only.filesystem.":workspace_roots"]
"." = "write"

[permissions.project-only.network]
enabled = false
```

This follows OpenAI's [workspace-only profile example](https://learn.chatgpt.com/docs/permissions#file-access-limited-to-workspace).
The intended access for sandboxed commands is:

| Location or capability | Intended access |
|---|---|
| Active project and ordinary descendants | Read, create, edit and delete |
| Parent and sibling projects | Denied, apart from runtime exceptions |
| Home files, SSH and cloud credentials outside the project | Denied, apart from other effective grants |
| Minimal OS/runtime paths selected by Codex | Read |
| System temporary directories | Denied |
| Command network access | Disabled |

The `:minimal` exception means this is not literally “no access to anything
outside the folder.” Inheriting `:workspace` also preserves read-only safeguards
for root-level `.git`, `.agents`, `.codex` and `.aws` paths. Project-local
credentials elsewhere remain readable unless separately denied or removed.
See the [profile inheritance rules](https://learn.chatgpt.com/docs/permissions#extend-a-profile).

!!! warning "Do not mix permission profiles with legacy sandbox options"
    Remove conflicting `sandbox_mode` and `[sandbox_workspace_write]` settings
    from applicable configuration layers and selected Codex profiles. Do not
    pass `--sandbox` or `--full-auto` when relying on `project-only`: legacy
    sandbox selection can override `default_permissions`. Managed requirements
    can further constrain the available profiles. Follow OpenAI's
    [migration guidance](https://learn.chatgpt.com/docs/permissions#migrate-from-older-sandbox-settings).

Restart Codex in the project with the custom profile and existing proxy provider:

```sh
cd ~/projects/my-app
codex --ask-for-approval on-request
```

If you use the named `cui` provider profile from [Connect Codex](codex.md#use-a-named-codex-profile),
append `--profile cui` after checking it has no legacy sandbox overrides.
This provider profile is distinct from the `project-only` permission profile.

## Check the effective boundary

Check the active permission profile and workspace roots after restart. Review
every root: `:workspace_roots` applies to all effective roots, so adding a parent
directory, another project or an extra writable directory can broaden access.

Use disposable files containing harmless text to verify the boundary through
Codex's sandboxed shell before using sensitive data. Create the test files
yourself in a terminal, then ask Codex to run these checks without escalation:

| Check | Expected result |
|---|---|
| Read, create, edit and delete a disposable file in a nested project directory | Allowed |
| Read a disposable file in a sibling directory using both `../` and its absolute path | Denied |
| Create a disposable file in that sibling directory | Denied |
| Read the sibling file through a symlink you created inside the project | Denied |
| Run a command that opens a network connection | Denied |

Use exit status to assess each check; do not print credentials or other sensitive
content. If an outside read succeeds, stop and review the active profile,
configuration layers, extra roots and execution surface. An approval prompt is
not a denial test: decline it while validating the sandbox. Repeat after client
upgrades or permission changes. A successful test only establishes the tested
client, platform and tool path.

Temporary files, dependency caches and tools installed outside runtime paths may
need explicit exceptions. Prefer project-local scratch/cache directories; grant
only necessary external paths and repeat the boundary checks. Git writes may
require approval because `.git` is protected. Do not disable the sandbox to make
a build work.

## Desktop app and scope limits

Open the intended project and select `project-only` in the permissions selector
if your desktop client exposes custom profiles. Restart after configuration
changes and verify the actual roots and behavior using the same disposable
checks. Do not assume the CLI and desktop discover identical workspace roots.
If your client cannot select or enforce the profile, use a supported client or
an isolated VM with only the project made available.

The profile governs sandboxed local commands. Browser tools, connectors, MCP
servers and approved escalations have separate controls. Keep approvals enabled
and review each request: approving outside access changes the boundary for that
action. For an invariant that outside access must never occur, enforce it at the
host or VM level too and expose only the needed files and tools. See OpenAI's
[scope and enforcement guidance](https://learn.chatgpt.com/docs/permissions#scope-and-enforcement).

Disabling command networking does not disable Codex's own model connection to
cui-proxy. Keep the proxy running separately, then [verify routing](testing.md)
as well as isolation. The [proxy's security guarantees](assurance.md) describe
what happens to supported model traffic; they do not replace these local access
controls.
