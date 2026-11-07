// Package ui serves the embedded localhost dashboard. It is a read-mostly
// surface that must never show real values: it exposes only rule names,
// counts, paths and block-reason codes, never vault, dictionary or header
// contents. Every request is checked against DNS rebinding (Host) and
// cross-origin POSTs (Content-Type, Origin, Sec-Fetch-Site).
package ui

import (
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"cui-proxy/internal/audit"
	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/metrics"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

//go:embed static
var staticFS embed.FS

// keepaliveInterval is how often /api/stream sends an SSE comment. A var so
// tests can shorten it.
var keepaliveInterval = 15 * time.Second

const (
	maxTestBody   = 1 << 20
	maxReloadErr  = 2048
	csp           = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
	jsonType      = "application/json"
	errUnresolved = "text could not be sanitized reversibly"
)

// Deps are the live pieces the dashboard reads. Cfg and Detectors are
// functions so a reload is visible without rebuilding the handler.
type Deps struct {
	Cfg       func() *config.Config
	Metrics   *metrics.Registry
	Audit     *audit.Log
	Detectors func() []detect.Detector // current rule set, for /api/test
	Reload    func() error
	Version   string
}

type server struct{ d Deps }

// New returns the dashboard handler.
func New(d Deps) http.Handler {
	s := &server{d: d}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.static("static/index.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /app.js", s.static("static/app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /style.css", s.static("static/style.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /api/config", s.config)
	mux.HandleFunc("GET /api/metrics", s.metrics)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/stream", s.stream)
	mux.HandleFunc("POST /api/test", s.test)
	mux.HandleFunc("POST /api/reload", s.reload)
	return guard(mux)
}

// guard sets the security headers on every response and applies the Host and
// state-changing-request checks before routing.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Frame-Options", "DENY")
		if !loopbackHost(r.Host) {
			fail(w, http.StatusMisdirectedRequest, "host not allowed")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !isJSON(r.Header.Get("Content-Type")) {
				fail(w, http.StatusUnsupportedMediaType, "content type must be application/json")
				return
			}
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) {
					fail(w, http.StatusForbidden, "cross-origin request refused")
					return
				}
			}
			if sf := r.Header.Get("Sec-Fetch-Site"); sf != "" && sf != "same-origin" && sf != "none" {
				fail(w, http.StatusForbidden, "cross-site request refused")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// loopbackHost reports whether a Host header names localhost or a loopback IP
// literal (any port).
func loopbackHost(hostport string) bool {
	if hostport == "" {
		return false
	}
	host := hostport
	if hh, _, err := net.SplitHostPort(hostport); err == nil {
		host = hh
	} else {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isJSON(ct string) bool {
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil || mt != jsonType {
		return false
	}
	for k, v := range params {
		if k != "charset" || !strings.EqualFold(v, "utf-8") {
			return false
		}
	}
	return true
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		code, b = http.StatusInternalServerError, []byte(`{"error":"internal error"}`)
	}
	w.Header().Set("Content-Type", jsonType)
	w.WriteHeader(code)
	_, _ = w.Write(b)
}

func (s *server) static(name, ctype string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := staticFS.ReadFile(name)
		if err != nil {
			fail(w, http.StatusInternalServerError, "internal error")
			return
		}
		w.Header().Set("Content-Type", ctype)
		_, _ = w.Write(b)
	}
}

type ruleView struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Pattern  string `json:"pattern,omitempty"`
	Validate string `json:"validate,omitempty"`
	Synth    string `json:"synth,omitempty"`
	File     string `json:"file,omitempty"` // dictionary file PATH only
}

type configView struct {
	Version   string            `json:"version"`
	Listen    string            `json:"listen"`
	UIListen  string            `json:"ui_listen"`
	Upstreams map[string]string `json:"upstreams"`
	Policy    struct {
		OnError      string `json:"on_error"`
		MaxBodyBytes int64  `json:"max_body_bytes"`
	} `json:"policy"`
	Vault struct {
		Mode    string `json:"mode"`
		KeyFile string `json:"key_file,omitempty"` // path only; key bytes are never read
		File    string `json:"file,omitempty"`
	} `json:"vault"`
	Rules []ruleView `json:"rules"`
}

func (s *server) config(w http.ResponseWriter, r *http.Request) {
	v := configView{Version: s.d.Version, Rules: []ruleView{}, Upstreams: map[string]string{}}
	if c := s.d.Cfg(); c != nil {
		v.Listen, v.UIListen = c.Listen, c.UI.Listen
		for k, u := range c.Upstreams {
			v.Upstreams[k] = u
		}
		v.Policy.OnError, v.Policy.MaxBodyBytes = c.Policy.OnError, c.Policy.MaxBodyBytes
		v.Vault.Mode, v.Vault.KeyFile, v.Vault.File = c.Vault.Mode, c.Vault.KeyFile, c.Vault.File
		for _, ru := range c.Rules {
			v.Rules = append(v.Rules, ruleView{ru.Name, ru.Type, ru.Pattern, ru.Validate, ru.Synth, ru.File})
		}
	}
	writeJSON(w, 200, v)
}

func (s *server) metrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.d.Metrics.Snapshot())
}

