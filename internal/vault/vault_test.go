package vault

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cui-proxy/internal/synth"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

func TestPseudonymStableAndInjective(t *testing.T) {
	v := New(testKey)
	seen := map[string]string{}
	for i := 0; i < 1000; i++ {
		real := fmt.Sprintf("host%d.corp.internal", i)
		s, err := v.Pseudonym("hostname", real)
		if err != nil {
			t.Fatal(err)
		}
		if prev, dup := seen[s]; dup {
			t.Fatalf("collision %q for %q and %q", s, prev, real)
		}
		seen[s] = real
	}
	if v.Len() != 1000 {
		t.Errorf("Len %d", v.Len())
	}
	a, _ := v.Pseudonym("hostname", "host5.corp.internal")
	b, _ := v.Pseudonym("hostname", "host5.corp.internal")
	if a != b || v.Len() != 1000 {
		t.Error("not stable")
	}
	// Determinism across vault instances with the same key.
	w := New(testKey)
	c, _ := w.Pseudonym("hostname", "host5.corp.internal")
	if c != a {
		t.Error("not deterministic across instances")
	}
	if d, _ := New([]byte("fedcba9876543210fedcba9876543210")).Pseudonym("hostname", "host5.corp.internal"); d == a {
		t.Error("different key should give different synthetic")
	}
}

func TestPseudonymUnknownClass(t *testing.T) {
	if _, err := New(testKey).Pseudonym("nope", "x"); err == nil {
		t.Error("expected error")
	}
}

func sum(class, real string) []byte {
	m := hmac.New(sha256.New, testKey)
	m.Write([]byte(class + "\x00" + real))
	return m.Sum(nil)
}

func TestPseudonymAvoidsReservedReal(t *testing.T) {
	real := "db.corp.internal"
	first, err := synth.Generate("hostname", sum("hostname", real), 0, real)
	if err != nil {
		t.Fatal(err)
	}
	v := New(testKey)
	v.Reserve(first)
	got, err := v.Pseudonym("hostname", real)
	if err != nil {
		t.Fatal(err)
	}
	if got == first {
		t.Error("synthetic equals reserved real")
	}
}

func TestPseudonymAvoidsHeldReal(t *testing.T) {
	// A real already in the vault (another class) must not be produced as a synthetic.
	real := "db.corp.internal"
	first, _ := synth.Generate("hostname", sum("hostname", real), 0, real)
	v := New(testKey)
	if _, err := v.Pseudonym("secret", first); err != nil {
		t.Fatal(err)
	}
	got, err := v.Pseudonym("hostname", real)
	if err != nil || got == first {
		t.Errorf("got %q err %v", got, err)
	}
}

