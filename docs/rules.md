# Detection rules and replacements

A rule answers two questions: **what text should be detected**, and **what
should happen when it matches**. Under the default `sanitize` profile, matches
are replaced with local vault synthetics. Under `strict`, they reject the
request. A rule can also explicitly block under sanitize.

Use the [configuration walkthrough](configuration.md) for the edit/validate/
reload cycle, [recipes](recipes.md) for complete files, and the
[reference](reference.md) for settings outside the `rules` list.

## Rule anatomy

This is a **partial YAML edit**: add the entry to your existing `rules` list.

```yaml
rules:
  - name: project-tickets
    type: regex
    pattern: 'DEMO-[0-9]{6}'
    synth: secret
    action: sanitize
```

It masks the entire `DEMO-123456` match under sanitize. Remove the explicit
action before using this entry in a strict configuration.

| Field | Applies to | Meaning |
|---|---|---|
| `name` | All; required | Unique rule name. Appears in diagnostics and logs, so use a category name, never a sensitive value |
| `type` | All; required | `builtin`, `regex`, or `dictionary` |
| `action` | All; optional | `sanitize` or `block`; omitted inherits `policy.profile`. Explicit sanitize is invalid under strict |
| `pattern` | Regex; required | Go regular expression; the whole match is selected |
| `validate` | Regex; optional | Additional candidate validator: `ipv4`, `ssn`, `luhn`, or `ocid` |
| `file` | Dictionary; required | Literal entries, one per line. Relative paths resolve from the YAML file's directory |
| `synth` | Regex/dictionary; required | Replacement class from the table below. Built-ins supply their own classes |

Use fields for their applicable rule type: `synth` does not change built-in
classes, and `validate` is not a general post-filter for the built-in bundle
or dictionaries. Unknown YAML field names are rejected. Rules are loaded from
the one selected YAML file; there is no automatic merge with starter rules.
Omitting rules does not enable built-ins. Sanitize can validate with no rules
and then detect nothing; strict requires at least one.

## The built-in bundle

This **partial YAML edit** enables the complete bundle:

```yaml
rules:
  - name: builtin
    type: builtin
```

Its detector names use `<name>.<class>`, such as `builtin.email`.
Multiple secret detectors share `builtin.secret`. It is one bundle: there is
no individual built-in category toggle, exclusion list, or per-value allowlist.

| Category | What it looks for | Important detail |
|---|---|---|
| `ocid` | Oracle Cloud identifiers | Checks OCID structure |
| `ipv4` | IPv4 literals | Validates addresses rather than accepting every dotted digit sequence |
| `ipv6` | IPv6 literals | Parses address candidates, including supported embedded address forms |
| `cidr` | IPv4/IPv6 address prefixes | Includes the prefix length in the detected span |
| `email` | Email-shaped text | Can match the user/host portion of an SSH remote |
| `ssn` | Dashed US Social Security number shapes | Applies validity checks; a synthetically invalid `000` area is not a real SSN detector sample |
| `card` | Separated card-like digit sequences | Uses Luhn validation and supported length checks |
| `secret` | Tokens using the `Bearer` scheme, JWT-shaped tokens, private-key blocks, selected numeric runs and address/port tokens | The bearer-token detector requires at least eight token characters; an unterminated private-key block is masked to end of text |

Unseparated card-like digit runs and long numeric IDs containing a Luhn hit
are masked as opaque secrets rather than preserving a card shape. Tcpdump-style
dotted address/port tokens are masked whole, including the port. Five
single-digit dotted components are treated as a version number, not an
address/port token.

The built-ins do **not** include AWS access-key IDs. The starter configuration
adds this separate rule (**partial YAML edit**):

```yaml
rules:
  - name: aws-keys
    type: regex
    pattern: '\b(?:AKIA|ASIA)[0-9A-Z]{16}\b'
    synth: secret
```

This detects key-ID shapes, not every AWS credential or secret-key format.
For OCI configuration fields and AWS credential assignments, use the
[OCI and AWS CLI rules guide](cloud-cli.md) and its complete recipe.
General hostnames, local paths, usernames, and names specific to your
organization need your own rules. A hostname that encodes an address is not
an IP literal. See [limitations](limitations.md).

## Custom regular expressions

Patterns use Go's RE2-style `regexp` syntax. Alternation, character classes,
repetition, and flags such as `(?i)` are available. Lookaround and
backreferences are not. Use single-quoted YAML strings so a backslash reaches
the regex engine unchanged.

**The entire regex match is replaced, not capture group 1.** For example,
`ticket=(DEMO-[0-9]{6})` masks `ticket=DEMO-123456`, including the label.
Use `DEMO-[0-9]{6}` if only the identifier should be replaced. The built-in
bearer-token detector has special internal handling for its token capture; custom
rules do not inherit it.

Validators reject candidates that fail their check; they do not change the
rule's action or automatically choose a replacement class. Match only the
value expected by the selected validator, and choose a compatible `synth`.
For arbitrary identifiers, `secret` is the simplest replacement class.

Test both positive and negative cases. A regex that looks useful on one line
may overmatch JSON tool output or an ordinary protocol identifier when you
enable blocking. The [testing guide](testing.md) explains local tests and
their limits.

## Dictionaries

A dictionary is a local text file of literal sensitive names. This is a
**complete example dictionary file**, using fabricated names:

