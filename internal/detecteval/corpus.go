// Package detecteval measures the built-in detectors against a generated
// corpus of made-up values.
//
// The corpus is built from public format descriptions (address notations,
// mailbox syntax, card numbering, token encodings), not from the detectors'
// patterns. Every value is produced by a seeded generator: the source holds
// no literal sample values, and the same seed always yields the same corpus.
package detecteval

import (
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"strings"
)

// Sample is one generated value placed in a surrounding context.
type Sample struct {
	Family   string // family the value belongs to, or imitates when negative
	Variant  string // the shape of the value within its family
	Context  string // the text the value was placed in
	Positive bool   // true when the value is one the family should catch
	Claimed  bool   // positives only: the documentation says this shape is detected
	Text     string
	Start    int // byte offsets of the value in Text
	End      int
}

// rng is a small deterministic generator (splitmix64). Its output does not
// depend on the Go release, so the corpus is stable across toolchains.
type rng struct{ s uint64 }

func (r *rng) next() uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// n returns a value in [0, max).
func (r *rng) n(max int) int { return int(r.next() % uint64(max)) }

// between returns a value in [lo, hi].
func (r *rng) between(lo, hi int) int { return lo + r.n(hi-lo+1) }

func (r *rng) pick(xs []string) string { return xs[r.n(len(xs))] }

func (r *rng) chars(alphabet string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[r.n(len(alphabet))]
	}
	return string(b)
}

func (r *rng) bytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.n(256))
	}
	return b
}

const (
	lower    = "abcdefghijklmnopqrstuvwxyz"
	upper    = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digits   = "0123456789"
	hexLower = "0123456789abcdef"
	base32lc = "abcdefghijklmnopqrstuvwxyz234567"
	alnum    = lower + upper + digits
	b64url   = alnum + "-_"
	b64std   = alnum + "+/"
)

// context wraps a value in surrounding text.
type context struct {
	name           string
	prefix, suffix string
}

// general contexts are applied to every single-line value.
var general = []context{
	{"prose", "The value ", " was recorded in the change ticket"},
	{"sentence-end", "Please check ", "."},
	{"json-string", `{"field": "`, `"}`},
	{"env-assignment", "TARGET=", ""},
	{"yaml-value", "target: ", ""},
	{"single-quoted", "set to '", "' by default"},
	{"parentheses", "the entry (", ") is new"},
	{"comma-list", "old, ", ", new"},
	{"backticks", "run with `", "` first"},
	{"log-field", "INFO handled item=", " status=ok"},
	{"shell-argument", "tool --target ", " --verbose"},
	{"tab-separated", "name\t", "\tactive"},
	{"angle-brackets", "<", ">"},
	{"own-line", "first line\n", "\nlast line"},
	{"url-query", "https://service.test/path?item=", "&page=2"},
	{"path-segment", "/srv/data/", "/report.txt"},
	{"escaped-json", `{\"field\": \"`, `\"}`},
	{"typographic-quotes", "\u201c", "\u201d"},
	{"colon-after", "", ": accepted"},
	{"tight-commas", "a,", ",b"},
	{"underscore-suffix", "saved as ", "_backup"},
	{"hyphen-suffix", "renamed to ", "-old"},
	{"semicolon-after", "use ", ";"},
	{"markdown-link", "[details](", ")"},
	{"xml-attribute", `<item value="`, `"/>`},
	{"non-ascii-neighbors", "\u0430\u0434\u0440\u0435\u0441 ", " \u0441\u0435\u0440\u0432\u0435\u0440\u0430"},
}

// multiline contexts are applied to values that span lines.
var multiline = []context{
	{"own-lines", "first line\n", "\nlast line"},
	{"after-prose", "The file contains:\n", "\n"},
	{"yaml-block", "key: |\n", "\nnext: value"},
}

func with(base []context, extra ...context) []context {
	return append(append([]context(nil), base...), extra...)
}

// generator produces values of one variant.
type generator struct {
	family, variant string
	positive        bool
	claimed         bool
	contexts        []context
	make            func(r *rng) string
	// lead is fixed text placed directly before the value. It is not part
	// of the value: a scheme word such as "Bearer " is not itself sensitive.
	lead string
}

