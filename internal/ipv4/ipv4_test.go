package ipv4

import "testing"

func TestParse(t *testing.T) {
	for in, want := range map[string]string{
		"198.18.1.1": "198.18.1.1", "198.018.001.001": "198.18.1.1", "198.18.001.1": "198.18.1.1",
		"000.0.00.255": "0.0.0.255", "0198.18.1.1": "", "256.1.1.1": "", "1.2.3": "", "1.2.3.4.5": "",
		"1..2.3": "", "a.b.c.d": "", "": "", "+1.2.3.4": "", "1.2.3.4 ": "", "::ffff:1.2.3.4": "",
	} {
		a, ok := Parse(in)
		if got := ""; ok {
			got = a.String()
			if got != want {
				t.Errorf("Parse(%q) = %s, want %q", in, got, want)
			}
		} else if want != "" {
			t.Errorf("Parse(%q) failed, want %s", in, want)
		}
	}
}

func TestParsePrefix(t *testing.T) {
	for in, want := range map[string]string{
		"198.018.1.1/24": "198.18.1.1/24", "10.0.0.0/8": "10.0.0.0/8", "1.2.3.4/0": "1.2.3.4/0",
		"1.2.3.4/33": "", "1.2.3.4": "", "1.2.3.4/": "", "1.2.3.4/x": "", "0198.18.1.1/8": "",
	} {
		p, ok := ParsePrefix(in)
		if ok != (want != "") || ok && p.String() != want {
			t.Errorf("ParsePrefix(%q) = %v %v, want %q", in, p, ok, want)
		}
	}
}
