package vault

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func recoveryOriginal(i int) string {
	return netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}).String()
}

func TestReopenPreservesCollisionAssignment(t *testing.T) {
	key := make([]byte, 32)
	seen := map[string]string{}
	var first, second, synthetic string
	for i := 1; i <= 2000; i++ {
		real := recoveryOriginal(i)
		candidate, err := New(key).Pseudonym("ipv4", real)
		if err != nil {
			t.Fatal("candidate allocation failed")
		}
		if prev, ok := seen[candidate]; ok {
			first, second, synthetic = prev, real, candidate
			break
		}
		seen[candidate] = real
	}
	if first == "" {
		t.Fatal("deterministic collision search found no collision")
	}
	v := New(key)
	if got, err := v.Pseudonym("ipv4", first); err != nil || got != synthetic {
		t.Fatal("collision setup changed")
	}
	path := filepath.Join(t.TempDir(), "vault.bin")
	if err := v.Save(path); err != nil {
		t.Fatal("save failed")
	}
	reopened, err := Open(path, key)
	if err != nil {
		t.Fatal("reopen failed")
	}
	other, err := reopened.Pseudonym("ipv4", second)
	if err != nil || other == synthetic {
		t.Fatal("persisted collision assignment was reused")
	}
	if got, ok := reopened.Real(synthetic); !ok || got != first {
		t.Fatal("original mapping changed after restart")
	}
	// A key alone cannot reconstruct occupied candidates after a lost snapshot.
	lost, err := New(key).Pseudonym("ipv4", second)
	if err != nil || lost != synthetic {
		t.Fatal("loss-window characterization changed")
	}
}

func TestConcurrentVaultGrowthAndReopen(t *testing.T) {
	var results []map[string]any
	for _, count := range []int{1000, 10000} {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		v := New(make([]byte, 32))
		expected := make(map[string]string, count)
		var mu sync.Mutex
		var wg sync.WaitGroup
		start := time.Now()
		for worker := 0; worker < 4; worker++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				for i := worker + 1; i <= count; i += 4 {
					real := recoveryOriginal(i)
					synthetic, err := v.Pseudonym("ipv4", real)
					if err != nil {
						t.Error("growth allocation failed")
						return
					}
					mu.Lock()
					if prior, ok := expected[synthetic]; ok && prior != real {
						t.Error("growth mapping collision")
					}
					expected[synthetic] = real
					mu.Unlock()
				}
			}(worker)
		}
		wg.Wait()
		grow := time.Since(start)
		if v.Len() != count || len(expected) != count {
			t.Fatal("growth count mismatch")
		}
		path := filepath.Join(t.TempDir(), "vault.bin")
		start = time.Now()
		if err := v.Save(path); err != nil {
			t.Fatal("growth save failed")
		}
		save := time.Since(start)
		start = time.Now()
		reopened, err := Open(path, make([]byte, 32))
		if err != nil {
			t.Fatal("growth reopen failed")
		}
		reopen := time.Since(start)
		for synthetic, real := range expected {
			if got, ok := reopened.Real(synthetic); !ok || got != real {
				t.Fatal("reopened mapping mismatch")
			}
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal("snapshot stat failed")
		}
		runtime.ReadMemStats(&after)
		results = append(results, map[string]any{"mappings": count, "callers": 4, "growth_ms": float64(grow) / float64(time.Millisecond), "save_ms": float64(save) / float64(time.Millisecond), "reopen_ms": float64(reopen) / float64(time.Millisecond), "file_bytes": info.Size(), "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "heap_after_bytes": after.HeapAlloc, "exact_mappings_verified": len(expected)})
	}
	if path := os.Getenv("CUI_VAULT_GROWTH_REPORT"); path != "" {
		data, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			t.Fatal("report encoding failed")
		}
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal("report write failed")
		}
	}
}
