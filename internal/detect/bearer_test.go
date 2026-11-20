package detect

import (
	"strings"
	"testing"

	"cui-proxy/internal/config"
)

// maskedAfterBearer reports how much of value is covered by a secret span
// when it follows lead in text built as prefix + lead + value + suffix.
func maskedAfterBearer(t *testing.T, prefix, lead, value, suffix string) (full, any bool) {
	t.Helper()
	ds, err := Build([]config.Rule{{Name: "builtin", Type: "builtin"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	text := prefix + lead + value + suffix
	s := len(prefix) + len(lead)
	e := s + len(value)
	covered := make([]bool, len(text))
	for _, sp := range Scan(ds, text) {
		for i := sp.Start; i < sp.End; i++ {
			covered[i] = true
		}
	}
	full = true
	for i := s; i < e; i++ {
		full = full && covered[i]
		any = any || covered[i]
	}
	return full, any
}

func TestBearerFollowedByProseIsNotMasked(t *testing.T) {
	for _, c := range []struct{ prefix, value, suffix string }{
		{"The API uses ", "authentication", " for every call."},
		{"", "credentials", " are required"},
		{"use ", "Authentication", "."},
		{"HTTP ", "AUTHENTICATION", " scheme"},
		{"see ", "token-based", " access"},
		{"send a ", "token.", " Then retry."},
		{"the ", "access_token", " placeholder"},
		{"WWW-Authenticate: ", "realm=", "\"example\""},
		{`{"note": "`, "challenge", `"}`},
		{"path /docs/", "tokens/overview", " explains"},
	} {
		if _, any := maskedAfterBearer(t, c.prefix, "Bearer ", c.value, c.suffix); any {
			t.Errorf("prose after Bearer was masked: %q", c.value)
		}
	}
}

func TestBearerTokensStillMasked(t *testing.T) {
	mixed := strings.Repeat("aBcDeFgH", 3)
	for name, c := range map[string]struct{ prefix, value string }{
		"with digits":               {"x ", "abcd1234"},
		"mixed case letters":        {"x ", mixed},
		"long lower-case letters":   {"x ", strings.Repeat("abcdefg", 3)},
		"long upper-case letters":   {"x ", strings.Repeat("ABCDEFG", 3)},
		"mixed case, separated":     {"x ", "abCdEfgh-ijKlmNop"},
		"digit in a later part":     {"x ", "alpha-beta_gamma7"},
		"digit after an underscore": {"x ", "alphabeta_9"},
		"lower-case scheme":         {"x bearer", ""},
	} {
		lead := "Bearer "
		if c.value == "" {
			c.prefix, lead, c.value = "x ", "bearer ", mixed
		}
		if full, _ := maskedAfterBearer(t, c.prefix, lead, c.value, " end"); !full {
			t.Errorf("%s: token after Bearer was not fully masked", name)
		}
	}
}

// In an Authorization header the value is a credential whatever it looks
// like, so a word-shaped token is still masked there.
func TestBearerWordShapedTokenMaskedInAuthorizationHeader(t *testing.T) {
	for _, prefix := range []string{
		"Authorization: ",
		"authorization: ",
		"AUTHORIZATION:",
		`"Authorization": "`,
		`\"Authorization\": \"`,
		`-H 'Authorization: `,
		"Authorization=",
		"Proxy-Authorization: ",
		"Authorization:\t",
	} {
		for _, value := range []string{"mysecrettoken", "correct-horse-battery", "Password"} {
			if full, _ := maskedAfterBearer(t, "curl "+prefix, "Bearer ", value, "' end"); !full {
				t.Errorf("word-shaped token not masked after %q", prefix)
			}
		}
	}
}

func TestBearerProseNearOtherHeaderNamesIsNotMasked(t *testing.T) {
	for _, prefix := range []string{"Authentication: ", "Note: ", "authorization ", "Authorizations: x "} {
		if _, any := maskedAfterBearer(t, prefix, "Bearer ", "authentication", " required"); any {
			t.Errorf("prose masked after %q", prefix)
		}
	}
}
