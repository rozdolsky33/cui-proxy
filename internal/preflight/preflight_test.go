package preflight

import (
	"context"
	"cui-proxy/internal/attachment"
	"cui-proxy/internal/config"
	"sync"
	"sync/atomic"
	"testing"
)

func TestServicePolicyAndCachedProbe(t *testing.T) {
	c := New("test-build")
	var calls atomic.Int32
	c.probe = func(context.Context) []attachment.ToolCheck {
		calls.Add(1)
		return []attachment.ToolCheck{{Name: "tesseract", Code: "missing_tool"}}
	}
	cfg := &config.Config{}
	cfg.Policy.Attachments = "extract_text"
	cfg.Policy.Profile = "strict"
	cfg.Policy.OnError = "block"
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := c.Check(context.Background(), cfg)
			if r.Ready || r.Policy.Profile != "strict" || r.Version != "test-build" || r.PID <= 0 {
				t.Error("incorrect service report")
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent checks duplicated probe")
	}
	cfg.Policy.Attachments = "block"
	r := c.Check(context.Background(), cfg)
	if !r.Ready || r.Policy.Attachments != "block" || len(r.Tools) != 0 {
		t.Fatal("effective policy not refreshed")
	}
}
func TestUnavailablePolicyIsNotReady(t *testing.T) {
	if New("dev").Check(context.Background(), nil).Ready {
		t.Fatal("unknown policy reported ready")
	}
}
func TestCancelledProbeWait(t *testing.T) {
	c := New("dev")
	c.gate <- struct{}{}
	defer func() { <-c.gate }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := &config.Config{}
	cfg.Policy.Attachments = "extract_text"
	r := c.Check(ctx, cfg)
	if r.Ready || r.ProbeCode != "probe_timeout" {
		t.Fatal("cancelled preflight not bounded")
	}
}