func TestIPv4ExhaustionReturnsError(t *testing.T) {
	v := New(testKey)
	for i := 0; i < 762; i++ {
		if _, err := v.Pseudonym("ipv4", fmt.Sprintf("10.%d.%d.1", i/256, i%256)); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	if _, err := v.Pseudonym("ipv4", "10.9.9.9"); err == nil {
		t.Error("763rd should fail")
	}
	if v.Len() != 762 {
		t.Errorf("Len %d", v.Len())
	}
}

func TestRealRoundTrip(t *testing.T) {
	v := New(testKey)
	s, _ := v.Pseudonym("email", "alice@corp.com")
	r, ok := v.Real(s)
	if !ok || r != "alice@corp.com" {
		t.Errorf("%q %v", r, ok)
	}
	if _, ok := v.Real("nothing"); ok {
		t.Error("unexpected hit")
	}
	if !v.IsSynthetic(s) || v.IsSynthetic("alice@corp.com") {
		t.Error("IsSynthetic")
	}
}

func TestHasSyntheticWithPrefix(t *testing.T) {
	v := New(testKey)
	s, _ := v.Pseudonym("email", "alice@corp.com")
	for i := 0; i <= len(s); i++ {
		if !v.HasSyntheticWithPrefix(s[:i]) {
			t.Errorf("prefix %q should match", s[:i])
		}
	}
	if v.HasSyntheticWithPrefix(s+"x") || v.HasSyntheticWithPrefix("zzz") {
		t.Error("false positive")
	}
}

func TestSyntheticsSortedLongestFirst(t *testing.T) {
	v := New(testKey)
	v.Pseudonym("ipv4", "10.0.0.1")
	v.Pseudonym("cidr", "10.0.0.0/8")
	v.Pseudonym("email", "a@b.co")
	v.Pseudonym("secret", "abcdefgh1234")
	v.Pseudonym("hostname", "x.y")
	got := v.Synthetics()
	if len(got) != 5 {
		t.Fatalf("%v", got)
	}
	for i := 1; i < len(got); i++ {
		a, b := got[i-1], got[i]
		if len(a) < len(b) || (len(a) == len(b) && a >= b) {
			t.Errorf("order: %q before %q", a, b)
		}
	}
}

func TestSaveOpenRoundTrip(t *testing.T) {
	v := New(testKey)
	s1, _ := v.Pseudonym("email", "alice@corp.com")
	s2, _ := v.Pseudonym("ipv4", "10.1.1.1")
	path := filepath.Join(t.TempDir(), "sub", "vault.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte("alice@corp.com")) {
		t.Error("plaintext in vault file")
	}
	w, err := Open(path, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if w.Len() != 2 {
		t.Errorf("Len %d", w.Len())
	}
	if r, ok := w.Real(s1); !ok || r != "alice@corp.com" {
		t.Error("s1")
	}
	if r, ok := w.Real(s2); !ok || r != "10.1.1.1" {
		t.Error("s2")
	}
	if again, _ := w.Pseudonym("email", "alice@corp.com"); again != s1 {
		t.Error("restored vault should reuse mapping")
	}
	// No temp files left behind.
	ents, _ := os.ReadDir(filepath.Dir(path))
	if len(ents) != 1 {
		t.Errorf("leftover files: %v", ents)
	}
}

func TestOpenWrongKeyFails(t *testing.T) {
	v := New(testKey)
	v.Pseudonym("email", "alice@corp.com")
	path := filepath.Join(t.TempDir(), "v.bin")
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	if w, err := Open(path, []byte("fedcba9876543210fedcba9876543210")); err == nil || w != nil {
		t.Error("wrong key must fail")
	}
	if _, err := Open(path, []byte("short")); err == nil {
		t.Error("short key must fail")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "missing"), testKey); err == nil {
		t.Error("missing file must fail")
	}
	raw, _ := os.ReadFile(path)
	raw[len(raw)-1] ^= 1
	os.WriteFile(path, raw, 0o600)
	if _, err := Open(path, testKey); err == nil {
		t.Error("tampered file must fail")
	}
}

func TestKeyFilePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "newdir", "inner")
	path := filepath.Join(dir, "key")
	k1, err := LoadOrCreateKey(path)
	if err != nil || len(k1) != 32 {
		t.Fatalf("%v len %d", err, len(k1))
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("key mode %v", st.Mode().Perm())
	}
	ds, _ := os.Stat(dir)
	if ds.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v", ds.Mode().Perm())
	}
	k2, err := LoadOrCreateKey(path)
	if err != nil || !bytes.Equal(k1, k2) {
		t.Error("second call should return same key")
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(path); err == nil {
		t.Error("wide mode must error")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o640 {
		t.Error("must not chmod silently")
	}
	os.Chmod(path, 0o600)
	os.WriteFile(path, []byte("short"), 0o600)
	if _, err := LoadOrCreateKey(path); err == nil {
		t.Error("wrong-size key must error")
	}
}

func TestConcurrentUse(t *testing.T) {
	v := New(testKey)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				real := fmt.Sprintf("h%d.corp", i%50)
				s, err := v.Pseudonym("hostname", real)
				if err != nil {
					t.Error(err)
					return
				}
				if r, ok := v.Real(s); !ok || r != real {
					t.Error("round trip")
				}
				v.HasSyntheticWithPrefix(s[:3])
				v.IsSynthetic(s)
				v.Reserve(fmt.Sprintf("r%d-%d", g, i))
				v.Synthetics()
				v.Len()
			}
		}(g)
	}
	wg.Wait()
	if v.Len() != 50 {
		t.Errorf("Len %d", v.Len())
	}
}

