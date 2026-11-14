package observe

import (
	"testing"
	"time"
)

func TestCheckpointStageIsClosedAndIndependent(t *testing.T) {
	now := time.Unix(0, 0)
	trace := newTrace(func() time.Time { return now })
	done := trace.Measure(MappingCheckpoint)
	now = now.Add(43 * time.Millisecond)
	done()
	done()
	upstream := trace.Measure(UpstreamWait)
	now = now.Add(7 * time.Millisecond)
	upstream()
	s := trace.Snapshot()
	if s.Duration[MappingCheckpoint] != 43*time.Millisecond || !s.Observed[MappingCheckpoint] {
		t.Fatal("checkpoint span not recorded exactly once")
	}
	if s.Duration[UpstreamWait] != 7*time.Millisecond {
		t.Fatal("checkpoint time included in upstream stage")
	}
	if len(Names) != int(stageCount) || Names[MappingCheckpoint] != "mapping_checkpoint" {
		t.Fatal("closed stage metadata mismatch")
	}
	if s.Observed[AuditWrite] || s.Observed[BodyInspect] {
		t.Fatal("checkpoint invented another observation")
	}
}
