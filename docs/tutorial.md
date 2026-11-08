# End-to-end configuration tutorial

Use this walkthrough to learn the whole edit → validate → preview → reload →
verify cycle. The scenario is a fictional service with a database host and
internal ticket IDs. Every sample is fabricated.

**You will finish with:** a separate test configuration, a working dictionary
and custom rule, a Codex session routed through it, and a strict-policy check.
The tutorial uses ports **18787 / 18788**, separate from the usual 8787 / 8788.

## 1. Create an isolated configuration directory

Install the binary using the [quick start](quickstart.md) if necessary. In a
terminal, run:

```sh
cui_demo_dir=$(mktemp -d "${TMPDIR:-/tmp}/cui-tutorial.XXXXXX")
curl -fsSL https://rozdolsky33.github.io/cui-proxy/examples/tutorial.yaml \
  -o "$cui_demo_dir/config.yaml"
curl -fsSL https://rozdolsky33.github.io/cui-proxy/examples/hosts.txt \
  -o "$cui_demo_dir/hosts.txt"
chmod 600 "$cui_demo_dir/config.yaml" "$cui_demo_dir/hosts.txt"
printf 'Tutorial directory: %s\n' "$cui_demo_dir"
```

Keep this terminal open: `$cui_demo_dir` refers to your new directory. You can
also download [tutorial.yaml](examples/tutorial.yaml) and
[hosts.txt](examples/hosts.txt) manually. Save the YAML as `config.yaml` beside
the dictionary. The directory contains only tutorial state; none of these
steps overwrites your normal setup.

## 2. Understand the complete file

```yaml title="config.yaml — complete tutorial configuration"
--8<-- "examples/tutorial.yaml"
```

The pieces have separate jobs:

1. `listen` receives Codex model traffic; `ui.listen` serves the dashboard.
2. `upstreams.codex` is the destination the proxy forwards to. It is different
   from the local URL configured in Codex.
3. `policy.profile: sanitize` masks detections. Both failure-handling settings
   remain `block`.
4. `vault.mode: memory` keeps reversible mappings until this process stops.
5. `audit.on_error: block` requires a decision record before forwarding. This
   recipe deliberately uses a stronger audit setting than the default starter.
6. Four rules enable built-ins, your dictionary, AWS-key patterns, and ticket IDs.

## 3. Validate and preview

```sh
cui-proxy check-config -config "$cui_demo_dir/config.yaml"
cui-proxy policy check -config "$cui_demo_dir/config.yaml"
printf '%s\n' 'Check db.demo.example at 203.0.113.42 for DEMO-123456' |
  cui-proxy test-rules -config "$cui_demo_dir/config.yaml"
```

Expect `ok: 4 rules`, a description of the sanitize policy, and three replaced
values in the text. The count table should name `service-hosts`, `builtin.ipv4`,
and `ticket-ids`. Exact stand-ins vary because each test uses a new temporary
vault. No upstream service is contacted by these commands.

## 4. Start and inspect

```sh
cui-proxy serve -config "$cui_demo_dir/config.yaml"
```

Open <http://localhost:18788/>. Confirm the header says **profile: sanitize**,
the listeners end in 18787 / 18788, and the rules table has all four entries.
Paste the previous test text into **Test a string** and click **Sanitize**.
See [Dashboard walkthrough](dashboard.md) for the expected display.

## 5. Extend your dictionary without restarting

Open the tutorial `hosts.txt` in your editor and add a new line:

```text
cache.demo.example
```

In the dashboard, try `Check cache.demo.example` **before** reloading. The
running service still has the old dictionary, so this name remains unchanged.
Click **Reload config**, confirm **reload ok**, and test again. The new name
should now become a synthetic hostname. The dictionary is a file loaded into
memory; editing it alone does not update a running service.

For normal operation, run `check-config` with the actual path before reloading.
A reload error keeps the previous valid configuration active. Changes to
listeners, upstreams, vault, or explicit audit settings need a restart.

## 6. Prove a request reaches this proxy

In a **second terminal**, launch a temporary Codex session:

```sh
codex -c 'model_provider="cui-tutorial"' \
  -c 'model_providers.cui-tutorial={name="cui-tutorial",base_url="http://localhost:18787/v1",wire_api="responses",requires_openai_auth=true,supports_websockets=false}'
```

Use the model already available in your Codex account. Submit:

```text
Repeat this line exactly without running tools:
Check db.demo.example at 203.0.113.42 for DEMO-123456
```

After it completes, inspect the dashboard's live feed and replacement/restore
counts. If the model repeated the stand-ins unchanged, your client should show
the original fabricated values. A model can paraphrase or omit text; use the
[verification guide](testing.md) to distinguish model behavior from routing or
restoration problems. The dashboard intentionally does not display the raw
upstream body or vault contents.

## 7. Switch to strict and see the difference

Edit only the profile in the tutorial YAML:

```yaml title="Partial edit inside the existing policy block"
policy:
  profile: strict
  on_error: block
  unsupported: block
```

All tutorial rules omit `action`, so they inherit the new profile. Audit is
already enabled with `on_error: block`. Validate the file in another terminal
(substitute the tutorial path printed in step 1), then click **Reload config**.

The header should show **profile: strict**. Test the same string. The dashboard
now reports `error: blocked by policy (rule=…, class=…)`; no sanitized text is returned. A complete
model request that matches any rule is refused before reaching the backend.
Strict applies to inspected workspace metadata and protocol fields too, so a
plain-looking prompt may still be blocked.

To mask ordinary values while blocking only selected categories, use the
[selective blocking recipe](recipes.md#mask-most-values-block-selected-rules).
Do not try to override strict with `action: sanitize`: validation rejects it.

## 8. Inspect a real request decision

Hover over a completed live-feed row to find its request ID. In a second
terminal, replace the example placeholders with your tutorial config path and
that ID:

```sh
cui-proxy explain -config /path/to/tutorial/config.yaml REQUEST-ID
cui-proxy audit verify -config /path/to/tutorial/config.yaml
```

String previews do not create decision records. Use a model request from step 6.
The explanation records the decision, reason, and rule counts without the text
or sensitive values. Hash-chain verification checks log integrity within its
[documented limits](decision-log.md).

## 9. Finish the exercise

Exit the temporary Codex session, then stop the tutorial proxy with Ctrl-C in
its terminal. Memory-vault mappings disappear at shutdown. The private tutorial
directory remains for inspection; remove it when you no longer need the exercise.

You can now apply the same cycle to your normal YAML: add only the rules you
need, validate, use fabricated positive and negative samples, reload, and verify
the actual Codex route. See [recipes](recipes.md) for complete starting points
and [configuration](configuration.md) for operational details.