// Task 4 fix S1: audit fingerprints are keyed, not a bare SHA-256 prefix.
func TestFingerprintKeyed(t *testing.T) {
	v := New(testKey)
	fp := v.Fingerprint("ssn", "123-45-6789")
	if len(fp) != 8 || strings.Trim(fp, "0123456789abcdef") != "" {
		t.Fatalf("fingerprint %q is not 8 hex chars", fp)
	}
	if fp != v.Fingerprint("ssn", "123-45-6789") || fp != New(testKey).Fingerprint("ssn", "123-45-6789") {
		t.Error("fingerprint not stable for the same key")
	}
	if fp == New([]byte("fedcba9876543210fedcba9876543210")).Fingerprint("ssn", "123-45-6789") {
		t.Error("different key gave same fingerprint")
	}
	if fp == v.Fingerprint("secret", "123-45-6789") {
		t.Error("class not bound into fingerprint")
	}
	h := sha256.Sum256([]byte("123-45-6789"))
	if fp == hex.EncodeToString(h[:4]) {
		t.Error("fingerprint is the unkeyed SHA-256 prefix")
	}
	// Domain separation: not the synthetic-derivation HMAC either.
	if fp == hex.EncodeToString(sum("ssn", "123-45-6789")[:4]) {
		t.Error("fingerprint reuses the synthetic-derivation key")
	}
}

// Task 4 fix S1: the vault file is sealed with a derived subkey, not the raw key.
func TestFileSealedWithSubkey(t *testing.T) {
	v := New(testKey)
	if _, err := v.Pseudonym("ipv4", "10.1.2.3"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "v.bin")
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := newGCMRaw(testKey)
	if err != nil {
		t.Fatal(err)
	}
	hdr := len(fileMagic) + raw.NonceSize()
	if _, err := raw.Open(nil, blob[len(fileMagic):hdr], blob[hdr:], fileMagic); err == nil {
		t.Error("vault file opens with the raw key; want a derived file subkey")
	}
	if w, err := Open(path, testKey); err != nil || w.Len() != 1 {
		t.Fatalf("Open: %v", err)
	}
}

// Task 4 F2: the restorer needs each synthetic's class.
func TestClassOf(t *testing.T) {
	v := New(testKey)
	s, err := v.Pseudonym("secret", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := v.ClassOf(s); !ok || c != "secret" {
		t.Errorf("ClassOf = %q, %v", c, ok)
	}
	if _, ok := v.ClassOf("tok"); ok {
		t.Error("ClassOf of a real value")
	}
	path := filepath.Join(t.TempDir(), "v.bin")
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	w, err := Open(path, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := w.ClassOf(s); !ok || c != "secret" {
		t.Errorf("after Open: ClassOf = %q, %v", c, ok)
	}
}

// Task 4 F2: files from the previous format give a clear version error.
func TestOpenOldVersionClearError(t *testing.T) {
	v := New(testKey)
	path := filepath.Join(t.TempDir(), "v.bin")
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(blob, []byte("CUIV2")) {
		t.Fatalf("magic %q, want CUIV2", blob[:5])
	}
	copy(blob, "CUIV1")
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Open(path, testKey)
	if err == nil || !strings.Contains(err.Error(), "version") || !strings.Contains(err.Error(), "CUIV1") {
		t.Errorf("err = %v, want a clear version error naming CUIV1", err)
	}
}
