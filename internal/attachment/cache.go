package attachment

import (
	"container/list"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type textCacheEntry struct {
	key     [32]byte
	data    []byte
	expires time.Time
	timer   *time.Timer
	element *list.Element
}

// textCache retains only bounded successful OCR output, never original images.
// Clearing removed buffers reduces retention but is not guaranteed secure erasure.
type textCache struct {
	mu                          sync.Mutex
	entries                     map[[32]byte]*textCacheEntry
	lru                         list.List
	bytes, maxEntries, maxBytes int
	ttl                         time.Duration
	closed                      bool
}

func newTextCache(entries, bytes int, ttl time.Duration) *textCache {
	if entries <= 0 || bytes <= 0 || ttl <= 0 {
		panic("attachment: invalid cache bounds")
	}
	return &textCache{entries: make(map[[32]byte]*textCacheEntry), maxEntries: entries, maxBytes: bytes, ttl: ttl}
}

func (c *textCache) get(key [32]byte) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[key]
	if c.closed || e == nil {
		return nil, false
	}
	if !time.Now().Before(e.expires) {
		c.remove(e)
		return nil, false
	}
	c.lru.MoveToFront(e.element)
	return append([]byte(nil), e.data...), true
}

func (c *textCache) put(key [32]byte, data []byte) {
	if c == nil || len(data) == 0 || len(data) > maxText || len(data) > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if old := c.entries[key]; old != nil {
		c.remove(old)
	}
	for len(c.entries) >= c.maxEntries || c.bytes+len(data) > c.maxBytes {
		c.remove(c.lru.Back().Value.(*textCacheEntry))
	}
	e := &textCacheEntry{key: key, data: append([]byte(nil), data...), expires: time.Now().Add(c.ttl)}
	e.element = c.lru.PushFront(e)
	c.entries[key] = e
	c.bytes += len(e.data)
	e.timer = time.AfterFunc(c.ttl, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		// An older expiry must never remove a replacement for the same key.
		if c.entries[key] == e {
			c.remove(e)
		}
	})
}

// remove is called only while holding mu.
func (c *textCache) remove(e *textCacheEntry) {
	delete(c.entries, e.key)
	c.lru.Remove(e.element)
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	c.bytes -= len(e.data)
	clear(e.data)
	e.data = nil
	e.element = nil
}

func (c *textCache) close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for _, e := range c.entries {
		c.remove(e)
	}
}

// imageCacheKey never exposes fingerprints. Failure only disables reuse; the
// normal extraction path still decides whether the helper can run.
func imageCacheKey(input []byte, format, path string, args, environment []string) ([32]byte, bool) {
	var zero [32]byte
	absolute, err := filepath.Abs(path)
	if err != nil {
		return zero, false
	}
	f, err := os.Open(absolute)
	if err != nil {
		return zero, false
	}
	defer f.Close()
	before, err := f.Stat()
	const maxHelperBytes = 32 << 20
	if err != nil || !before.Mode().IsRegular() || before.Size() > maxHelperBytes {
		return zero, false
	}
	executable := sha256.New()
	n, err := io.Copy(executable, io.LimitReader(f, maxHelperBytes+1))
	if err != nil || n != before.Size() || n > maxHelperBytes {
		return zero, false
	}
	after, err := os.Stat(absolute)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		return zero, false
	}
	h := sha256.New()
	write := func(data []byte) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(data)))
		h.Write(length[:])
		h.Write(data)
	}
	write([]byte("cui-image-ocr-cache-v1"))
	write([]byte(format))
	write([]byte(absolute))
	write(executable.Sum(nil))
	write(input)
	for _, values := range [][]string{args, environment} {
		var count [8]byte
		binary.BigEndian.PutUint64(count[:], uint64(len(values)))
		h.Write(count[:])
		for _, v := range values {
			write([]byte(v))
		}
	}
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key, true
}