func ipv4(r *rng) string {
	return fmt.Sprintf("%d.%d.%d.%d", r.between(1, 223), r.n(256), r.n(256), r.n(256))
}

func ipv4Private(r *rng) string {
	switch r.n(3) {
	case 0:
		return fmt.Sprintf("10.%d.%d.%d", r.n(256), r.n(256), r.n(256))
	case 1:
		return fmt.Sprintf("172.%d.%d.%d", r.between(16, 31), r.n(256), r.n(256))
	}
	return fmt.Sprintf("192.168.%d.%d", r.n(256), r.n(256))
}

// group returns one IPv6 group without leading zeros.
func group(r *rng) string { return fmt.Sprintf("%x", r.between(1, 0xffff)) }

func groups(r *rng, n int, pad bool) string {
	parts := make([]string, n)
	for i := range parts {
		if pad {
			parts[i] = fmt.Sprintf("%04x", r.n(0x10000))
		} else {
			parts[i] = group(r)
		}
	}
	return strings.Join(parts, ":")
}

func ipv6Middle(r *rng) string {
	return groups(r, r.between(1, 3), false) + "::" + groups(r, r.between(1, 3), false)
}

// luhn appends the check digit that makes body pass the Luhn test.
func luhn(body string) string {
	sum := 0
	for i := range len(body) {
		d := int(body[len(body)-1-i] - '0')
		if i%2 == 0 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return body + string(rune('0'+(10-sum%10)%10))
}

func split(s string, sep string, sizes ...int) string {
	var parts []string
	for _, n := range sizes {
		parts = append(parts, s[:n])
		s = s[n:]
	}
	return strings.Join(parts, sep)
}

func card16(r *rng, prefix string) string {
	return luhn(prefix + r.chars(digits, 15-len(prefix)))
}

// notLuhn returns a 16-digit number that fails the Luhn test.
func notLuhn(r *rng) string {
	c := card16(r, "4")
	last := c[15]
	return c[:15] + string(rune('0'+(int(last-'0')+r.between(1, 9))%10))
}

func ssn(r *rng, sep string) string {
	area := r.between(1, 899)
	if area == 666 {
		area = 667
	}
	return fmt.Sprintf("%03d%s%02d%s%04d", area, sep, r.between(1, 99), sep, r.between(1, 9999))
}

var tlds = []string{"com", "org", "net", "io", "dev", "gov", "edu", "de", "co.uk"}

func word(r *rng, lo, hi int) string { return r.chars(lower, r.between(lo, hi)) }

func domain(r *rng) string { return word(r, 3, 10) + "." + r.pick(tlds) }

func pem(r *rng, label string) string {
	dashes := strings.Repeat("-", 5)
	var b strings.Builder
	b.WriteString(dashes + "BEGIN " + label + dashes + "\n")
	for range r.between(4, 9) {
		b.WriteString(r.chars(b64std, 64) + "\n")
	}
	b.WriteString(dashes + "END " + label + dashes)
	return b.String()
}

func jwt(r *rng) string {
	enc := base64.RawURLEncoding
	header := fmt.Sprintf(`{"alg":"%s","typ":"JWT"}`, r.pick([]string{"HS256", "RS256", "ES256"}))
	payload := fmt.Sprintf(`{"sub":"%s","iat":%d}`, r.chars(digits, 10), r.between(1_600_000_000, 1_900_000_000))
	return enc.EncodeToString([]byte(header)) + "." + enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(r.bytes(32))
}

const privateKey = "PRIVATE" + " KEY"

var ocidTypes = []string{"instance", "compartment", "user", "vcn", "subnet", "volume", "bucket"}

func generators() []generator {
	pos := func(family, variant string, claimed bool, ctx []context, mk func(r *rng) string) generator {
		return generator{family, variant, true, claimed, ctx, mk, ""}
	}
	neg := func(family, variant string, ctx []context, mk func(r *rng) string) generator {
		return generator{family, variant, false, false, ctx, mk, ""}
	}
	led := func(g generator, lead string) generator {
		g.lead = lead
		return g
	}
	ip4ctx := with(general,
		context{"with-port", "listening on ", ":8443"},
		context{"in-url", "https://", "/status"})
	ip6ctx := with(general,
		context{"bracketed-port", "listening on [", "]:8443"},
		context{"in-url", "https://[", "]/status"})
	mailctx := with(general,
		context{"mailto", "mailto:", ""},
		context{"display-name", "Sam Example <", ">"})
	bearerctx := with(general, context{"authorization-header", "Authorization: ", ""})

	return []generator{
		// Positives. "claimed" marks shapes the rules documentation says are detected.
		pos("ipv4", "general", true, ip4ctx, ipv4),
		pos("ipv4", "private-range", true, ip4ctx, ipv4Private),
		pos("ipv4", "single-digit-octets", true, ip4ctx, func(r *rng) string {
			return fmt.Sprintf("%d.%d.%d.%d", r.between(1, 9), r.n(10), r.n(10), r.n(10))
		}),

		pos("ipv6", "full-padded", true, ip6ctx, func(r *rng) string { return groups(r, 8, true) }),
		pos("ipv6", "full-unpadded", true, ip6ctx, func(r *rng) string { return groups(r, 8, false) }),
		pos("ipv6", "compressed-middle", true, ip6ctx, ipv6Middle),
		pos("ipv6", "compressed-end", true, ip6ctx, func(r *rng) string { return groups(r, r.between(1, 4), false) + "::" }),
		pos("ipv6", "compressed-start", true, ip6ctx, func(r *rng) string { return "::" + groups(r, r.between(1, 4), false) }),
		pos("ipv6", "uppercase", true, ip6ctx, func(r *rng) string { return strings.ToUpper(ipv6Middle(r)) }),
		pos("ipv6", "ipv4-mapped", true, ip6ctx, func(r *rng) string { return "::ffff:" + ipv4(r) }),
		pos("ipv6", "short", true, ip6ctx, func(r *rng) string { return group(r) + "::" + fmt.Sprintf("%x", r.between(1, 15)) }),

		pos("cidr", "ipv4-prefix", true, general, func(r *rng) string { return fmt.Sprintf("%s/%d", ipv4(r), r.between(8, 32)) }),
		pos("cidr", "ipv6-prefix", true, general, func(r *rng) string {
			return fmt.Sprintf("%s::/%d", groups(r, r.between(2, 4), false), r.between(16, 64))
		}),

		pos("email", "simple", true, mailctx, func(r *rng) string { return word(r, 3, 10) + "@" + domain(r) }),
		pos("email", "dotted-local", true, mailctx, func(r *rng) string { return word(r, 3, 8) + "." + word(r, 3, 8) + "@" + domain(r) }),
		pos("email", "plus-tag", true, mailctx, func(r *rng) string { return word(r, 3, 8) + "+" + word(r, 2, 6) + "@" + domain(r) }),
		pos("email", "hyphen-underscore", true, mailctx, func(r *rng) string {
			return word(r, 2, 6) + "_" + word(r, 2, 6) + "-" + word(r, 2, 6) + "@" + domain(r)
		}),
		pos("email", "digits-in-local", true, mailctx, func(r *rng) string { return word(r, 3, 8) + r.chars(digits, r.between(1, 4)) + "@" + domain(r) }),
		pos("email", "subdomain", true, mailctx, func(r *rng) string {
			return word(r, 3, 8) + "@" + word(r, 2, 6) + "." + word(r, 2, 6) + "." + domain(r)
		}),
		pos("email", "mixed-case", true, mailctx, func(r *rng) string {
			return r.chars(upper, 1) + word(r, 2, 7) + "." + r.chars(upper, 1) + word(r, 2, 7) + "@" + r.chars(upper, 1) + domain(r)
		}),
		pos("email", "long-tld", true, mailctx, func(r *rng) string {
			return word(r, 3, 8) + "@" + word(r, 3, 8) + "." + r.pick([]string{"technology", "museum", "engineering", "international"})
		}),

		pos("email", "single-letter-local", true, mailctx, func(r *rng) string { return r.chars(lower, 1) + "@" + domain(r) }),
		pos("email", "apostrophe-local", true, mailctx, func(r *rng) string { return r.chars(lower, 1) + "'" + word(r, 3, 8) + "@" + domain(r) }),
		pos("email", "hyphenated-domain", true, mailctx, func(r *rng) string {
			return word(r, 3, 8) + "@" + word(r, 2, 6) + "-" + word(r, 2, 6) + "." + r.pick(tlds)
		}),

		pos("ssn", "dashed", true, general, func(r *rng) string { return ssn(r, "-") }),
		pos("ssn", "spaced", false, general, func(r *rng) string { return ssn(r, " ") }),
		pos("ssn", "unseparated", false, general, func(r *rng) string { return ssn(r, "") }),

		pos("card", "16-digit-spaces", true, general, func(r *rng) string { return split(card16(r, "4"), " ", 4, 4, 4, 4) }),
		pos("card", "16-digit-dashes", true, general, func(r *rng) string {
			return split(card16(r, fmt.Sprint(r.between(51, 55))), "-", 4, 4, 4, 4)
		}),
		pos("card", "15-digit-spaces", true, general, func(r *rng) string {
			return split(luhn(r.pick([]string{"34", "37"})+r.chars(digits, 12)), " ", 4, 6, 5)
		}),
		pos("card", "14-digit-spaces", true, general, func(r *rng) string {
			return split(luhn("36"+r.chars(digits, 11)), " ", 4, 6, 4)
		}),
		pos("card", "16-digit-unseparated", true, general, func(r *rng) string { return card16(r, "4") }),
		pos("card", "15-digit-inside-longer-run", true, general, func(r *rng) string {
			return r.chars("789", 1) + luhn(r.pick([]string{"34", "37"})+r.chars(digits, 12))
		}),
		pos("card", "13-digit-inside-longer-run", true, general, func(r *rng) string {
			return r.chars("789", 1) + luhn("4"+r.chars(digits, 11))
		}),
		pos("card", "13-digit-other-prefix-inside-longer-run", false, general, func(r *rng) string {
			return r.chars("789", 1) + luhn(r.pick([]string{"5", "6"})+r.chars(digits, 11))
		}),

		led(pos("secret", "bearer-token", true, bearerctx, func(r *rng) string { return r.chars(b64url, r.between(24, 48)) }), "Bearer "),
		led(pos("secret", "bearer-letters-only-token", true, bearerctx, func(r *rng) string { return r.chars(lower+upper, r.between(24, 48)) }), "Bearer "),
		led(pos("secret", "bearer-word-shaped-token-in-header", true, []context{{"authorization-header", "Authorization: ", ""}}, func(r *rng) string { return word(r, 8, 16) }), "Bearer "),
		led(pos("secret", "bearer-word-shaped-token-outside-header", false, general, func(r *rng) string { return word(r, 8, 16) }), "Bearer "),
		led(pos("secret", "bearer-lowercase-scheme", false, bearerctx, func(r *rng) string { return r.chars(b64url, r.between(24, 48)) }), "bearer "),
		pos("secret", "jwt", true, general, jwt),
		pos("secret", "private-key-pkcs8", true, multiline, func(r *rng) string { return pem(r, privateKey) }),
		pos("secret", "private-key-rsa", true, multiline, func(r *rng) string { return pem(r, "RSA "+privateKey) }),
		pos("secret", "private-key-ec", true, multiline, func(r *rng) string { return pem(r, "EC "+privateKey) }),
		pos("secret", "private-key-openssh", true, multiline, func(r *rng) string { return pem(r, "OPENSSH "+privateKey) }),

		pos("secret", "private-key-encrypted", true, multiline, func(r *rng) string { return pem(r, "ENCRYPTED "+privateKey) }),
		pos("secret", "private-key-escaped-newlines", true, general, func(r *rng) string {
			return strings.ReplaceAll(pem(r, privateKey), "\n", `\n`)
		}),

		pos("ocid", "regional", true, general, func(r *rng) string {
			return "ocid1." + r.pick(ocidTypes) + ".oc1." + r.pick([]string{"iad", "phx", "fra", "lhr"}) + "." + r.chars(base32lc, 60)
		}),
		pos("ocid", "no-region", true, general, func(r *rng) string { return "ocid1.tenancy.oc1.." + r.chars(base32lc, 60) }),
		pos("ocid", "other-realm", true, general, func(r *rng) string {
			return "ocid1." + r.pick(ocidTypes) + "." + r.pick([]string{"oc2", "oc3"}) + ".." + r.chars(base32lc, 60)
		}),

		// Token shapes the documentation says the built-in set does not cover.
		pos("token-not-covered", "access-key-id-style", false, general, func(r *rng) string { return "AKIA" + r.chars(upper+"234567", 16) }),
		pos("token-not-covered", "prefixed-underscore-token", false, general, func(r *rng) string { return "ghp_" + r.chars(alnum, 36) }),
		pos("token-not-covered", "prefixed-dash-token", false, general, func(r *rng) string { return "sk-" + r.chars(alnum, 40) }),
		pos("token-not-covered", "hex-32", false, general, func(r *rng) string { return r.chars(hexLower, 32) }),

		// Negatives: values that resemble a family but do not belong to it.
		neg("ipv4", "octet-over-255", general, func(r *rng) string {
			return fmt.Sprintf("%d.%d.%d.%d", r.between(256, 999), r.n(256), r.n(256), r.n(256))
		}),
		neg("ipv4", "three-part-version", general, func(r *rng) string { return fmt.Sprintf("%d.%d.%d", r.n(20), r.n(60), r.n(100)) }),
		neg("ipv4", "five-single-digits", general, func(r *rng) string {
			return fmt.Sprintf("%d.%d.%d.%d.%d", r.n(10), r.n(10), r.n(10), r.n(10), r.n(10))
		}),
		neg("ipv4", "decimal-number", general, func(r *rng) string { return fmt.Sprintf("%d.%s", r.n(1000), r.chars(digits, r.between(2, 6))) }),
		neg("ipv4", "dotted-date", general, func(r *rng) string {
			return fmt.Sprintf("%d.%02d.%02d", r.between(1990, 2030), r.between(1, 12), r.between(1, 28))
		}),

		neg("ipv6", "mac-address", general, func(r *rng) string {
			parts := make([]string, 6)
			for i := range parts {
				parts[i] = r.chars(hexLower, 2)
			}
			return strings.Join(parts, ":")
		}),
		neg("ipv6", "clock-time", general, func(r *rng) string { return fmt.Sprintf("%02d:%02d:%02d", r.n(24), r.n(60), r.n(60)) }),
		neg("ipv6", "scope-operator", general, func(r *rng) string {
			return r.chars("ghijklmnopqrstuvwxyz", r.between(3, 6)) + "::" + r.chars("ghijklmnopqrstuvwxyz", r.between(3, 8))
		}),

		neg("cidr", "fraction", general, func(r *rng) string { return fmt.Sprintf("%d/%d", r.between(1, 99), r.between(2, 100)) }),
		neg("cidr", "slash-date", general, func(r *rng) string {
			return fmt.Sprintf("%d/%d/%d", r.between(1, 12), r.between(1, 28), r.between(1990, 2030))
		}),

		neg("email", "at-mention", general, func(r *rng) string { return "@" + word(r, 3, 12) }),
		neg("email", "package-at-version", general, func(r *rng) string {
			return word(r, 3, 10) + "@" + fmt.Sprintf("%d.%d.%d", r.n(20), r.n(40), r.n(100))
		}),

		neg("ssn", "phone-3-3-4", general, func(r *rng) string {
			return fmt.Sprintf("%03d-%03d-%04d", r.between(200, 999), r.between(200, 999), r.n(10000))
		}),
		neg("ssn", "iso-date", general, func(r *rng) string {
			return fmt.Sprintf("%d-%02d-%02d", r.between(1990, 2030), r.between(1, 12), r.between(1, 28))
		}),
		neg("ssn", "zip-plus-four", general, func(r *rng) string { return r.chars(digits, 5) + "-" + r.chars(digits, 4) }),
		neg("ssn", "never-issued-area", general, func(r *rng) string {
			area := []int{0, 666, r.between(900, 999)}[r.n(3)]
			return fmt.Sprintf("%03d-%02d-%04d", area, r.between(1, 99), r.between(1, 9999))
		}),

		neg("card", "16-digit-spaces-bad-checksum", general, func(r *rng) string { return split(notLuhn(r), " ", 4, 4, 4, 4) }),
		neg("card", "16-digit-dashes-bad-checksum", general, func(r *rng) string { return split(notLuhn(r), "-", 4, 4, 4, 4) }),

		led(neg("secret", "bearer-then-short-word", general, func(r *rng) string { return word(r, 3, 7) }), "Bearer "),
		led(neg("secret", "bearer-then-long-word", general, func(r *rng) string {
			return r.pick([]string{"authentication", "credentials", "required", "challenge", "validation"})
		}), "Bearer "),
		neg("secret", "certificate-block", multiline, func(r *rng) string { return pem(r, "CERTIFICATE") }),
		neg("secret", "public-key-block", multiline, func(r *rng) string { return pem(r, "PUBLIC KEY") }),
		neg("secret", "dotted-identifier", general, func(r *rng) string { return word(r, 2, 5) + "." + word(r, 4, 9) + "." + word(r, 4, 9) }),

		neg("benign", "uuid", general, func(r *rng) string {
			return split(r.chars(hexLower, 32), "-", 8, 4, 4, 4, 12)
		}),
		neg("benign", "commit-hash-40", general, func(r *rng) string { return r.chars(hexLower, 40) }),
		neg("benign", "digest-64", general, func(r *rng) string { return r.chars(hexLower, 64) }),
		neg("benign", "epoch-seconds", general, func(r *rng) string { return fmt.Sprint(r.between(1_600_000_000, 1_900_000_000)) }),
		neg("benign", "epoch-milliseconds", general, func(r *rng) string {
			return fmt.Sprint(r.between(1_600_000_000, 1_900_000_000)) + r.chars(digits, 3)
		}),
		neg("benign", "semantic-version", general, func(r *rng) string {
			return fmt.Sprintf("%d.%d.%d-rc.%d", r.n(20), r.n(60), r.n(100), r.between(1, 9))
		}),
		neg("benign", "iso-timestamp", general, func(r *rng) string {
			return fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02dZ", r.between(2000, 2030), r.between(1, 12), r.between(1, 28), r.n(24), r.n(60), r.n(60))
		}),
		neg("benign", "phone-international", general, func(r *rng) string { return "+1" + r.chars("23456789", 1) + r.chars(digits, 9) }),
		neg("benign", "order-number-12", general, func(r *rng) string { return r.chars("123456789", 1) + r.chars(digits, 11) }),
		neg("benign", "integer-18", general, func(r *rng) string { return r.chars("123456789", 1) + r.chars(digits, 17) }),
		neg("benign", "integer-16", general, func(r *rng) string { return r.chars("123456789", 1) + r.chars(digits, 15) }),
		neg("benign", "hex-color", general, func(r *rng) string { return "#" + r.chars(hexLower, 6) }),
	}
}

// Corpus generates perVariant values for every variant and places each one in
// every context that applies to it.
func Corpus(seed uint64, perVariant int) []Sample {
	var out []Sample
	for _, g := range generators() {
		// Each variant has its own stream, keyed by its name, so adding or
		// reordering variants does not change the values of the others.
		h := fnv.New64a()
		h.Write([]byte(g.family + "/" + g.variant))
		r := &rng{s: seed ^ h.Sum64()}
		for range perVariant {
			v := g.make(r)
			for _, c := range g.contexts {
				out = append(out, Sample{
					Family: g.family, Variant: g.variant, Context: c.name,
					Positive: g.positive, Claimed: g.claimed,
					Text: c.prefix + g.lead + v + c.suffix, Start: len(c.prefix) + len(g.lead), End: len(c.prefix) + len(g.lead) + len(v),
				})
			}
		}
	}
	return out
}
