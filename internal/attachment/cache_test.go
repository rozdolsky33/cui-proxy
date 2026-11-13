package attachment

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func cacheTestKey(n byte) (key [32]byte) { key[0] = n; return }

func TestTextCacheBoundsAndLRU(t *testing.T) {
	c := newTextCache(2, 8, time.Hour)
	defer c.close()
	a, b, d := cacheTestKey(1), cacheTestKey(2), cacheTestKey(3)
	c.put(a, []byte("aaaa"))
	c.put(b, []byte("bbbb"))
	if _, ok := c.get(a); !ok {
		t.Fatal("entry missing")
	}
	c.put(d, []byte("dddd"))
	if _, ok := c.get(b); ok {
		t.Fatal("least recently used entry retained")
	}
	if _, ok := c.get(a); !ok {
		t.Fatal("recently used entry evicted")
	}
	c.put(b, []byte("oversized"))
	if _, ok := c.get(b); ok {
		t.Fatal("oversized output retained")
	}
	c.put(a, []byte("aaaaaa"))
	if _, ok := c.get(d); ok {
		t.Fatal("byte budget exceeded")
	}
	if got, ok := c.get(a); !ok || string(got) != "aaaaaa" {
		t.Fatal("replacement missing")
	}
}

func TestTextCacheOwnsCopiesAndClearsRemovedOutput(t *testing.T) {
	c := newTextCache(1, 8, time.Hour)
	key := cacheTestKey(1)
	data := []byte("abcd")
	c.put(key, data)
	data[0] = 'x'
	got, ok := c.get(key)
	if !ok || string(got) != "abcd" {
		t.Fatal("cache borrowed input")
	}
	got[0] = 'z'
	again, _ := c.get(key)
	if string(again) != "abcd" {
		t.Fatal("cache exposed retained buffer")
	}
	c.mu.Lock()
	retained := c.entries[key].data
	c.mu.Unlock()
	c.put(cacheTestKey(2), []byte("next"))
	if !bytes.Equal(retained, make([]byte, len(retained))) {
		t.Fatal("evicted output not cleared")
	}
	c.mu.Lock()
	retained = c.entries[cacheTestKey(2)].data
	c.mu.Unlock()
	c.close()
	if !bytes.Equal(retained, make([]byte, len(retained))) {
		t.Fatal("closed cache output not cleared")
	}
	c.put(key, []byte("late"))
	if _, ok := c.get(key); ok {
		t.Fatal("closed cache accepted output")
	}
}

func TestTextCacheExpiresWithoutLookup(t *testing.T) {
	c := newTextCache(2, 8, 10*time.Millisecond)
	defer c.close()
	key := cacheTestKey(1)
	c.put(key, []byte("abcd"))
	deadline := time.Now().Add(time.Second)
	for {
		c.mu.Lock()
		count := len(c.entries)
		used := c.bytes
		c.mu.Unlock()
		if count == 0 && used == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle cache retained expired output")
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok := c.get(key); ok {
		t.Fatal("expired entry returned")
	}
}

func TestTextCacheConcurrentBounds(t *testing.T) {
	c := newTextCache(4, 16, time.Hour)
	defer c.close()
	var wg sync.WaitGroup
	for worker := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 100 {
				key := cacheTestKey(byte(worker*7 + i))
				c.put(key, []byte("data"))
				c.get(key)
			}
		}()
	}
	wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) > 4 || c.bytes > 16 || c.bytes < 0 {
		t.Fatal("cache exceeded bounds")
	}
}

func TestImageCacheIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(path, []byte("version-one"), 0700); err != nil {
		t.Fatal(err)
	}
	input := []byte("image-content")
	args := []string{"stdin", "stdout", "-l", "eng"}
	env := []string{"LC_ALL=C"}
	first, ok := imageCacheKey(input, "png", path, args, env)
	if !ok {
		t.Fatal("identity unavailable")
	}
	same, ok := imageCacheKey(input, "png", path, args, env)
	if !ok || first != same {
		t.Fatal("stable identity changed")
	}
	variants := []struct {
		input        []byte
		format, path string
		args, env    []string
	}{
		{[]byte("other-content"), "png", path, args, env},
		{input, "jpeg", path, args, env},
		{input, "png", path, []string{"stdin", "stdout", "-l", "other"}, env},
		{input, "png", path, args, []string{"LC_ALL=other"}},
	}
	other := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(other, []byte("version-one"), 0700); err != nil {
		t.Fatal(err)
	}
	variants = append(variants, struct {
		input        []byte
		format, path string
		args, env    []string
	}{input, "png", other, args, env})
	for _, v := range variants {
		key, ok := imageCacheKey(v.input, v.format, v.path, v.args, v.env)
		if !ok || key == first {
			t.Fatal("identity omitted an extraction input")
		}
	}
	if err := os.WriteFile(path, []byte("version-two"), 0700); err != nil {
		t.Fatal(err)
	}
	changed, ok := imageCacheKey(input, "png", path, args, env)
	if !ok || first == changed {
		t.Fatal("executable content change reused identity")
	}
}

func TestImageCacheIdentityFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper")
	if _, ok := imageCacheKey(nil, "png", path, nil, nil); ok {
		t.Fatal("missing helper accepted")
	}
	if err := os.WriteFile(path, []byte("version"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, (32<<20)+1); err != nil {
		t.Fatal(err)
	}
	if _, ok := imageCacheKey(nil, "png", path, nil, nil); ok {
		t.Fatal("oversized helper fingerprinted")
	}
	if _, ok := imageCacheKey(nil, "png", filepath.Dir(path), nil, nil); ok {
		t.Fatal("directory fingerprinted")
	}
}

func TestImageCacheIdentityUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read mode-zero files")
	}
	path := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(path, []byte("version"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0700)
	if _, ok := imageCacheKey(nil, "png", path, nil, nil); ok {
		t.Fatal("unreadable helper fingerprinted")
	}
}

func TestImageCacheIdentityRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper")
	if err := syscall.Mkfifo(path, 0700); err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 1)
	go func() { _, ok := imageCacheKey(nil, "png", path, nil, nil); done <- ok }()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("nonregular helper accepted")
		}
	case <-time.After(time.Second):
		// Release a blocked reader so even the failing regression leaves no goroutine.
		f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("blocked fingerprint reader did not clean up")
		}
		t.Fatal("fingerprinting blocked on a nonregular helper")
	}
}