```text
# demo-names.txt
demo-customer
demo-build-host
```

Connect it with this **partial YAML edit**:

```yaml
rules:
  - name: demo-names
    type: dictionary
    file: demo-names.txt
    synth: hostname
```

Blank lines and lines beginning with `#` after trimming are ignored. Leading
and trailing whitespace is removed. Entries are literal, not regex patterns;
matching is case-insensitive, duplicate entries are removed, and longer
entries are tried first. At an alphanumeric edge, the entry cannot be glued
to an adjacent alphanumeric character. Punctuation is not a general word
boundary: test the exact forms you expect to send.

An empty dictionary is rejected when detectors are built. Relative paths use
the YAML directory, not your shell's current directory; `~` inside YAML is
not expanded. Reload after editing the file. Adding a name increases
detection; a dictionary is not an allowlist.

## Replacement classes

`synth` chooses an output shape, not a detector. Setting `synth: email` does
not make an arbitrary regex validate email addresses. Shapes help the model
work with a value's role, but replacements need not preserve its exact format
or remain meaningful to external tools.

| Class | Replacement behavior and limits |
|---|---|
| `secret` | Opaque `REDACTED-` token followed by eight hexadecimal characters; useful for arbitrary identifiers |
| `hostname` | Hash-derived `h-….example` hostname |
| `email` | Hash-derived `user-…@mail.example` address |
| `ocid` | `ocid1.<type>.ocsynth..…`; retains a valid resource-type token when possible, otherwise uses `resource` |
| `ipv4` | Starts with the three RFC 5737 documentation blocks; after that finite pool is exhausted, may use the RFC 2544 benchmarking range |
| `ipv6` | Address in the IPv6 documentation prefix |
| `cidr` | Generates an IPv4-shaped address and retains the source prefix-length number; does not preserve IPv6 family or network topology |
| `ssn` | Dashed shape with area `000`, deliberately invalid as a real SSN |
| `card` | Replaces digits, retaining separators; starts with `400000` and computes a Luhn check digit. Generator accepts 8–19 digits; this is not a usable payment credential |

CIDR replacements are textual stand-ins. In particular, an IPv6 prefix length
can exceed the valid length for the generated IPv4 shape. Do not treat them
as operational network definitions or assume subnet relationships survive.

The IPv4 documentation pool has 762 addresses. The overflow pool can carry
real traffic in laboratory or fake-IP environments; it is not guaranteed to
be non-real. The vault stops allocating overflow addresses after observing a
real value in that range. A detected value equal to an overflow synthetic can
block rather than pass as already sanitized. Pools and collision retries are
finite; allocation failures block with the default policy.

Broad CIDRs, including default routes, can still use documentation-address
replacements when overflow allocation is disabled. A replacement's address
determines its pool; its prefix length does not. Real prefixes overlapping
the overflow range continue to disable overflow allocation.

Mappings are stable within the same vault. A new memory vault after a restart
does not retain the previous mapping. File mode retains mappings encrypted
locally. See [architecture](architecture.md#the-vault-and-response-restoration).

### When a replacement becomes an opaque token

The sanitizer verifies that its output restores faithfully in its surrounding
text. Some classes use boundaries to avoid accidental restoration inside
unrelated text. If a replacement would land against neighboring characters
that make those boundaries fail, the offending span is retried as `secret`.
Its audit event has `downgraded: true`; this changes the output shape, not the
policy action. A failed restoration check after that retry still blocks.

Overlapping detections also affect output shape: the merged union uses the
winning member's class, which may cover more text than that member originally
matched. If that class cannot generate or restore the merged value safely, the
request can be refused. Choose `secret` for broad custom patterns whose values
do not have a predictable shape.

## Precedence, overlaps, and actions

Rules are not a first-match firewall. Every detector runs. Overlapping spans
are merged transitively into their full union; spans that only touch stay
separate. The representative rule/class is chosen by longest match, then
earliest start, then class specificity (`ocid`, `cidr`, other classes, `ipv4`),
then detector input order for an exact tie.

**A block always survives merging.** If a short blocking rule overlaps a
longer sanitizing rule, the entire request is rejected. Reordering the list
does not allow the sanitizing rule to cancel that decision.

| Profile | Omitted action | `action: block` | `action: sanitize` |
|---|---|---|---|
| `sanitize` | Mask and restore | Reject whole request | Mask and restore |
| `strict` | Reject whole request | Reject whole request | Invalid configuration |

When any rule blocks, protocol fields normally preserved without scanning are
also checked against blocking rules, without rewriting. Known protocol
`include` constants remain unscanned; unknown include strings are inspected.
The ciphertext exception remains. Headers and query fields that cannot be rewritten block on
any detection, even under sanitize. Unsupported content always blocks.

Any blocking rule requires `policy.on_error: block`. Passthrough does not
override a policy match and is not an allowlist. There is no `allow`, `exclude`,
per-request exception, account allowlist, or verified approval mechanism in
the current implementation. Strict requires enabled auditing with audit
failure set to block. See [policy profiles](policy.md).

Validate configuration, try fabricated text in `test-rules` or the
[dashboard](dashboard.md), then reload. A failed reload keeps the old rules.
Use the [decision log](decision-log.md) and [troubleshooting guide](troubleshooting.md)
to find the effective rule behind a block. For a complete first run, use the
[quick start](quickstart.md) or [tutorial](tutorial.md).
