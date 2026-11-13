package admission

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func waitQueued(t *testing.T, l *Limiter, n int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if l.Snapshot().Queued == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("queued count: got %d, want %d", l.Snapshot().Queued, n)
}

func TestBoundsAndCancellation(t *testing.T) {
	l := New(Limits{Active: 1, Queued: 1, Bytes: 10})
	release, err := l.Acquire(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		r, e := l.Acquire(ctx, 1)
		if r != nil {
			r()
		}
		done <- e
	}()
	waitQueued(t, l, 1)
	if _, err := l.Acquire(context.Background(), 1); !errors.Is(err, ErrFull) {
		t.Fatalf("full: %v", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	release()
	release()
	if s := l.Snapshot(); s != (State{}) {
		t.Fatalf("not drained: %+v", s)
	}
	if _, err := l.Acquire(context.Background(), 11); !errors.Is(err, ErrWeight) {
		t.Fatalf("weight: %v", err)
	}
}

func TestReservedSmallCapacityAndFIFO(t *testing.T) {
	l := New(Limits{Active: 3, Queued: 3, Bytes: 12, SmallBytes: 2, ReservedActive: 1, ReservedBytes: 4})
	first, err := l.Acquire(context.Background(), 8)
	if err != nil {
		t.Fatal(err)
	}
	order := make(chan int, 2)
	finish := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func(i int) {
			r, e := l.Acquire(context.Background(), 8)
			if e != nil {
				t.Error(e)
				return
			}
			order <- i
			<-finish
			r()
		}(i)
		waitQueued(t, l, i+1)
	}
	small, err := l.Acquire(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if s := l.Snapshot(); s.Active != 2 || s.Bytes != 10 || s.Queued != 2 {
		t.Fatalf("state: %+v", s)
	}
	small()
	first()
	if got := <-order; got != 0 {
		t.Fatalf("FIFO first: %d", got)
	}
	finish <- struct{}{}
	if got := <-order; got != 1 {
		t.Fatalf("FIFO second: %d", got)
	}
	close(finish)
}

func TestEligibleBulkProgress(t *testing.T) {
	l := New(Limits{Active: 2, Queued: 4, Bytes: 10, SmallBytes: 2, ReservedActive: 1, ReservedBytes: 2})
	a, _ := l.Acquire(context.Background(), 2)
	b, _ := l.Acquire(context.Background(), 2)
	bulk := make(chan func(), 1)
	go func() { r, _ := l.Acquire(context.Background(), 6); bulk <- r }()
	waitQueued(t, l, 1)
	small := make(chan func(), 1)
	go func() { r, _ := l.Acquire(context.Background(), 1); small <- r }()
	waitQueued(t, l, 2)
	a()
	b()
	select {
	case r := <-bulk:
		r()
	case <-time.After(time.Second):
		t.Fatal("bulk starved")
	}
	select {
	case r := <-small:
		r()
	case <-time.After(time.Second):
		t.Fatal("small starved")
	}
}

func TestGrantCancellationRace(t *testing.T) {
	for range 300 {
		l := New(Limits{Active: 1, Queued: 1, Bytes: 1})
		r, _ := l.Acquire(context.Background(), 1)
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, _ := l.Acquire(ctx, 1)
			if release != nil {
				release()
			}
		}()
		go cancel()
		r()
		wg.Wait()
		if s := l.Snapshot(); s != (State{}) {
			t.Fatalf("leaked: %+v", s)
		}
	}
}

func TestInvalidAndImpossibleReservations(t *testing.T) {
	l := New(Limits{Active: 2, Queued: 1, Bytes: 10, SmallBytes: 2, ReservedActive: 1, ReservedBytes: 4})
	for _, n := range []int64{-1, 7, 11} {
		if _, e := l.Acquire(context.Background(), n); !errors.Is(e, ErrWeight) {
			t.Fatalf("weight %d: %v", n, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := l.Acquire(ctx, 0); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancelled zero: %v", e)
	}
}

func TestBulkWaitStopsBorrowingReservedCapacity(t *testing.T) {
	l := New(Limits{Active: 4, Queued: 4, Bytes: 10, SmallBytes: 2, ReservedActive: 1, ReservedBytes: 2})
	a, _ := l.Acquire(context.Background(), 2)
	b, _ := l.Acquire(context.Background(), 2)
	c, _ := l.Acquire(context.Background(), 2)
	bulk := make(chan func(), 1)
	go func() { r, _ := l.Acquire(context.Background(), 8); bulk <- r }()
	waitQueued(t, l, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	small := make(chan func(), 1)
	go func() { r, _ := l.Acquire(ctx, 2); small <- r }()
	select {
	case r := <-small:
		if r != nil {
			r()
		}
		a()
		b()
		c()
		(<-bulk)()
		t.Fatal("small request borrowed capacity needed by queued bulk head")
	case <-time.After(20 * time.Millisecond):
	}
	a()
	b()
	select {
	case r := <-bulk:
		r()
	case <-time.After(time.Second):
		t.Fatal("bulk did not progress")
	}
	c()
	if r := <-small; r != nil {
		r()
	}
}