type eventView struct {
	Time      string         `json:"time"`
	Adapter   string         `json:"adapter"`
	Method    string         `json:"method"`
	Path      string         `json:"path"`
	Status    int            `json:"status"`
	BytesIn   int            `json:"bytes_in"`
	BytesOut  int            `json:"bytes_out"`
	Rules     map[string]int `json:"rules"`
	Blocked   string         `json:"blocked"`
	LatencyMS float64        `json:"latency_ms"`
}

func toView(e audit.Entry) eventView {
	rules := e.Rules
	if rules == nil {
		rules = map[string]int{}
	}
	return eventView{e.Time.UTC().Format(time.RFC3339Nano), e.Adapter, e.Method, e.Path, e.Status, e.BytesIn, e.BytesOut, rules, e.Blocked, e.LatencyMS}
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	rec := s.d.Audit.Recent()
	out := make([]eventView, 0, len(rec))
	for _, e := range rec {
		out = append(out, toView(e))
	}
	writeJSON(w, 200, out)
}

func (s *server) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	ch, cancel := s.d.Audit.Subscribe()
	defer cancel()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	tick := time.NewTicker(keepaliveInterval)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
		case e, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(toView(e))
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
		}
		fl.Flush()
	}
}

// collector is a transform.Sink that aggregates counts per rule and class.
type collector struct{ m map[[2]string]int }

func (c *collector) Record(e transform.Event) { c.m[[2]string{e.Rule, e.Class}] += e.Count }

type matchView struct {
	Rule  string `json:"rule"`
	Class string `json:"class"`
	Count int    `json:"count"`
}

func (s *server) test(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTestBody)
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			fail(w, http.StatusRequestEntityTooLarge, "text too large")
			return
		}
		fail(w, http.StatusBadRequest, "malformed request")
		return
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	col := &collector{m: map[[2]string]int{}}
	san := transform.NewSanitizer(s.d.Detectors(), vault.New(key), col) // throwaway vault: live vault and metrics untouched
	out, err := san.Text(req.Text)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, errUnresolved)
		return
	}
	ms := make([]matchView, 0, len(col.m))
	for k, n := range col.m {
		ms = append(ms, matchView{k[0], k[1], n})
	}
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].Rule != ms[j].Rule {
			return ms[i].Rule < ms[j].Rule
		}
		return ms[i].Class < ms[j].Class
	})
	writeJSON(w, 200, map[string]any{"sanitized": out, "matches": ms})
}

func (s *server) reload(w http.ResponseWriter, r *http.Request) {
	if s.d.Reload == nil {
		fail(w, http.StatusNotImplemented, "reload unavailable")
		return
	}
	if err := s.d.Reload(); err != nil {
		msg := err.Error()
		if len(msg) > maxReloadErr {
			msg = strings.ToValidUTF8(msg[:maxReloadErr], "")
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"ok": false, "error": msg})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
