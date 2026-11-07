// Package audit keeps a bounded in-memory log of proxied requests. Entries
// carry metadata only (rule names and counts); there is deliberately no field
// that could hold a real value, body or text.
package audit

import (
	"sync"
	"sync/atomic"
	"time"
)

const subBuffer = 64 // per-subscriber channel buffer

// Entry describes one proxied request.
type Entry struct {
	Time              time.Time
	Adapter, Method   string
	Path              string
	Status            int
	BytesIn, BytesOut int
	Rules             map[string]int
	Blocked           string
	LatencyMS         float64
}

// Log is a fixed-capacity ring buffer with non-blocking pub/sub. Safe for
// concurrent use.
type Log struct {
	mu      sync.Mutex
	buf     []Entry
	start   int // index of oldest
	n       int
	nextID  int
	subs    map[int]chan Entry
	dropped atomic.Uint64
}

// New returns a Log holding at most capacity entries (<=0 stores nothing).
func New(capacity int) *Log {
	if capacity < 0 {
		capacity = 0
	}
	return &Log{buf: make([]Entry, capacity), subs: map[int]chan Entry{}}
}

func cloneEntry(e Entry) Entry {
	if e.Rules != nil {
		m := make(map[string]int, len(e.Rules))
		for k, v := range e.Rules {
			m[k] = v
		}
		e.Rules = m
	}
	return e
}

// Add stores e, evicting the oldest entry when full, and offers it to every
// subscriber without blocking; a full subscriber drops the entry (Dropped).
func (l *Log) Add(e Entry) {
	e = cloneEntry(e)
	l.mu.Lock()
	defer l.mu.Unlock()
	if c := len(l.buf); c > 0 {
		if l.n < c {
			l.buf[(l.start+l.n)%c] = e
			l.n++
		} else {
			l.buf[l.start] = e
			l.start = (l.start + 1) % c
		}
	}
	for _, ch := range l.subs {
		select {
		case ch <- cloneEntry(e):
		default:
			l.dropped.Add(1)
		}
	}
}

// Recent returns a copy of the stored entries, oldest first.
func (l *Log) Recent() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, 0, l.n)
	for i := 0; i < l.n; i++ {
		out = append(out, cloneEntry(l.buf[(l.start+i)%len(l.buf)]))
	}
	return out
}

// Dropped is the total number of entries not delivered to slow subscribers.
func (l *Log) Dropped() uint64 { return l.dropped.Load() }

// Subscribe returns a buffered channel of future entries and an idempotent
// cancel func that unsubscribes and closes the channel exactly once.
func (l *Log) Subscribe() (<-chan Entry, func()) {
	ch := make(chan Entry, subBuffer)
	l.mu.Lock()
	id := l.nextID
	l.nextID++
	l.subs[id] = ch
	l.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			l.mu.Lock()
			delete(l.subs, id)
			l.mu.Unlock()
			close(ch)
		})
	}
}
