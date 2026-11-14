package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// fileMagic is the current format: v2 seals with the derived file subkey.
// Any other "CUIV" version is rejected with a clear error.
var fileMagic = []byte("CUIV2")

// payload is the decrypted file content. Older CUIV2 files hold a bare
// []entry; NoOverflow is additive (absent means false).
type payload struct {
	Entries    []entry `json:"entries"`
	NoOverflow bool    `json:"no_overflow,omitempty"`
}

type entry struct {
	Class     string `json:"c"`
	Real      string `json:"r"`
	Synthetic string `json:"s"`
}

// newGCM returns the file cipher: AES-256-GCM under the domain-separated
// subkey HMAC-SHA256(key, "cui-proxy/file"), so the raw vault key (also used
// for synthetic derivation) is never used directly as an AES key.
func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("vault: key must be %d bytes", keySize)
	}
	return newGCMRaw(subkey(key, "cui-proxy/file"))
}

func newGCMRaw(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// Save writes the vault encrypted with AES-256-GCM using the vault key. The
// write is atomic: temp file (0600) in the same directory, fsync, rename.
func (v *Vault) Save(path string) error {
	return v.save(path, syncDirectory)
}

func (v *Vault) save(path string, syncDir func(string) error) error {
	gcm, err := newGCM(v.key)
	if err != nil {
		return err
	}
	v.mu.RLock()
	entries := make([]entry, 0, len(v.fwd))
	for fk, syn := range v.fwd {
		class, real := splitKey(fk)
		entries = append(entries, entry{class, real, syn})
	}
	noOverflow := v.noOverflow
	v.mu.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Synthetic < entries[j].Synthetic })
	plain, err := json.Marshal(payload{Entries: entries, NoOverflow: noOverflow})
	if err != nil {
		return errors.New("vault: encode")
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("vault: nonce: %w", err)
	}
	blob := append(append([]byte(nil), fileMagic...), nonce...)
	blob = gcm.Seal(blob, nonce, plain, fileMagic)

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".vault-*")
	if err != nil {
		return fmt.Errorf("vault: temp file: %w", err)
	}
	tmpName := tmp.Name()
	fail := func(e error) error {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("vault: save: %w", e)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail(err)
	}
	if _, err := tmp.Write(blob); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("vault: save: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("vault: save: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("vault: directory sync: %w", err)
	}
	return nil
}

func splitKey(fk string) (class, real string) {
	for i := 0; i < len(fk); i++ {
		if fk[i] == 0 {
			return fk[:i], fk[i+1:]
		}
	}
	return "", fk
}

// Open decrypts and loads a vault file. Any failure (wrong key, tampering,
// inconsistent content) returns an error and no vault.
func Open(path string, key []byte) (*Vault, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vault: read: %w", err)
	}
	if len(blob) >= len(fileMagic) && string(blob[:4]) == "CUIV" && string(blob[:len(fileMagic)]) != string(fileMagic) {
		return nil, fmt.Errorf("vault: unsupported vault file version %q (want %q); preserve the vault and key and use a compatible binary; do not resume existing histories with a new vault", blob[:len(fileMagic)], fileMagic)
	}
	hdr := len(fileMagic) + gcm.NonceSize()
	if len(blob) < hdr+gcm.Overhead() || string(blob[:len(fileMagic)]) != string(fileMagic) {
		return nil, errCorrupt
	}
	plain, err := gcm.Open(nil, blob[len(fileMagic):hdr], blob[hdr:], fileMagic)
	if err != nil {
		return nil, errors.New("vault: cannot decrypt vault file (wrong key or corrupt)")
	}
	var p payload
	if first := firstByte(plain); first == '[' {
		err = json.Unmarshal(plain, &p.Entries) // older CUIV2 payload
	} else {
		err = json.Unmarshal(plain, &p)
	}
	if err != nil {
		return nil, errCorrupt
	}
	v := New(key)
	seen := map[[2]string]bool{}
	for _, e := range p.Entries {
		if seen[[2]string{e.Class, e.Real}] {
			return nil, errCorrupt
		}
		seen[[2]string{e.Class, e.Real}] = true
		if _, dup := v.rev[e.Synthetic]; dup {
			return nil, errCorrupt
		}
		v.insert(e.Class+"\x00"+e.Real, e.Real, e.Synthetic)
	}
	if p.NoOverflow {
		v.noOverflow = true
	}
	v.changes = 0 // as loaded: nothing to save yet
	return v, nil
}

func firstByte(b []byte) byte {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			return c
		}
	}
	return 0
}

func syncDirectory(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}
