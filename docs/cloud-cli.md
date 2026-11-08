# OCI and AWS CLI configuration rules

Use these rules when CLI configuration or command output may appear in Codex
prompts or tool results. They extend the existing AWS access-key rule with
coverage for selected OCI configuration fields and AWS credential assignments.
The proxy inspects supported model-request content; it does not watch, modify,
or automatically load `~/.oci/` or `~/.aws/`, or control local CLI execution.

## Know which file you are protecting

| Source | Typical contents | Coverage to configure |
|---|---|---|
| `~/.oci/config` | Profiles, `user` and `tenancy` OCIDs, `fingerprint`, `key_file`, `region`; optional `pass_phrase` and `security_token_file` | Built-ins for OCIDs; custom rules below for selected assignments; dictionary for sensitive profile names and regions |
| `~/.oci/oci_cli_rc` | CLI defaults, aliases, queries, compartment IDs and endpoints | Built-ins for OCIDs and addresses; dictionaries for sensitive names, endpoints and other defaults |
| OCI key and session-token files | Private key material or authentication tokens | Built-in private-key/JWT patterns cover recognized shapes, not every token format |
| `~/.aws/config` and `~/.aws/credentials` | Profiles, regions, access-key IDs, secret access keys and session tokens | Separate AWS key-ID rule plus assignment rules below; dictionaries for other sensitive values |

These are default paths; both CLIs support alternative locations. See Oracle's
[CLI configuration guide](https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/cliconfigure.htm),
[environment-variable reference](https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/clienvironmentvariables.htm)
and the [AWS configuration reference](https://docs.aws.amazon.com/cli/latest/topic/config-vars.html)
for their file formats and overrides.

`key_file` and `security_token_file` contain paths, not the referenced key or
token. Matching those paths does not inspect their target files. Likewise,
`fingerprint` is a public-key identifier, not a private key; mask it if your
policy treats infrastructure identifiers as sensitive.

## Start with a complete recipe

Download [cloud-cli.yaml](examples/cloud-cli.yaml) and
[hosts.txt](examples/hosts.txt) into a new private directory using the
[recipe download workflow](recipes.md#download-and-validate-a-recipe).
This is a **complete proxy configuration**, separate from either cloud CLI's
configuration. It masks matches under `sanitize` and keeps the built-in bundle,
AWS key-ID rule, service-name dictionary and tutorial ticket rule.

```yaml title="cloud-cli.yaml — complete configuration"
--8<-- "examples/cloud-cli.yaml"
```

For an existing proxy config, copy only the `oci-config-fields` and
`aws-credential-fields` entries into its current `rules` list. Retain your
existing rules, especially `type: builtin` and `aws-keys`; YAML files are not
merged automatically. Validate, preview and [reload](configuration.md#hot-reload)
after editing rules. Listener or storage changes require a restart.

The two custom patterns match **whole nonempty assignment lines**, including
the field name, optional `export`, spaces and value. Custom rules replace the
whole match, not just a capture group. This deliberately masks entire paths and
passphrases even when they contain spaces. It can also mask trailing comments.
Names are case-insensitive. The patterns support INI-style entries and selected
environment-variable assignments, for example:

```ini title="Fabricated OCI text — not an operational config"
[DEFAULT]
user=ocid1.user.oc1..demouseralpha
tenancy=ocid1.tenancy.oc1..demotenancyalpha
fingerprint=aa:bb:cc:dd:ee:ff:aa:bb:cc:dd:ee:ff:aa:bb:cc:dd
key_file=/demo/.oci/demo-key.pem
pass_phrase=demo passphrase only
security_token_file=/demo/.oci/sessions/demo/token
region=us-ashburn-1
```

The OCIDs are masked by `builtin.ocid`. Fingerprint, key path, passphrase and
token path assignments are masked by `oci-config-fields` (overlapping built-in
matches may also be reported). `[DEFAULT]` and `region=us-ashburn-1` remain
unchanged unless another rule matches them.

```ini title="Fabricated AWS text — not credentials"
[default]
aws_access_key_id=AKIAAAAAAAAAAAAAAAAA
aws_secret_access_key=demo-secret-only
aws_session_token=demo-session-only
```

The access-key ID is masked by `aws-keys`; the secret and session assignments
are masked by `aws-credential-fields`. `ASIA` key IDs of the same length are
also covered. This key-ID regex is part of the recipe and starter configuration,
not the built-in bundle.

## Preview locally before enabling

From the directory containing the downloaded files, run these fabricated tests:

```sh
cui-proxy check-config -config cloud-cli.yaml
printf '%s\n' 'tenancy=ocid1.tenancy.oc1..demotenancyalpha' \
  'key_file=/demo/.oci/demo-key.pem' \
  'export OCI_CLI_PASSPHRASE=demo-passphrase-only' \
  'aws_secret_access_key=demo-secret-only' \
  'AWS_SESSION_TOKEN=demo-session-only' |
  cui-proxy test-rules -config cloud-cli.yaml
printf '%s\n' 'region=us-ashburn-1' 'output=json' |
  cui-proxy test-rules -config cloud-cli.yaml
```

The first preview must remove the fabricated values and report the matching
rules. The second stays unchanged. `test-rules` uses a throwaway vault, opens no
listener and makes no cloud requests. It tests text matching, not a complete
Codex request or response-restoration path. See [testing](testing.md) for those
checks.

If your policy requires refusal, add `action: block` to selected rules, or use
`policy.profile: strict` to reject every detected value. Keep
`policy.on_error: block`; strict also requires enabled audit with
`audit.on_error: block`. With a blocking rule, expect matching previews to fail
with exit code 1. Masking rules may restore original values in supported replies;
use blocking when that behavior is unsuitable.

## Extend coverage deliberately

The assignment patterns do not match JSON such as `{"pass_phrase":"demo"}`,
YAML `key: value`, command flags, or an assignment embedded in prose on the same
line. They do not recognize an unlabeled opaque secret. A JWT or PEM block may
match the built-ins, but a raw OCI security token or AWS secret is not guaranteed
to have a recognizable shape. The optional `export` handling is text matching,
not a shell parser. Test the actual representation your tools produce.

Add literal profile names, local paths, regions, endpoint hostnames, Object
Storage namespaces, AWS account IDs and role ARNs to a private dictionary when
your policy requires them. Use `synth: secret` for arbitrary names or paths.
Do not put an entire INI file into a dictionary: entries are literal strings,
not parsed settings. See [dictionary rules](rules.md#dictionaries).

These examples add no default protection to an existing installation. Detection
remains pattern-based; unmatched text can be forwarded. The proxy does not
prevent Codex from reading files or invoking cloud tools locally. Configure the
client's tool permissions separately and use fabricated data for verification.
