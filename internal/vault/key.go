package vault

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const keySize = 32

// LoadOrCreateKey returns the 32-byte vault key stored at path, creating it
// (and its parent directory, 0700) with mode 0600 if absent. An existing file
// with permissions wider than 0600, or of the wrong size, is an error; the
// file is never silently chmodded.
func LoadOrCreateKey(path string) ([]byte, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("vault: key dir: %w", err)
	}
	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("vault: generate key: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	switch {
	case err == nil:
		_, werr := f.Write(key)
		if serr := f.Sync(); werr == nil {
			werr = serr
		}
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(path)
			return nil, fmt.Errorf("vault: write key: %w", werr)
		}
		return key, nil
	case errors.Is(err, os.ErrExist):
		return readKey(path)
	default:
		return nil, fmt.Errorf("vault: create key: %w", err)
	}
}

func readKey(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("vault: open key: %w", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("vault: stat key: %w", err)
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("vault: key file is not a regular file")
	}
	if st.Mode().Perm()&^0o600 != 0 {
		return nil, fmt.Errorf("vault: key file %s has mode %o, must be 0600 or stricter", path, st.Mode().Perm())
	}
	key, err := io.ReadAll(io.LimitReader(f, keySize+1))
	if err != nil {
		return nil, fmt.Errorf("vault: read key: %w", err)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("vault: key file must be exactly %d bytes", keySize)
	}
	return key, nil
}
