// Package admission bounds admitted work without retaining request content.
package admission

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrFull   = errors.New("admission queue full")
	ErrWeight = errors.New("admission reservation cannot fit")
)

type Limits struct {
	Active, Queued    int
	Bytes, SmallBytes int64
	ReservedActive    int
	ReservedBytes     int64
}

type State struct {
	Active int   `json:"active"`
	Queued int   `json:"queued"`
	Bytes  int64 `json:"reserved_input_bytes"`
}

type waiter struct {
	bytes   int64
	lane    int
	ready   chan struct{}
	granted bool
}

type Limiter struct {
	mu         sync.Mutex
	limits     Limits
	state      State
	bulkActive int
	bulkBytes  int64
	queue      [2][]*waiter
	next       int
}

// Requests is shared by all proxy handlers in this process. The byte budget
// bounds reserved input, not total heap/RSS or child-process memory.
var Requests = New(Limits{Active: 8, Queued: 8, Bytes: 64 << 20,
	SmallBytes: 2 << 20, ReservedActive: 2, ReservedBytes: 8 << 20})

func New(l Limits) *Limiter {
	if l.Active <= 0 || l.Queued < 0 || l.Bytes <= 0 || l.SmallBytes < 0 || l.SmallBytes > l.Bytes ||
		l.ReservedActive < 0 || l.ReservedActive >= l.Active || l.ReservedBytes < 0 || l.ReservedBytes >= l.Bytes {
		panic("admission: invalid limits")
	}
	return &Limiter{limits: l}
}

func (l *Limiter) Snapshot() State {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state
}

func (l *Limiter) Acquire(ctx context.Context, bytes int64) (func(), error) {
	w := &waiter{bytes: bytes, ready: make(chan struct{})}
	if bytes > l.limits.SmallBytes {
		w.lane = 1
	}
	if bytes < 0 || bytes > l.limits.Bytes || (w.lane == 1 && bytes > l.limits.Bytes-l.limits.ReservedBytes) {
		return nil, ErrWeight
	}
	l.mu.Lock()
	if err := ctx.Err(); err != nil {
		l.mu.Unlock()
		return nil, err
	}
	if len(l.queue[w.lane]) == 0 && l.fits(w) {
		l.grant(w)
	} else {
		if l.state.Queued >= l.limits.Queued {
			l.mu.Unlock()
			return nil, ErrFull
		}
		l.queue[w.lane] = append(l.queue[w.lane], w)
		l.state.Queued++
	}
	l.mu.Unlock()
	select {
	case <-w.ready:
	case <-ctx.Done():
	}
	l.mu.Lock()
	if err := ctx.Err(); err != nil {
		if w.granted {
			l.release(w)
		} else {
			q := l.queue[w.lane]
			for i, v := range q {
				if v == w {
					copy(q[i:], q[i+1:])
					q[len(q)-1] = nil
					l.queue[w.lane] = q[:len(q)-1]
					l.state.Queued--
					break
				}
			}
		}
		l.drain()
		l.mu.Unlock()
		return nil, err
	}
	l.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { l.mu.Lock(); defer l.mu.Unlock(); l.release(w); l.drain() }) }, nil
}

func (l *Limiter) fits(w *waiter) bool {
	if l.state.Active >= l.limits.Active || w.bytes > l.limits.Bytes-l.state.Bytes {
		return false
	}
	// Stop lending the general pool to small arrivals while a bulk head waits.
	// Otherwise a steady stream of small grants can keep a large reservation
	// from ever fitting. Existing borrowers drain; reserved small work continues.
	if w.lane == 0 && len(l.queue[1]) > 0 {
		if l.state.Active-l.bulkActive >= l.limits.ReservedActive || w.bytes > l.limits.ReservedBytes-(l.state.Bytes-l.bulkBytes) {
			return false
		}
	}
	return w.lane == 0 || (l.bulkActive < l.limits.Active-l.limits.ReservedActive && w.bytes <= l.limits.Bytes-l.limits.ReservedBytes-l.bulkBytes)
}

func (l *Limiter) grant(w *waiter) {
	l.state.Active++
	l.state.Bytes += w.bytes
	if w.lane == 1 {
		l.bulkActive++
		l.bulkBytes += w.bytes
	}
	w.granted = true
	close(w.ready)
}

func (l *Limiter) release(w *waiter) {
	l.state.Active--
	l.state.Bytes -= w.bytes
	if w.lane == 1 {
		l.bulkActive--
		l.bulkBytes -= w.bytes
	}
}

func (l *Limiter) drain() {
	for {
		lane := -1
		for _, n := range []int{l.next, 1 - l.next} {
			if len(l.queue[n]) > 0 && l.fits(l.queue[n][0]) {
				lane = n
				break
			}
		}
		if lane < 0 {
			return
		}
		q := l.queue[lane]
		w := q[0]
		q[0] = nil
		l.queue[lane] = q[1:]
		l.state.Queued--
		l.grant(w)
		l.next = 1 - lane
	}
}
