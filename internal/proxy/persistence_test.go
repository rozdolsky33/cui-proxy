package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestPersistenceFailureBlocksEveryForwardingRoute(t *testing.T) {
	for _, path := range []string{"/v1/models", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			calls := 0
			h := newHarness(t, nil, func(_ *harness, d *Deps) {
				d.PersistMappings = func(context.Context) error { calls++; return errors.New("injected storage failure") }
			})
			method, body := http.MethodGet, ""
			if path == "/v1/responses" {
				method = http.MethodPost
				body = userBody("hello")
			}
			req := h.req(method, path, body)
			metadata, _ := json.Marshal(map[string]string{"cwd": realIP})
			req.Header.Set("X-Codex-Turn-Metadata", string(metadata))
			res, b := h.do(req)
			assertBlocked(t, h, res, b, http.StatusServiceUnavailable, "vault")
			h.noUpstreamCall()
			if calls != 1 {
				t.Fatalf("persistence callbacks: %d", calls)
			}
			if h.set().v.Len() == 0 {
				t.Fatal("metadata mapping was not exercised")
			}
		})
	}
}
