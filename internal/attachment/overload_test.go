package attachment

import (
	"context"
	"testing"
	"time"
)

func TestExtractionQueueTimeoutIsBusy(t *testing.T) {
	for i := 0; i < cap(slots); i++ {
		slots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(slots); i++ {
			<-slots
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	e := New(ctx)
	defer e.Close()
	_, why := e.run("nonexistent-test-tool", nil, 10)
	if why != Busy {
		t.Fatal("queue exhaustion not classified as overload")
	}
}
