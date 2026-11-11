package metrics

import runtimemetrics "runtime/metrics"

// RuntimeStats describes this Go process, including dashboard work. Child
// extractors and total OS RSS are measured separately. LiveHeapBytes is the
// live heap at the most recent GC, not an implicit request to run a GC.
type RuntimeStats struct {
	Available        bool   `json:"available"`
	HeapObjectsBytes uint64 `json:"heap_objects_bytes"`
	LiveHeapBytes    uint64 `json:"live_heap_bytes"`
	Goroutines       uint64 `json:"goroutines"`
	GCCycles         uint64 `json:"gc_cycles"`
}

func runtimeSnapshot() RuntimeStats {
	samples := []runtimemetrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/gc/heap/live:bytes"},
		{Name: "/sched/goroutines:goroutines"},
		{Name: "/gc/cycles/total:gc-cycles"},
	}
	runtimemetrics.Read(samples)
	for _, s := range samples {
		if s.Value.Kind() != runtimemetrics.KindUint64 {
			return RuntimeStats{}
		}
	}
	return RuntimeStats{Available: true, HeapObjectsBytes: samples[0].Value.Uint64(),
		LiveHeapBytes: samples[1].Value.Uint64(), Goroutines: samples[2].Value.Uint64(), GCCycles: samples[3].Value.Uint64()}
}
