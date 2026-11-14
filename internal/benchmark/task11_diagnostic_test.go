package benchmark

import "testing"

func BenchmarkTask11DetectorCost(b *testing.B) {
	f := load(b)
	for _, d := range f.ds {
		b.Run(d.Name(), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(f.text)))
			for b.Loop() {
				d.Find(f.text)
			}
		})
	}
}
