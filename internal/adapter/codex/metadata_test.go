package codex

import (
	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestMetadataCustomRulesAndRestoration(t *testing.T) {
	ds, err := detect.Build([]config.Rule{{Name: "workspace", Type: "regex", Pattern: "restricted-project", Synth: "hostname"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	v := vault.New(testKey)
	s := transform.NewSanitizer(ds, v, nil)
	raw := `{"turn_id":"stable-id","workspaces":{"/tmp/restricted-project":{"remote":"ssh://restricted-project/repo"}},"cwd":"/tmp/restricted-project"}`
	h := http.Header{"X-Codex-Turn-Metadata": {raw}, "Authorization": {"Bearer unchanged"}}
	if err := New().SanitizeHeaders(h, s); err != nil {
		t.Fatal(err)
	}
	out := h.Get(turnMetadataHeader)
	if strings.Contains(out, "restricted-project") {
		t.Fatal("custom match leaked")
	}
	if got := transform.NewRestorer(v).Text(out); got != raw {
		t.Fatalf("restoration mismatch: %s", got)
	}
	if h.Get("Authorization") != "Bearer unchanged" {
		t.Fatal("credential changed")
	}
}

func TestMetadataRejectsInvalidHeaders(t *testing.T) {
	cases := [][]string{
		{"{}", "{}"}, {}, {strings.Repeat(" ", 65536) + "{}"},
		{`{"workspaces":` + strings.Repeat("{\"a\":", 65) + `"x"` + strings.Repeat("}", 65) + `}`},
		{"{\"cwd\":\"\xff\"}"},
	}
	for i, values := range cases {
		e := newEnv(t)
		h := http.Header{turnMetadataHeader: values}
		if err := New().SanitizeHeaders(h, e.s); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}

func TestMetadataEscapedAndDuplicateFields(t *testing.T) {
	e := newEnv(t)
	raw := `{"workspaces":{"a":{"remote":"git\u0040github.com:example/a"}},"workspaces":{"b":{"remote":"alice@example.com"}}}`
	h := http.Header{turnMetadataHeader: {raw}}
	if err := New().SanitizeHeaders(h, e.s); err != nil {
		t.Fatal(err)
	}
	out := h.Get(turnMetadataHeader)
	if !json.Valid([]byte(out)) {
		t.Fatal("invalid output")
	}
	for _, s := range strs(t, []byte(out)) {
		if strings.Contains(s, "git@github.com") || strings.Contains(s, "alice@example.com") {
			t.Fatal("duplicate or escaped value leaked")
		}
	}
}

func FuzzTurnMetadata(f *testing.F) {
	for _, seed := range []string{`{}`, `{"workspaces":{"/tmp/10.20.30.15":{"remote":"git@github.com:demo/repo"}}}`, `{"cwd":"/tmp/demo"}`, `{"unknown":"alice\u0040example.com"}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		e := newEnv(t)
		h := http.Header{turnMetadataHeader: {raw}}
		if err := New().SanitizeHeaders(h, e.s); err != nil {
			return
		}
		out := h.Get(turnMetadataHeader)
		if !json.Valid([]byte(out)) {
			t.Fatal("invalid output")
		}
		for _, text := range strs(t, []byte(out)) {
			if len(e.s.Detect(text)) > 0 {
				t.Fatal("detected value survived")
			}
		}
	})
}
