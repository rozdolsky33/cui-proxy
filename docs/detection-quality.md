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
| `card` | 9 | 9,360 | 0 | 0.0% |
| `secret` | 10 | 4,880 | 0 | 0.0% |
| `ocid` | 3 | 3,120 | 0 | 0.0% |
| All | 47 | 45,120 | 0 | 0.0% |

| Look-alikes of | Shapes tested | Samples | Flagged | False-positive rate |
|---|---|---|---|---|
| `ipv4` | 5 | 5,200 | 0 | 0.0% |
| `ipv6` | 3 | 3,120 | 0 | 0.0% |
| `cidr` | 2 | 2,080 | 0 | 0.0% |
| `email` | 2 | 2,080 | 0 | 0.0% |
| `ssn` | 4 | 4,160 | 0 | 0.0% |
| `card` | 2 | 2,080 | 0 | 0.0% |
| `secret` | 5 | 3,360 | 0 | 0.0% |
| Other ordinary values | 13 | 13,520 | 780 | 5.8% |
| All | 36 | 35,600 | 780 | 2.2% |

Every shape counts equally in these totals. A family's rate therefore
depends on which shapes were chosen, not on how often each occurs in real
work. The rate for other ordinary values, for example, comes from three shapes
out of thirteen.

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
| A 16-digit integer | 1,040 | 208 | 20.0% |
| An 18-digit integer | 1,040 | 182 | 17.5% |
| A 20-digit integer | 1,040 | 390 | 37.5% |

All three rows follow from the documented rule that a long digit run containing a
checksum hit is masked.

### Long integers

A run of digits is masked when it passes the card checksum as a whole, or
when a slice of 13 to 19 digits inside it does. Earlier baselines flagged
42.5% of 16-digit integers, and 60.0% of 18-digit integers measured the same
way, because any slice starting with 3 to 6 counted.

A slice now counts only when it starts like a card of
that length:

| Slice length | Must start with |
|---|---|
| 13 digits | 4 |
| 14 digits | 30, 36, 38 or 39 |
| 15 digits | 34 or 37 |
| 16 digits | 4; 50 to 58; 60, 62 to 65 or 67; 30, 35, 36, 38 or 39 |
| 17 or 18 digits | 50 or 56 to 58; 60, 62 to 65 or 67; 30, 35, 36, 38 or 39 |
| 19 digits | 4; 50 or 56 to 58; 60, 62 to 65 or 67; 30, 35, 36, 38 or 39 |

The limits on 13 to 15 digit slices shipped in v0.7.2 and took 18-digit
integers to 27.5%. The limits on longer slices shipped in v0.7.3.

A 20-digit integer is too long to be a card as a whole, but it holds 35
slices of card length. It was flagged in 40.0% of samples before the limits
on 18 and 19 digit slices and in 37.5% after. By calculation, most of what
remains comes from 13 and 16 digit slices, where real cards are common. A 64-character
hexadecimal digest was flagged in 2.5% of samples for the same reason and is
now flagged in none.

The rate cannot reach zero with this kind of rule. One random 16-digit
integer in ten passes the card checksum as a whole and cannot be told apart
from an unseparated card number. With 40 values per shape, these rates
are rough.

This has a cost. A card number that starts with something outside the table,
sitting inside a longer run of digits, was masked before and is not now. The
table of [shapes not covered](#shapes-the-built-in-set-does-not-cover)
measures three such cases. The prefix table follows the published ranges of
the large card schemes. A smaller or national scheme with another prefix is
not in it. The same number standing alone or written with separators is
detected as before.

### The word `Bearer` followed by ordinary words

Earlier baselines flagged the word `Bearer` followed by an ordinary word:
every sample with a word of eight or more letters, and 12.0% of samples with
a shorter word. Text such as "Bearer authentication is required" was masked.

The detector now skips text after `Bearer` that reads as words: no digit,
and each run of letters at most 20 long and all lower case, all upper case
or capitalized. Both shapes are now flagged in 0 of 1,040 samples.

This has a cost. A real token that is itself word-shaped, such as a
hand-chosen lower-case phrase, is no longer masked after `Bearer` unless it
is the value of an `Authorization` header or field. In that position
everything after `Bearer` is still masked. The table of
[shapes not covered](#shapes-the-built-in-set-does-not-cover) measures the
uncovered case.

These 33 look-alike shapes were never flagged:

- **Addresses:** an octet above 255, three-part and five-part version
  numbers, decimal numbers, dotted dates, MAC addresses, clock times, scope
  operators, fractions and slash dates.
- **Email:** at-mentions and `package@version` strings.
- **Social Security numbers:** phone numbers, ISO dates, ZIP+4 codes and
  area numbers that are never issued.
- **Cards:** 16-digit grouped numbers with a wrong checksum.
- **Secrets:** the word `Bearer` followed by an ordinary word, certificate
  blocks, public-key blocks and dotted identifiers.
- **Other:** UUIDs, 40-character commit hashes, 64-character digests, epoch
  seconds and
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
| Word-shaped token after `Bearer`, outside an `Authorization` header | 1,040 | 0 |
| Access-key-ID style token | 1,040 | 0 |
| Token with a short prefix and an underscore | 1,040 | 0 |
| Token with a short prefix and a dash | 1,040 | 0 |
| 32-character hexadecimal string | 1,040 | 0 |
| 13-digit card-like number starting with 5 or 6, inside a longer digit run | 1,040 | 0 |
| 16-digit card-like number starting with 31, 33, 59, 61, 66 or 69, inside a longer digit run | 1,040 | 26 |
| 19-digit card-like number starting with 33, 51, 61 or 69, inside a longer digit run | 1,040 | 234 |

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
- **Scale.** 40 values per shape. 58 positive shapes and 36 negative shapes
  give 92,200 samples.
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
