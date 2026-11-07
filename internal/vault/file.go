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

var fileMagic = []byte("CUIV1")

type entry struct {
	Class     string `json:"c"`
	Real      string `json:"r"`
	Synthetic string `json:"s"`
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("vault: key must be %d bytes", keySize)
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// Save writes the vault encrypted with AES-256-GCM using the vault key. The
// write is atomic: temp file (0600) in the same directory, fsync, rename.
func (v *Vault) Save(path string) error {
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
	v.mu.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Synthetic < entries[j].Synthetic })
	plain, err := json.Marshal(entries)
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
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
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
	hdr := len(fileMagic) + gcm.NonceSize()
	if len(blob) < hdr+gcm.Overhead() || string(blob[:len(fileMagic)]) != string(fileMagic) {
		return nil, errCorrupt
	}
	plain, err := gcm.Open(nil, blob[len(fileMagic):hdr], blob[hdr:], fileMagic)
	if err != nil {
		return nil, errors.New("vault: cannot decrypt vault file (wrong key or corrupt)")
	}
	var entries []entry
	if err := json.Unmarshal(plain, &entries); err != nil {
		return nil, errCorrupt
	}
	v := New(key)
	for _, e := range entries {
		fk := e.Class + "\x00" + e.Real
		if _, dup := v.fwd[fk]; dup {
			return nil, errCorrupt
		}
		if _, dup := v.rev[e.Synthetic]; dup {
			return nil, errCorrupt
		}
		v.insert(fk, e.Real, e.Synthetic)
	}
	return v, nil
}
