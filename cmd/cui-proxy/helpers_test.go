package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cui-proxy/internal/sse"
)

// Every wait is bounded by waitLimit only so that a broken build fails
// instead of hanging; synchronization itself uses channels.
const waitLimit = 10 * time.Second

// Real values seeded into requests. Only these literals are asserted on.
const (
	realIP   = "10.20.30.40"
	realOCID = "ocid1.instance.oc1.phx.anyhqljrabcdefghijklmnop"
	hostA    = "prod-db.agency.internal"
	hostB    = "billing.agency.internal"
)

// ---- logs ----

type logBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuf) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.b, format+"\n", args...)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// ---- config files ----

// cfgSpec renders a test config. Empty fields take defaults.
type cfgSpec struct {
	Listen, UIListen string
	Upstream         string
	OnError          string
	Vault            string // the full vault: line value
	Extra            string // extra top-level YAML lines
	Rules            string // the rules: list body
}

const rulesA = `  - name: builtin
    type: builtin
  - name: hosts-a
    type: dictionary
    file: a.txt
    synth: hostname
`

const rulesB = `  - name: builtin
    type: builtin
  - name: hosts-b
    type: dictionary
    file: b.txt
    synth: hostname
`

func (c cfgSpec) render() string {
	def := func(v, d string) string {
		if v == "" {
			return d
		}
		return v
	}
	return fmt.Sprintf(`listen: %s
ui: { listen: %s }
upstreams:
  codex: %s
policy: { on_error: %s, max_body_bytes: 1048576 }
vault: %s
%s
rules:
%s`, def(c.Listen, "127.0.0.1:0"), def(c.UIListen, "127.0.0.1:0"), def(c.Upstream, "http://127.0.0.1:1/codex"),
		def(c.OnError, "block"), def(c.Vault, "{ mode: memory }"), c.Extra, def(c.Rules, rulesA))
}

func writeFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// testDir creates a config directory with the two dictionaries and returns
// it with the config path (written from spec).
func testDir(t testing.TB, spec cfgSpec) (dir, cfgPath string) {
	t.Helper()
	dir = t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "# hosts A\n"+hostA+"\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "# hosts B\n"+hostB+"\n")
	cfgPath = filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, spec.render())
	return dir, cfgPath
}

// ---- fake upstream ----

// upstream is a fake Codex backend. It records the user text it received
// and echoes it back as one output_text delta, so a client that receives
// the original text proves restoration.
type upstream struct {
	srv   *httptest.Server
	texts chan string
	gate  chan struct{} // when non-nil, the stream waits for it after response.created
	hang  bool          // keep the stream open until the client goes away
}

func newUpstream(t *testing.T, mod func(u *upstream)) *upstream {
	t.Helper()
	u := &upstream{texts: make(chan string, 32)}
	if mod != nil {
		mod(u)
	}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/codex/responses" {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Input []struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"input"`
		}
		_ = json.Unmarshal(b, &req)
		text := ""
		if len(req.Input) > 0 && len(req.Input[0].Content) > 0 {
			text = req.Input[0].Content[0].Text
		}
		u.texts <- text
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		writeEvent(w, "response.created", `{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`)
		if u.gate != nil {
			select {
			case <-u.gate:
			case <-r.Context().Done():
				return
			}
		}
		if u.hang {
			<-r.Context().Done()
			return
		}
		writeEvent(w, "response.output_text.delta", textDelta(text))
		writeEvent(w, "response.completed", `{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *upstream) url() string { return u.srv.URL + "/codex" }

// next returns the next text the upstream received.
func (u *upstream) next(t *testing.T) string {
	t.Helper()
	select {
	case s := <-u.texts:
		return s
	case <-time.After(waitLimit):
		t.Fatal("upstream received no request")
		return ""
	}
}

func writeEvent(w http.ResponseWriter, name, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
	w.(http.Flusher).Flush()
}

func textDelta(d string) string {
	b, _ := json.Marshal(d)
	return `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":` + string(b) + `}`
}

func userBody(text string) string {
	b, _ := json.Marshal(text)
	return `{"model":"gpt-5-codex","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":` + string(b) + `}]}]}`
}

var testClient = &http.Client{Timeout: waitLimit}

// ask sends text through the proxy and returns the status and the
// concatenated output_text deltas of the response.
func ask(t *testing.T, proxyAddr, text string) (int, string) {
	t.Helper()
	res, err := testClient.Post("http://"+proxyAddr+"/v1/responses", "application/json", strings.NewReader(userBody(text)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, deltas(t, b)
}

func deltas(t *testing.T, b []byte) string {
	t.Helper()
	rd := sse.NewReader(bytes.NewReader(b))
	var out strings.Builder
	for {
		e, err := rd.Next()
		if errors.Is(err, io.EOF) {
			return out.String()
		}
		if err != nil {
			t.Fatal(err)
		}
		if e.Name != "response.output_text.delta" {
			continue
		}
		var d struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}
		out.WriteString(d.Delta)
	}
}

// ---- app ----

// startApp builds and starts an App; it is closed at test end.
func startApp(t *testing.T, cfgPath string, logs *logBuf, mod func(*AppOptions)) *App {
	t.Helper()
	opts := AppOptions{ConfigPath: cfgPath, Version: "test", Logf: logs.logf}
	if mod != nil {
		mod(&opts)
	}
	a, err := NewApp(opts)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(a.Close)
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return a
}

func assertNoValues(t *testing.T, where, s string, values ...string) {
	t.Helper()
	for _, v := range values {
		if strings.Contains(s, v) {
			t.Errorf("%s contains a real value %q", where, v)
		}
	}
}
