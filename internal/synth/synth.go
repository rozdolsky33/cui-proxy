// Package synth generates deterministic synthetic stand-ins for sensitive
// values. Outputs come from reserved ranges (RFC 5737 / RFC 3849 addresses,
// .example hostnames, the 000 SSN area) so they can never be real values.
// The one exception is the IPv4 overflow pool (198.18.0.0/15, see
// OverflowPool), used only after RFC 5737 is exhausted.
// Generators never retain or log the real value.
package synth

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"cui-proxy/internal/ipv4"
)

// Generator derives a synthetic from sum = HMAC-SHA256(key, class+"\x00"+real)
// and an attempt counter (0 first; the caller increments on collision). The
// result must be a pure function of its arguments.
type Generator func(sum []byte, attempt int, real string) (string, error)

var registry = map[string]Generator{
	"ipv4":     genIPv4,
	"ipv6":     genIPv6,
	"cidr":     genCIDR,
	"hostname": genHostname,
	"email":    genEmail,
	"ocid":     genOCID,
	"ssn":      genSSN,
	"card":     genCard,
	"secret":   genSecret,
}

// Known reports whether class has a registered generator.
func Known(class string) bool { _, ok := registry[class]; return ok }

// Generate returns the synthetic for class, or an error for an unknown class
// or when the class's space is exhausted for this attempt.
func Generate(class string, sum []byte, attempt int, real string) (string, error) {
	g, ok := registry[class]
	if !ok {
		return "", fmt.Errorf("synth: unknown class %q", class)
	}
	if len(sum) != sha256.Size {
		return "", errors.New("synth: sum must be 32 bytes")
	}
	if attempt < 0 {
		return "", errors.New("synth: negative attempt")
	}
	return g(sum, attempt, real)
}

// stream expands (sum, attempt) into 32 pseudo-random bytes.
func stream(sum []byte, attempt int) []byte {
	var a [4]byte
	binary.BigEndian.PutUint32(a[:], uint32(attempt))
	h := sha256.New()
	h.Write(sum)
	h.Write([]byte{0})
	h.Write(a[:])
	return h.Sum(nil)
}

var v4Nets = [3]string{"192.0.2", "198.51.100", "203.0.113"}

const v4Pool = 3 * 254 // hosts .1-.254 in each RFC 5737 block

// OverflowPool is the number of overflow IPv4 synthetics: hosts .1-.254 of
// the 512 /24s of 198.18.0.0/15 (RFC 2544 benchmarking). They are coined
// only after every RFC 5737 address is taken. Unlike RFC 5737, the range can
// carry real traffic (lab networks, proxy fake-IP modes), so a vault stops
// using it once it has seen a real address there (see InOverflow).
const OverflowPool = 512 * 254

// ErrExhausted means a class has no free synthetic left. Its text carries
// no value.
var ErrExhausted = errors.New("synthetic pool exhausted")

var overflowNet = netip.MustParsePrefix("198.18.0.0/15")

// defaultAttempts bounds collision retries for classes with a large space.
const defaultAttempts = 4096

// Attempts is the number of attempts that visit a class's whole space: the
// pool size for ipv4 and cidr, defaultAttempts otherwise.
func Attempts(class string) int {
	if class == "ipv4" || class == "cidr" {
		return v4Pool + OverflowPool
	}
	return defaultAttempts
}

// InOverflow reports whether s is an IPv4 address (leading-zero octets and
// IPv4-mapped IPv6 included) inside 198.18.0.0/15, or a prefix that overlaps
// it.
func InOverflow(s string) bool {
	if p, ok := ipv4.ParsePrefix(s); ok {
		return p.Overlaps(overflowNet)
	}
	if a, ok := ipv4.Parse(s); ok {
		return overflowNet.Contains(a)
	}
	if p, err := netip.ParsePrefix(s); err == nil {
		a := p.Addr()
		if a.Is4In6() {
			if p.Bits() < 96 {
				return false
			}
			p = netip.PrefixFrom(a.Unmap(), p.Bits()-96)
		}
		return p.Overlaps(overflowNet)
	}
	a, err := netip.ParseAddr(s)
	return err == nil && overflowNet.Contains(a.Unmap())
}

// Canonical returns the one spelling of an ipv4 or cidr real used for vault
// keys and collision checks ("198.018.001.001" -> "198.18.1.1"; IPv6 CIDRs
// in netip form). ok is false when an ipv4/cidr value cannot be parsed.
// Other classes are returned unchanged.
func Canonical(class, real string) (string, bool) {
	switch class {
	case "ipv4":
		a, ok := ipv4.Parse(real)
		if !ok {
			return "", false
		}
		return a.String(), true
	case "cidr":
		if p, ok := ipv4.ParsePrefix(real); ok {
			return p.String(), true
		}
		p, err := netip.ParsePrefix(real)
		if err != nil {
			return "", false
		}
		return p.String(), true
	}
	return real, true
}

