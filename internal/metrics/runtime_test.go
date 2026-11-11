package metrics

import (
	"runtime"
	"testing"
)

func TestRuntimeSnapshotMeasuresLiveProcess(t *testing.T) {
	r := New()
	before := r.Snapshot().Runtime
	retained := make([]byte, 16<<20)
	for i := range retained {
		retained[i] = byte(i)
	}
	runtime.GC()
	after := r.Snapshot().Runtime
	runtime.KeepAlive(retained)
	if !after.Available {
		t.Fatal("runtime measurements unavailable")
	}
	if after.LiveHeapBytes < uint64(len(retained)) || after.HeapObjectsBytes < uint64(len(retained)) {
		t.Fatal("retained heap not measured")
	}
	if after.GCCycles <= before.GCCycles {
		t.Fatal("GC cycle count did not advance")
	}
	if after.Goroutines == 0 {
		t.Fatal("goroutines missing")
	}
}
