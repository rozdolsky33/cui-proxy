# Configuration recipes

Choose the behavior you need, download a **complete YAML file**, and put the
dictionary beside it. These examples use invented names. Review the rules for
your environment before relying on them.

| Goal | Configuration | Dictionary files | What a match does |
|---|---|---|---|
| Mask common values and your service names | [sanitize.yaml](examples/sanitize.yaml) | [hosts.txt](examples/hosts.txt) | Replace and restore |
| Refuse any detected value | [strict.yaml](examples/strict.yaml) | [hosts.txt](examples/hosts.txt) | Block the entire request |
| Mask ordinary values, block restricted categories | [selective-block.yaml](examples/selective-block.yaml) | [hosts.txt](examples/hosts.txt), [restricted-names.txt](examples/restricted-names.txt) | Per-rule behavior |
| Extract PDF/image text before masking (from v0.4.0) | [attachment-text.yaml](examples/attachment-text.yaml) | [hosts.txt](examples/hosts.txt) | Replace files with extracted text, then sanitize matches |
| Keep mappings across restarts | [persistent-vault.yaml](examples/persistent-vault.yaml) | [hosts.txt](examples/hosts.txt) | Replace and restore using an encrypted file vault |
| Learn on separate ports | [tutorial.yaml](examples/tutorial.yaml) | [hosts.txt](examples/hosts.txt) | Mask on ports 18787 / 18788 |

## Download and validate a recipe

This creates a private new directory and does not replace your running config:

```sh
cui_recipe_dir=$(mktemp -d "${TMPDIR:-/tmp}/cui-recipe.XXXXXX")
curl -fsSL https://rozdolsky33.github.io/cui-proxy/examples/sanitize.yaml \
  -o "$cui_recipe_dir/config.yaml"
curl -fsSL https://rozdolsky33.github.io/cui-proxy/examples/hosts.txt \
  -o "$cui_recipe_dir/hosts.txt"
chmod 600 "$cui_recipe_dir/config.yaml" "$cui_recipe_dir/hosts.txt"
cui-proxy check-config -config "$cui_recipe_dir/config.yaml"
printf '%s\n' 'Check db.demo.example for DEMO-123456' |
  cui-proxy test-rules -config "$cui_recipe_dir/config.yaml"
```

For another recipe, change the downloaded YAML filename and fetch any additional
dictionary in the table. Relative paths resolve against the YAML's directory,
regardless of the current terminal directory. YAML does not expand `~` or
environment variables and does not merge multiple files.

All recipes except the tutorial use the normal ports. Stop an existing proxy before
serving one, or give it unique listener ports, vault files and audit files.
`check-config` and `test-rules` do not start a listener. For a permanent setup,
move the reviewed files to a private directory you intend to keep and explicitly
select that YAML with `serve -config` or the menu-bar installer.

All recipes enable audit and set `audit.on_error: block`. The generated
`cui-proxy init` starter instead defaults to warning on an audit-write failure
under sanitize. This choice is explicit so a recipe cannot silently forward a
request without its decision record.

## Local PDF/image text extraction (from v0.4.0)

This requires the v0.4.0 or newer plus local extraction tools. Read
[attachment inspection](attachments.md) before enabling it. Original files and
filenames are dropped; only extracted text is inspected and forwarded. Strict or
explicit block rules still apply to that text. All conversion-mode failures block.

```yaml title="attachment-text.yaml — complete configuration"
--8<-- "examples/attachment-text.yaml"
```

## Everyday masking

Use this when detected values may be represented by stand-ins. It enables the
built-in bundle, service names, an extra AWS-key regex, and a fictional ticket
pattern. A `type: builtin` rule is required to enable the bundle; it is not
automatically added to arbitrary YAML.

```yaml title="sanitize.yaml — complete file"
--8<-- "examples/sanitize.yaml"
```

```text title="hosts.txt"
--8<-- "examples/hosts.txt"
```

Expected previews:

| Input | Result |
|---|---|
| `db.demo.example` | Synthetic `.example` hostname; `service-hosts` match |
| `203.0.113.42` | Different address; `builtin.ipv4` match |
| `DEMO-123456` | Opaque stand-in; `ticket-ids` match |
| `DEMO-12345` | Unchanged: does not have six digits |
| `Explain a retry loop` | Unchanged: no matching value |

## Strict: refuse every detection

Use strict when your policy requires a request containing any detected value
to be refused. The whole request is blocked; the proxy does not send a redacted
version. Strict also broadens checks on protocol identifiers. Unrecognized
sensitive content is still outside pattern-based detection.

```yaml title="strict.yaml — complete file"
--8<-- "examples/strict.yaml"
```

Run the same `test-rules` examples with this file. A host, address or ticket match
returns an error and exit code 1. A clean phrase succeeds unchanged.
`policy check -require-strict -config PATH` verifies the profile is strict.

Strict requires at least one rule, `policy.on_error: block`, enabled audit,
and blocking audit-write failures. Do not add `action: sanitize`; it is invalid
under strict. See [Policy profiles](policy.md).

## Mask most values, block selected rules

Use `sanitize` at the top level and explicit `action: block` on the categories
you must refuse. This example masks service names and ticket IDs but blocks a
restricted name or an AWS access-key-shaped value.

```yaml title="selective-block.yaml — complete file"
--8<-- "examples/selective-block.yaml"
```

```text title="restricted-names.txt"
--8<-- "examples/restricted-names.txt"
```

`db.demo.example` is masked; `restricted-demo-project` blocks. If one request
contains both, the entire request blocks. A blocking rule wins even when its
match overlaps a sanitizing rule. Reordering rules cannot create an exception.

## Keep mappings across restarts

Use a file vault when a conversation must retain its synthetic mappings across
normal proxy restarts. The key and encrypted mapping file are both local.

```yaml title="persistent-vault.yaml — complete file"
--8<-- "examples/persistent-vault.yaml"
```

The proxy creates missing state directories/files with private permissions.
Keep the key and vault together in your private backup process; someone with
both can recover the mappings. Only one proxy may use this vault at a time.
Writes are periodic and on clean shutdown, so an abrupt crash can lose recent
mappings. Do not delete or rotate the key while expecting old mappings to work.

`test-rules` still uses a throwaway vault even when given this configuration.
It does not inspect or change the production file vault.

## Adapt a recipe for your environment

| Need | Change | Check |
|---|---|---|
| More hostnames | Add literal lines to `hosts.txt` | Positive and near-miss string tests, then reload |
| Usernames, paths or project names | Add a separate dictionary rule with `synth: secret` | Built-ins do not cover arbitrary names/paths |
| Your own identifier format | Add a Go regex with a suitable `synth` | Whole match is replaced; [rule anatomy](rules.md) |
| Different API/backend | Change upstream and corresponding Codex authentication | Restart; [Codex setup](codex.md) |
| A second isolated policy | Separate YAML, ports, audit file and file vault | Point that client at the correct local URL |
| Larger request bodies | Edit `policy.max_body_bytes` | Does not raise fixed metadata/argument-stream limits |

There is no per-value allowlist, built-in-category selector, per-request bypass,
or automatic per-repository policy selection. A dictionary entry is a detection
rule, not an approved destination. See [supported overrides](configuration.md#overrides-that-exist-and-ones-that-do-not)
before trying to narrow a rule.
