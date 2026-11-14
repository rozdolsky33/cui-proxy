package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckpointStageRecordsEveryInvokedOutcome(t *testing.T) {
	for _, mode := range []string{"absent", "success", "failure", "cancel", "panic"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			h := newHarness(t, nil, func(_ *harness, d *Deps) {
				if mode == "absent" {
					return
				}
				d.PersistMappings = func(ctx context.Context) error {
					calls++
					switch mode {
					case "failure":
						return errors.New("injected checkpoint failure")
					case "cancel":
						cancel()
						return ctx.Err()
					case "panic":
						panic("injected checkpoint panic")
					}
					return nil
				}
			})
			defer h.close()
			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil).WithContext(ctx)
			clientRequest := h.req(http.MethodGet, "/v1/models", "")
			req.Header = clientRequest.Header.Clone()
			req.Host = clientRequest.URL.Host
			req.RemoteAddr = "127.0.0.1:12345"
			recorder := httptest.NewRecorder()
			h.px.Config.Handler.ServeHTTP(recorder, req)
			snap := h.reg.Snapshot()
			stage, seen := snap.StagesMS["mapping_checkpoint"]
			expectedCalls := 1
			if mode == "absent" {
				expectedCalls = 0
			}
			if calls != expectedCalls {
				t.Fatalf("callback count %d, want %d; status %d", calls, expectedCalls, recorder.Code)
			}
			if seen != (expectedCalls == 1) {
				t.Fatalf("checkpoint observed=%v, want %v", seen, expectedCalls == 1)
			}
			if seen && stage.Samples != 1 {
				t.Fatalf("checkpoint samples %d, want 1", stage.Samples)
			}
			if snap.ActiveRequests != 0 {
				t.Fatal("request gauge did not drain")
			}
			if mode == "failure" || mode == "cancel" || mode == "panic" {
				h.noUpstreamCall()
				if snap.StagesMS["upstream_wait"].Samples != 0 {
					t.Fatal("failed checkpoint recorded upstream work")
				}
				want := http.StatusServiceUnavailable
				if mode == "panic" {
					want = http.StatusInternalServerError
				}
				if recorder.Code != want {
					t.Fatalf("status %d, want %d", recorder.Code, want)
				}
			}
		})
	}
}
