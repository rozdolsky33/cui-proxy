package attachment

import (
	"context"
	"testing"
	"time"
)

func TestHelperQueueBoundAndCancellation(t *testing.T) {
	a, _ := helperSlots.Acquire(context.Background(), 1)
	b, _ := helperSlots.Acquire(context.Background(), 1)
	defer a()
	defer b()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Cause, 2)
	for range 2 {
		go func() {
			e := New(ctx)
			defer e.Close()
			_, why := e.run("cui-missing-test-helper", nil, 1)
			done <- why
		}()
	}
	deadline := time.Now().Add(time.Second)
	for helperSlots.Snapshot().Queued != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if helperSlots.Snapshot().Queued != 2 {
		t.Fatal("waiters did not queue")
	}
	e := New(context.Background())
	defer e.Close()
	_, why := e.run("cui-missing-test-helper", nil, 1)
	if why != Busy {
		t.Fatalf("full queue cause: %v", why)
	}
	cancel()
	for range 2 {
		if why := <-done; why != Busy {
			t.Fatalf("cancelled waiter: %v", why)
		}
	}
	if s := helperSlots.Snapshot(); s.Queued != 0 || s.Active != 2 {
		t.Fatalf("queue did not drain: %+v", s)
	}
}
