package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"cui-proxy/internal/vault"
)

func TestFileVaultRefusesConcurrentApps(t *testing.T) {
	_, cfg := testDir(t, cfgSpec{Vault: fileVault, Audit: "{enabled: false}"})
	opts := AppOptions{ConfigPath: cfg, Logf: (&logBuf{}).logf}
	first, err := NewApp(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	if second, err := NewApp(opts); err == nil {
		second.Close()
		t.Fatal("second app opened the same vault; stale writers can overwrite mappings")
	} else if !errors.Is(err, vault.ErrInUse) {
		t.Fatalf("second app failed for an unexpected reason: %v", err)
	}
	syn, err := first.vault.Pseudonym("hostname", "first.example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := NewApp(opts)
	if err != nil {
		t.Fatalf("reopen after shutdown: %v", err)
	}
	defer second.Close()
	if real, ok := second.vault.Real(syn); !ok || real != "first.example.invalid" {
		t.Fatal("reopened vault lost the first app's final mapping")
	}
}

func TestFileVaultLockReleasedAfterStartupFailure(t *testing.T) {
	for _, stage := range []string{"vault open", "audit open", "listener start"} {
		t.Run(stage, func(t *testing.T) {
			spec := cfgSpec{Vault: fileVault, Audit: "{enabled: false}"}
			dir, cfg := testDir(t, spec)
			opts := AppOptions{ConfigPath: cfg, Logf: (&logBuf{}).logf}
			switch stage {
			case "vault open":
				if _, err := vault.LoadOrCreateKey(filepath.Join(dir, "keys", "vault.key")); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(dir, "vault.bin"), "invalid vault")
			case "audit open":
				if err := os.Mkdir(filepath.Join(dir, "decisions.jsonl"), 0o700); err != nil {
					t.Fatal(err)
				}
				spec.Audit = "{file: decisions.jsonl}"
				writeFile(t, cfg, spec.render())
			}
			a, err := NewApp(opts)
			if stage == "listener start" {
				if err != nil {
					t.Fatal(err)
				}
				a.listen = func(string, string) (net.Listener, error) { return nil, errors.New("synthetic bind failure") }
				if err := a.Start(); err == nil {
					a.Close()
					t.Fatal("start accepted a failing listener")
				}
				a.Close()
			} else if err == nil {
				a.Close()
				t.Fatal("invalid startup succeeded")
			}
			if stage == "vault open" {
				if err := os.Remove(filepath.Join(dir, "vault.bin")); err != nil {
					t.Fatal(err)
				}
			}
			spec.Audit = "{enabled: false}"
			writeFile(t, cfg, spec.render())
			a, err = NewApp(opts)
			if err != nil {
				t.Fatalf("failed startup left the vault locked: %v", err)
			}
			a.Close()
		})
	}
}

func TestFileVaultLockReleasedAfterSaveFailure(t *testing.T) {
	dir, cfg := testDir(t, cfgSpec{Vault: fileVault, Audit: "{enabled: false}"})
	opts := AppOptions{ConfigPath: cfg, Logf: (&logBuf{}).logf}
	a, err := NewApp(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.vault.Pseudonym("hostname", "example.invalid"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "vault.bin")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.Shutdown(context.Background()); err == nil {
		t.Fatal("shutdown succeeded when a directory prevented saving the vault")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	next, err := NewApp(opts)
	if err != nil {
		t.Fatalf("failed final save left the vault locked: %v", err)
	}
	next.Close()
}

func TestFileVaultRefusesUnsafeLockFile(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "public file"} {
		t.Run(kind, func(t *testing.T) {
			dir, cfg := testDir(t, cfgSpec{Vault: fileVault, Audit: "{enabled: false}"})
			lockPath := filepath.Join(dir, "vault.bin.lock")
			switch kind {
			case "symlink":
				target := filepath.Join(dir, "other.lock")
				writeFile(t, target, "unrelated content")
				if err := os.Symlink(target, lockPath); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(lockPath, 0o700); err != nil {
					t.Fatal(err)
				}
			case "public file":
				writeFile(t, lockPath, "")
				if err := os.Chmod(lockPath, 0o666); err != nil {
					t.Fatal(err)
				}
			}
			a, err := NewApp(AppOptions{ConfigPath: cfg, Logf: (&logBuf{}).logf})
			if err == nil {
				a.Close()
				t.Fatal("startup accepted an unsafe vault lock file")
			}
		})
	}
}
