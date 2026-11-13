package ui

import (
	"cui-proxy/internal/audit"
	"cui-proxy/internal/config"
	"cui-proxy/internal/metrics"
	"cui-proxy/internal/observe"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsStageAPIAndCorrelationOff(t *testing.T) {
	r := metrics.New()
	tr := observe.NewTrace()
	tr.Measure(observe.AuditWrite)()
	r.ObserveStages(tr.Snapshot(), 10)
	r.SetCorrelationEnabled(true)
	r.ObserveRecent(r.Identity(http.Header{"Session_id": []string{"valid"}}), tr.Snapshot())
	s := server{d: Deps{Cfg: func() *config.Config { return &config.Config{} }, Metrics: r, Audit: audit.New(1)}}
	w := httptest.NewRecorder()
	s.metrics(w, httptest.NewRequest("GET", "/api/metrics", nil))
	var v map[string]json.RawMessage
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || v["stages_ms"] == nil || v["active_requests"] == nil {
		t.Fatal("API missing stage fields")
	}
	if v["recent_requests"] != nil {
		t.Fatal("disabled records exposed")
	}
	r.SetCorrelationEnabled(false)
	r.SetCorrelationEnabled(true)
	if len(r.Snapshot().RecentRequests) != 0 {
		t.Fatal("disabled history retained")
	}
}
