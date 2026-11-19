# Measured detection quality

This page reports how the built-in detectors perform on a generated corpus of
made-up values: how often a value of each family is fully masked, and how
often a harmless look-alike is masked by mistake.

The figures describe this corpus. They are not rates for your data. Read
[what the measurement does not show](#what-the-measurement-does-not-show)
before relying on them.

## Summary

Measured on the detectors at the commit that last changed
`internal/detecteval/testdata/baseline.json`.

| Family | Shapes tested | Samples | Not fully masked | False-negative rate |
|---|---|---|---|---|
| `ipv4` | 3 | 3,360 | 0 | 0.0% |
| `ipv6` | 8 | 8,960 | 0 | 0.0% |
| `cidr` | 2 | 2,080 | 0 | 0.0% |
| `email` | 11 | 12,320 | 0 | 0.0% |
| `ssn` | 1 | 1,040 | 0 | 0.0% |
| `card` | 5 | 5,200 | 0 | 0.0% |
| `secret` | 8 | 3,760 | 0 | 0.0% |
| `ocid` | 3 | 3,120 | 0 | 0.0% |
| All | 41 | 39,840 | 0 | 0.0% |

| Look-alikes of | Shapes tested | Samples | Flagged | False-positive rate |
|---|---|---|---|---|
| `ipv4` | 5 | 5,200 | 0 | 0.0% |
| `ipv6` | 3 | 3,120 | 0 | 0.0% |
| `cidr` | 2 | 2,080 | 0 | 0.0% |
| `email` | 2 | 2,080 | 0 | 0.0% |
| `ssn` | 4 | 4,160 | 0 | 0.0% |
| `card` | 2 | 2,080 | 0 | 0.0% |
| `secret` | 5 | 3,360 | 1,165 | 34.7% |
| Other ordinary values | 11 | 11,440 | 468 | 4.1% |
| All | 34 | 33,520 | 1,633 | 4.9% |

Every shape counts equally in these totals. A family's rate therefore
depends on which shapes were chosen, not on how often each occurs in real
work. The `secret` false-positive rate, for example, comes from two shapes
out of five.

## Values that were not fully masked

A value counts as fully masked only when every byte of it falls inside a
detected span. Anything less would forward part of the value.

In the current baseline, every documented shape was fully masked in every
context it was placed in.

The first run of this measurement found two shapes that were not. Both were
fixed before these figures were published, each with a regression test:

| Family | Shape | Samples affected | What happened |
|---|---|---|---|
| `email` | An apostrophe in the local part, such as a name written with one | 1,120 of 1,120 | Partly masked. The text before the apostrophe was left in place. |
| `ipv6` | An address ending in `::` that is directly followed by a colon | 40 of 1,120 for that shape | Missed in that one context. |

Unseparated 16-digit card numbers were fully masked, as an opaque `secret`
token and not as a `card`. The [rules guide](rules.md#the-built-in-bundle)
describes that behavior.

## Harmless values that were flagged

| Shape | Samples | Flagged | Rate |
|---|---|---|---|
| The word `Bearer` followed by an ordinary word of eight or more letters | 1,040 | 1,040 | 100.0% |
| The word `Bearer` followed by a word of three to seven letters | 1,040 | 125 | 12.0% |
| A 16-digit integer that is not a card number | 1,040 | 442 | 42.5% |
| A 64-character hexadecimal digest | 1,040 | 26 | 2.5% |

The second row is flagged when characters directly after the short word,
such as a suffix, a path or a full stop, extend it to eight token characters. The integer and digest rows follow from the
documented rule that a long digit run containing a checksum hit is masked.

These 30 look-alike shapes were never flagged:

- **Addresses:** an octet above 255, three-part and five-part version
  numbers, decimal numbers, dotted dates, MAC addresses, clock times, scope
  operators, fractions and slash dates.
- **Email:** at-mentions and `package@version` strings.
- **Social Security numbers:** phone numbers, ISO dates, ZIP+4 codes and
  area numbers that are never issued.
- **Cards:** 16-digit grouped numbers with a wrong checksum.
- **Secrets:** certificate blocks, public-key blocks and dotted identifiers.
- **Other:** UUIDs, 40-character commit hashes, epoch seconds and
  milliseconds, semantic versions, ISO timestamps, international phone
  numbers, 12-digit order numbers and hex colors.

## Shapes the built-in set does not cover

These were measured to confirm what the documentation already says. None is a
defect in a documented claim. If they matter to you, add
[your own rules](rules.md).

| Shape | Samples | Fully masked |
|---|---|---|
| Social Security number separated by spaces | 1,040 | 0 |
| Social Security number with no separators | 1,040 | 0 |
| Token after a lowercase `bearer` scheme word | 1,080 | 1,080 |
| Access-key-ID style token | 1,040 | 0 |
| Token with a short prefix and an underscore | 1,040 | 0 |
| Token with a short prefix and a dash | 1,040 | 0 |
| 32-character hexadecimal string | 1,040 | 0 |

The lowercase `bearer` form is not stated in the rules guide, and it was
masked in every sample.

Whole classes of data are outside pattern detection altogether, and no
corpus of this kind can measure them:

- Names of people, organizations, projects and products.
- Hostnames, usernames and local paths that are not in your dictionary.
- Values in an encoding the rules do not describe, such as base64.
- Sensitive meaning expressed in ordinary words.
- Anything your own regex and dictionary rules are meant to catch. Those
  rules are specific to you and are not part of this measurement.

## Method

- **Detectors.** The built-in bundle alone, under the default profile, with
  no custom or dictionary rules.
- **Positive values.** For each family, a generator produces made-up values
  in several shapes taken from public format descriptions: address notations,
  mailbox syntax, card numbering with its checksum, token encodings and
  identifier layouts. Each shape is marked as documented or not, according to
  the [rules guide](rules.md#the-built-in-bundle).
- **Negative values.** Generators produce look-alikes that are not members of
  a family, plus ordinary values that occur in technical text.
- **Contexts.** Each value is placed in every applicable context: prose,
  JSON, YAML, shell arguments, log fields, URLs, paths, quotes, brackets,
  punctuation directly before or after, and non-ASCII neighbors. There are
  35 contexts in total. Values that span lines use three multi-line
  contexts.
- **Scale.** 40 values per shape. 48 positive shapes and 34 negative shapes
  give 80,680 samples.
- **Scoring.** A positive is fully masked, partly masked or missed, by the
  bytes that detected spans cover. A negative is flagged when any span
  touches it.
- **Repeatability.** The generator is seeded and does not depend on the Go
  release. The source contains no literal sample values.

## What the measurement does not show

- **It is not a field rate.** Real traffic has its own mix of shapes and
  contexts. A shape that is rare here may be common for you, and the other
  way round.
- **The corpus was written inside the project.** Its shapes were chosen from
  format descriptions without reading the detectors' patterns, but the same
  project wrote both. A corpus supplied by someone else would be stronger
  evidence. So would shapes drawn from real, de-identified traffic.
- **The detectors are tested alone.** Request parsing, JSON decoding and
  stream restoration are not exercised here.
- **Clean results cover only the shapes tested.** A family at 0.0% has no
  misses among its listed shapes. It may have misses among shapes nobody
  thought to include.

## Keeping the figures current

`go test ./internal/detecteval` compares the detectors' results with a
recorded baseline and fails when any count changes. A change to detection
that moves a count has to update the baseline and this page in the same
change:

```sh
CUI_DETECTEVAL_UPDATE=1 go test ./internal/detecteval -run TestBaseline
go run ./scripts/detecteval
```

The first command rewrites the baseline. The second prints the tables as
counts and rates. Neither prints a corpus value.