// ipv4Addr walks the RFC 5737 pool linearly from a hash-derived start, so
// attempts 0..v4Pool-1 visit every address there exactly once; attempts
// v4Pool..v4Pool+OverflowPool-1 then walk the overflow pool the same way.
func ipv4Addr(sum []byte, attempt int) (string, error) {
	r := stream(sum, 0)
	if attempt < v4Pool {
		start := int(binary.BigEndian.Uint32(r[:4]) % v4Pool)
		idx := (start + attempt) % v4Pool
		return fmt.Sprintf("%s.%d", v4Nets[idx/254], idx%254+1), nil
	}
	if attempt -= v4Pool; attempt >= OverflowPool {
		return "", ErrExhausted
	}
	start := int(binary.BigEndian.Uint32(r[4:8]) % OverflowPool)
	idx := (start + attempt) % OverflowPool
	net := idx / 254 // 0..511: 198.18.0-255 then 198.19.0-255
	return fmt.Sprintf("198.%d.%d.%d", 18+net/256, net%256, idx%254+1), nil
}

func genIPv4(sum []byte, attempt int, _ string) (string, error) { return ipv4Addr(sum, attempt) }

func genIPv6(sum []byte, attempt int, _ string) (string, error) {
	var b [16]byte
	copy(b[:], []byte{0x20, 0x01, 0x0d, 0xb8})
	copy(b[4:], stream(sum, attempt)[:12])
	return netip.AddrFrom16(b).String(), nil
}

func genCIDR(sum []byte, attempt int, real string) (string, error) {
	i := strings.LastIndexByte(real, '/')
	if i < 0 {
		return "", errors.New("synth: cidr has no prefix length")
	}
	p, err := netip.ParsePrefix(real)
	if err != nil {
		return "", errors.New("synth: invalid cidr")
	}
	ip, err := ipv4Addr(sum, attempt)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%d", ip, p.Bits()), nil
}

func genHostname(sum []byte, attempt int, _ string) (string, error) {
	return "h-" + hex.EncodeToString(stream(sum, attempt)[:4]) + ".example", nil
}

func genEmail(sum []byte, attempt int, _ string) (string, error) {
	return "user-" + hex.EncodeToString(stream(sum, attempt)[:4]) + "@mail.example", nil
}

func genSecret(sum []byte, attempt int, _ string) (string, error) {
	return "REDACTED-" + hex.EncodeToString(stream(sum, attempt)[:4]), nil
}

func genOCID(sum []byte, attempt int, real string) (string, error) {
	typ := "resource"
	if parts := strings.Split(real, "."); len(parts) > 1 && isTypeToken(parts[1]) {
		typ = parts[1]
	}
	return "ocid1." + typ + ".ocsynth.." + hex.EncodeToString(stream(sum, attempt)[:13]), nil
}

func isTypeToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func genSSN(sum []byte, attempt int, _ string) (string, error) {
	n := binary.BigEndian.Uint32(stream(sum, attempt)[:4]) % 1000000
	return fmt.Sprintf("000-%02d-%04d", n/10000, n%10000), nil
}

// genCard keeps the original grouping: every digit is replaced, every other
// byte is kept. Layout: 400000 + random body + Luhn check digit.
func genCard(sum []byte, attempt int, real string) (string, error) {
	n := 0
	for i := 0; i < len(real); i++ {
		if real[i] >= '0' && real[i] <= '9' {
			n++
		}
	}
	if n < 8 || n > 19 {
		return "", errors.New("synth: card digit count out of range")
	}
	digits := make([]byte, n)
	copy(digits, "400000")
	st := stream(sum, attempt)
	for i := 6; i < n-1; i++ {
		digits[i] = '0' + st[i]%10
	}
	digits[n-1] = '0' + luhnCheck(digits[:n-1])
	out := []byte(real)
	j := 0
	for i := range out {
		if out[i] >= '0' && out[i] <= '9' {
			out[i] = digits[j]
			j++
		}
	}
	return string(out), nil
}

// luhnCheck returns the check digit to append to payload.
func luhnCheck(payload []byte) byte {
	sum := 0
	for i := len(payload) - 1; i >= 0; i-- {
		d := int(payload[i] - '0')
		if (len(payload)-1-i)%2 == 0 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return byte((10 - sum%10) % 10)
}
