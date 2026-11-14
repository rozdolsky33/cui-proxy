package vault

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSavePropagatesDirectorySyncFailure(t *testing.T) {
	v := New(make([]byte, 32))
	want := errors.New("injected directory sync failure")
	called := false
	err := v.save(filepath.Join(t.TempDir(), "vault.bin"), func(string) error { called = true; return want })
	if !called || !errors.Is(err, want) {
		t.Fatal("directory sync failure not propagated")
	}
}
