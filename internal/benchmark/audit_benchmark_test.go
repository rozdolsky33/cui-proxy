package benchmark

import (
	"encoding/binary"
	"path/filepath"
	"sync/atomic"
	"testing"

	"cui-proxy/internal/decisionlog"
)

// BenchmarkAuditDurability includes the production hash-chain append and
// fsync. Use -cpu to control parallel workers; setup and verification are untimed.
func BenchmarkAuditDurability(b *testing.B) {
	for _, parallel := range []bool{false, true} {
		name := "serial"
		if parallel {
			name = "parallel"
		}
		b.Run(name, func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "decisions.jsonl")
			log, err := decisionlog.Open(decisionlog.Options{Path: path, MaxBytes: 1 << 30})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = log.Close() })
			entry := decisionlog.Entry{
				Adapter: "codex", Profile: "benchmark",
				Decision: decisionlog.Sanitized, Counts: map[string]int{"benchmark": 40},
			}
			var sequence atomic.Uint64
			record := func() bool {
				current := entry
				binary.LittleEndian.PutUint64(current.RequestID[:], sequence.Add(1))
				if err := log.Record(current); err != nil {
					b.Error(err)
					return false
				}
				return true
			}
			var workers atomic.Uint64
			if !parallel {
				workers.Store(1)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if parallel {
				b.RunParallel(func(pb *testing.PB) {
					workers.Add(1)
					for pb.Next() {
						if !record() {
							return
						}
					}
				})
			} else {
				for i := 0; i < b.N; i++ {
					if !record() {
						break
					}
				}
			}
			b.StopTimer()
			if err := log.Close(); err != nil {
				b.Fatal(err)
			}
			verified, err := decisionlog.Verify(path)
			if err != nil {
				b.Fatal(err)
			}
			if verified.Records != b.N || len(verified.Breaks) != 0 || len(verified.Gaps) != 0 || verified.Unverified != 0 {
				b.Fatalf("audit evidence incomplete: verified=%d attempted=%d", verified.Records, b.N)
			}
			b.ReportMetric(float64(workers.Load()), "workers")
			b.ReportMetric(float64(verified.Records), "verified_records")
		})
	}
}
