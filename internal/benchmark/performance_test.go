// Package benchmark measures production components using the same private,
// versioned fixtures as the end-to-end performance runner.
package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cui-proxy/internal/adapter/codex"
	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

type fixture struct {
	ds   []detect.Detector
	v    *vault.Vault
	body []byte
	text string
}

func load(b *testing.B) fixture {
	b.Helper()
	dir := os.Getenv("CUI_BENCH_FIXTURE_DIR")
	if dir == "" {
		b.Skip("requires generated private benchmark fixture")
	}
	c, err := config.Load(filepath.Join(dir, "config.json"))
	if err != nil {
		b.Fatal("config invalid")
	}
	ds, err := detect.Build(c.Rules, dir)
	if err != nil {
		b.Fatal("detectors invalid")
	}
	key, err := vault.LoadOrCreateKey(c.Vault.KeyFile)
	if err != nil {
		b.Fatal("key invalid")
	}
	v, err := vault.Open(c.Vault.File, key)
	if err != nil {
		b.Fatal("vault invalid")
	}
	body, err := os.ReadFile(filepath.Join(dir, "case.json"))
	if err != nil {
		b.Fatal("case unavailable")
	}
	var tree any
	if json.Unmarshal(body, &tree) != nil {
		b.Fatal("case invalid")
	}
	var leaves []string
	var walk func(any)
	walk = func(x any) {
		switch n := x.(type) {
		case string:
			leaves = append(leaves, n)
		case []any:
			for _, v := range n {
				walk(v)
			}
		case map[string]any:
			for _, v := range n {
				walk(v)
			}
		}
	}
	walk(tree)
	text := strings.Join(leaves, "\n")
	spans := detect.Scan(ds, text)
	if len(spans) != 40 {
		b.Fatalf("fixture expected 40 matches; got %d", len(spans))
	}
	// Every match must already be in the seeded corpus; accidental matching
	// cannot enlarge the benchmark population during the measured loop.
	count := v.Len()
	for _, sp := range spans {
		if _, err := v.Pseudonym(sp.Class, text[sp.Start:sp.End]); err != nil {
			b.Fatal("mapping invalid")
		}
	}
	if v.Len() != count {
		b.Fatal("fixture contains unseeded matches")
	}
	b.ReportMetric(float64(len(ds)), "detectors")
	b.ReportMetric(float64(v.Len()), "mappings")
	return fixture{ds, v, body, text}
}
func BenchmarkDetection(b *testing.B) {
	f := load(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(f.text)))
	b.ResetTimer()
	for b.Loop() {
		if len(detect.Scan(f.ds, f.text)) != 40 {
			b.Fatal("match count changed")
		}
	}
}
func BenchmarkRequestWalk(b *testing.B) {
	f := load(b)
	s := transform.NewSanitizer(f.ds, f.v, nil)
	a := codex.New()
	if _, err := a.SanitizeRequest(f.body, s); err != nil {
		b.Fatal("warmup failed")
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(f.body)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := a.SanitizeRequest(f.body, s); err != nil {
			b.Fatal("request failed")
		}
	}
}
func BenchmarkRestoration(b *testing.B) {
	for _, cold := range []bool{false, true} {
		b.Run(fmt.Sprintf("cold=%t", cold), func(b *testing.B) {
			f := load(b)
			s := transform.NewSanitizer(f.ds, f.v, nil)
			in, err := s.Text(f.text)
			if err != nil {
				b.Fatal("sanitize failed")
			}
			r := transform.NewRestorer(f.v)
			if r.Text(in) != f.text {
				b.Fatal("oracle mismatch")
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(in)))
			b.ResetTimer()
			for b.Loop() {
				if cold {
					r = transform.NewRestorer(f.v)
				}
				if r.Text(in) != f.text {
					b.Fatal("restore mismatch")
				}
			}
		})
	}
}
func BenchmarkVaultSave(b *testing.B) {
	f := load(b)
	path := filepath.Join(b.TempDir(), "state.enc")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if f.v.Save(path) != nil {
			b.Fatal("save failed")
		}
	}
}
func BenchmarkMappingChurn(b *testing.B) {
	f := load(b)
	r := transform.NewRestorer(f.v)
	i := 0
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		i++
		value := fmt.Sprintf("ChurnNotebook%09dEnd", i)
		s, err := f.v.Pseudonym("secret", value)
		if err != nil || r.Text(s) != value {
			b.Fatal("churn restore failed")
		}
	}
}
