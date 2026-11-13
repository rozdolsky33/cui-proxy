package main

import (
	"context"
	"cui-proxy/internal/vault"
	"errors"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFileVaultCheckpointBeforeForward(t *testing.T) {
	up := newUpstream(t, func(u *upstream) { u.gate = make(chan struct{}) })
	dir, cfg := testDir(t, cfgSpec{Upstream: up.url(), Vault: fileVault})
	a := startApp(t, cfg, &logBuf{}, func(o *AppOptions) { o.saveInterval = time.Hour })
	px, _ := a.Addrs()
	done := make(chan struct{})
	go func() { ask(t, px, realIP); close(done) }()
	defer func() {
		close(up.gate)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("request did not finish")
		}
	}()
	syn := up.next(t)
	key, err := os.ReadFile(filepath.Join(dir, "keys", "vault.key"))
	if err != nil {
		t.Fatal("key unavailable")
	}
	saved, err := vault.Open(filepath.Join(dir, "vault.bin"), key)
	if err != nil {
		t.Fatal("forwarded mapping has no readable durable snapshot")
	}
	if got, ok := saved.Real(syn); !ok || got != realIP {
		t.Fatal("forwarded mapping not recoverable exactly")
	}
}

func TestFileVaultSaveFailureRefusesForwardAndRetries(t *testing.T) {
	up := newUpstream(t, nil)
	dir, cfg := testDir(t, cfgSpec{Upstream: up.url(), Vault: fileVault})
	a := startApp(t, cfg, &logBuf{}, func(o *AppOptions) { o.saveInterval = time.Hour })
	px, _ := a.Addrs()
	path := filepath.Join(dir, "vault.bin")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal("failure setup")
	}
	status, _ := ask(t, px, realIP)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("persistence refusal status: %d", status)
	}
	select {
	case <-up.texts:
		t.Fatal("failed persistence reached upstream")
	default:
	}
	if a.savedAt != 0 {
		t.Fatal("failed save advanced checkpoint")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal("failure recovery")
	}
	status, _ = ask(t, px, realIP)
	if status != http.StatusOK {
		t.Fatalf("retry status: %d", status)
	}
	up.next(t)
	if a.savedAt == 0 {
		t.Fatal("successful retry did not advance checkpoint")
	}
}

func TestMappingPersistenceCancellationAndMemoryMode(t *testing.T) {
	a := &App{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.persistMappings(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled checkpoint accepted")
	}
	if err := a.persistMappings(context.Background()); err != nil {
		t.Fatal("memory checkpoint touched disk")
	}
}

func TestConcurrentMappingCheckpointsRemainRecoverable(t *testing.T) {
	a := &App{vault: vault.New(make([]byte, 32)), vaultAt: filepath.Join(t.TempDir(), "vault.bin")}
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 1; i <= 8; i++ {
				real := netip.AddrFrom4([4]byte{10, 80, byte(worker), byte(i)}).String()
				syn, err := a.vault.Pseudonym("ipv4", real)
				if err != nil {
					t.Error("mapping allocation failed")
					return
				}
				if err := a.persistMappings(context.Background()); err != nil {
					t.Error("checkpoint failed")
					return
				}
				saved, err := vault.Open(a.vaultAt, make([]byte, 32))
				if err != nil {
					t.Error("checkpoint could not reopen")
					return
				}
				if got, ok := saved.Real(syn); !ok || got != real {
					t.Error("completed checkpoint lost mapping")
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	if a.vault.Len() != 32 || a.savedAt != a.vault.Changes() {
		t.Fatal("final checkpoint incomplete")
	}
}
