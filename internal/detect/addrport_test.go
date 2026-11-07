package detect

import (
	"reflect"
	"testing"
)

// I3: tcpdump addr.port notation is masked whole (port included) as class
// secret; version-like dotted runs are not.
func TestAddrPortNotation(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []got
	}{
		{"IP 10.20.30.15.443 > 10.20.30.16.51234: Flags [S]", []got{{"secret", "10.20.30.15.443"}, {"secret", "10.20.30.16.51234"}}},
		{"IP6 2001:db8::1.443 > 2001:db8::2.51234", []got{{"secret", "2001:db8::1.443"}, {"secret", "2001:db8::2.51234"}}},
		{"IP6 fe80::1ff:fe23:4567:890a.22 > fe80::2.5353: UDP", []got{{"secret", "fe80::1ff:fe23:4567:890a.22"}, {"secret", "fe80::2.5353"}}},
		{"10.20.30.15.443", []got{{"secret", "10.20.30.15.443"}}},
		{"x 10.20.30.15.7 y", []got{{"secret", "10.20.30.15.7"}}},
		{"(10.20.30.15.65535)", []got{{"secret", "10.20.30.15.65535"}}},
		// The port must be a port; otherwise nothing changes from before.
		{"10.20.30.15.65536", nil},
		{"10.20.30.15.443.1", nil},
		{"10.20.30.15.443x", nil},
		{"2001:db8::1.65536", nil},
		// Version-like strings stay.
		{"1.2.3.4.5", nil},
		{"256.1.1.1.80", nil},
		{"v1.2.3.4.5", nil},
		{"go1.26.3.1.2", nil},
		// Prose: a trailing period still leaves a plain ipv4 / ipv6.
		{"see 10.20.30.15.", []got{{"ipv4", "10.20.30.15"}}},
		{"see 2001:db8::1.", []got{{"ipv6", "2001:db8::1"}}},
		// Embedded-IPv4 IPv6 is an address, not addr.port.
		{"::ffff:10.20.30.15", []got{{"ipv6", "::ffff:10.20.30.15"}}},
	} {
		if g := builtinScan(t, c.in); !reflect.DeepEqual(g, c.want) {
			t.Errorf("%q: got %v, want %v", c.in, g, c.want)
		}
	}
}
