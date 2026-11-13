package attachment

import (
	"context"
	"testing"
	"time"
)

func TestExtractionQueueTimeoutIsBusy(t *testing.T) {
	releaseA, _ := helperSlots.Acquire(context.Background(), 1)
	releaseB, _ := helperSlots.Acquire(context.Background(), 1)
	defer releaseA()
	defer releaseB()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	e := New(ctx)
	defer e.Close()
	_, why := e.run("nonexistent-test-tool", nil, 10)
	if why != Busy {
		t.Fatal("queue exhaustion not classified as overload")
	}
}
