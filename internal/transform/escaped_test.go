package transform

import (
	"encoding/json"
	"strings"
	"testing"

	"cui-proxy/internal/vault"
)

// A real value with a quote, a backslash, a newline, a tab and '<' lands
// inside a JSON string literal (function-call arguments are JSON text).
const awkwardReal = "-----BEGIN PRIVATE KEY-----\nab\"c\\d\te<f>&\n-----END PRIVATE KEY-----"

func TestJSONEscapedTextKeepsInnerJSONValid(t *testing.T) {
	v := vault.New(testKey)
	syn := pseudo(t, v, "secret", awkwardReal)
	ip := pseudo(t, v, "ipv4", "10.20.30.15")
	r := NewRestorer(v).JSONEscaped()

	args := `{"cmd":"echo ` + syn + ` > /tmp/k; ping ` + ip + `"}`
	got := r.Text(args)
	var obj map[string]string
	if err := json.Unmarshal([]byte(got), &obj); err != nil {
		t.Fatalf("restored arguments are not valid JSON: %v", err)
	}
	if want := "echo " + awkwardReal + " > /tmp/k; ping 10.20.30.15"; obj["cmd"] != want {
		t.Errorf("cmd = %q, want %q", obj["cmd"], want)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("raw newline in restored JSON text")
	}
	// HTML characters are not escaped: the literal content is the minimal form.
	if !strings.Contains(got, `e<f>&`) {
		t.Errorf("HTML characters escaped: %q", got)
	}
	// The plain restorer is unaffected and the variant shares the vault.
	if plain := NewRestorer(v).Text(syn); plain != awkwardReal {
		t.Errorf("plain restorer changed: %q", plain)
	}
}

func TestJSONEscapedStreamEqualsText(t *testing.T) {
	v := vault.New(testKey)
	syn := pseudo(t, v, "secret", awkwardReal)
	ip := pseudo(t, v, "ipv4", "10.20.30.15")
	r := NewRestorer(v).JSONEscaped()
	texts := []string{
		`{"cmd":"echo ` + syn + ` ping ` + ip + `"}`,
		syn + ip,
		ip + " " + syn,
	}
	for _, text := range texts {
		want := r.Text(text)
		for _, i := range runeSplits(text) {
			st := r.NewStream()
			if got := st.Write(text[:i]) + st.Write(text[i:]) + st.Flush(); got != want {
				t.Errorf("split %d: got %q want %q", i, got, want)
			}
		}
	}
	if !json.Valid([]byte(r.Text(texts[0]))) {
		t.Errorf("escaped stream result is not valid JSON")
	}
}
