// clientprobe runs an installed Codex client against a local stand-in for the
// model service and records the structure of what the client sends. It makes
// no request to a real model service and needs no account.
//
// The client is run twice with the same fabricated prompt: once pointed
// directly at the stand-in, and once through a cui-proxy binary whose upstream
// is the stand-in. The probe then checks that the proxy forwarded every route
// the client used with the body structure unchanged, that the fabricated
// value never reached the stand-in in the proxied run, and that the client
// still printed the original value.
//
// Output is structural: routes, header names, JSON key paths and protocol
// discriminator values. Request content is never written.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"cui-proxy/internal/clientprobe"
)

// A fabricated private address. The built-in rules replace it.
const original = "10.20.30.15"

// execTool matches the definition of the client's free-form exec tool.
var execTool = regexp.MustCompile(`"name"\s*:\s*"exec"`)

var echoRE = regexp.MustCompile(`host=([0-9A-Za-z.:-]+)`)

type record struct {
	Client  string              `json:"client"`
	Routes  []string            `json:"routes"`
	Headers map[string][]string `json:"header_names"`
	Shapes  map[string][]string `json:"request_shapes"`
}

type standIn struct {
	mu       sync.Mutex
	routes   map[string]int
	headers  map[string]map[string]struct{}
	shapes   map[string][]string
	sawValue bool
	turns    int
	toolSent bool // a tool call was sent to the client
	toolBack bool // the client sent a tool output in a later request
	problems []string
}

func newStandIn() *standIn {
	return &standIn{routes: map[string]int{}, headers: map[string]map[string]struct{}{}, shapes: map[string][]string{}}
}

func (s *standIn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	route := r.Method + " " + r.URL.Path
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[route]++
	if s.headers[route] == nil {
		s.headers[route] = map[string]struct{}{}
	}
	for name := range r.Header {
		s.headers[route][strings.ToLower(name)] = struct{}{}
	}
	if err != nil {
		s.problems = append(s.problems, route+": read failed")
	}
	if bytes.Contains(body, []byte(original)) {
		s.sawValue = true
	}
	var shape []string
	if len(body) > 0 {
		if shape, err = clientprobe.Shape(body); err != nil {
			s.problems = append(s.problems, route+": body is not one JSON document")
		} else {
			s.shapes[route] = clientprobe.Merge(s.shapes[route], shape)
		}
	}
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/responses"):
		s.turns++
		value := original
		if m := echoRE.FindAllSubmatch(body, -1); len(m) > 0 {
			value = string(m[len(m)-1][1])
		}
		offersExec, hasOutput := execTool.Match(body), false
		for _, e := range shape {
			hasOutput = hasOutput || strings.HasSuffix(e, "input[].type=custom_tool_call_output")
		}
		s.toolBack = s.toolBack || hasOutput
		if offersExec && !s.toolSent {
			s.toolSent = true
			writeStream(w, s.turns, "host="+value, true)
			return
		}
		writeStream(w, s.turns, "host="+value, false)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models"):
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"object":"list","data":[]}`)
	default:
		http.Error(w, `{"error":{"message":"not provided by the stand-in"}}`, http.StatusNotFound)
	}
}

// writeStream sends a minimal Responses stream with one output item: an
// assistant message, or a call to the client's free-form exec tool. The text
// is split inside the value so that restoration has to hold back.
func writeStream(w http.ResponseWriter, turn int, text string, tool bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	id, item := fmt.Sprintf("resp_probe%d", turn), fmt.Sprintf("item_probe%d", turn)
	cut := len(text) - 3
	at := func(e map[string]any) map[string]any {
		e["item_id"], e["output_index"] = item, 0
		return e
	}
	var start, done map[string]any
	var middle []map[string]any
	if tool {
		call := fmt.Sprintf("call_probe%d", turn)
		start = map[string]any{"id": item, "type": "custom_tool_call", "status": "in_progress", "call_id": call, "name": "exec", "input": ""}
		done = map[string]any{"id": item, "type": "custom_tool_call", "status": "completed", "call_id": call, "name": "exec", "input": text}
		middle = []map[string]any{
			at(map[string]any{"type": "response.custom_tool_call_input.delta", "delta": text[:cut]}),
			at(map[string]any{"type": "response.custom_tool_call_input.delta", "delta": text[cut:]}),
			at(map[string]any{"type": "response.custom_tool_call_input.done", "input": text}),
		}
	} else {
		part := func(t string) map[string]any {
			return map[string]any{"type": "output_text", "text": t, "annotations": []any{}}
		}
		start = map[string]any{"id": item, "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}
		done = map[string]any{"id": item, "type": "message", "role": "assistant", "status": "completed", "content": []any{part(text)}}
		middle = []map[string]any{
			at(map[string]any{"type": "response.content_part.added", "content_index": 0, "part": part("")}),
			at(map[string]any{"type": "response.output_text.delta", "content_index": 0, "delta": text[:cut]}),
			at(map[string]any{"type": "response.output_text.delta", "content_index": 0, "delta": text[cut:]}),
			at(map[string]any{"type": "response.output_text.done", "content_index": 0, "text": text}),
			at(map[string]any{"type": "response.content_part.done", "content_index": 0, "part": part(text)}),
		}
	}
	usage := map[string]any{"input_tokens": 1, "input_tokens_details": map[string]any{"cached_tokens": 0},
		"output_tokens": 1, "output_tokens_details": map[string]any{"reasoning_tokens": 0}, "total_tokens": 2}
	events := []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": id, "object": "response", "status": "in_progress", "output": []any{}}},
		{"type": "response.output_item.added", "output_index": 0, "item": start},
	}
	events = append(events, middle...)
	events = append(events,
		map[string]any{"type": "response.output_item.done", "output_index": 0, "item": done},
		map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "object": "response", "status": "completed", "output": []any{done}, "usage": usage}},
	)
	for i, e := range events {
		e["sequence_number"] = i
		data, _ := json.Marshal(e)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e["type"], data)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func (s *standIn) record(client string) record {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := record{Client: client, Headers: map[string][]string{}, Shapes: s.shapes}
	for route := range s.routes {
		rec.Routes = append(rec.Routes, route)
		for h := range s.headers[route] {
			rec.Headers[route] = append(rec.Headers[route], h)
		}
		sort.Strings(rec.Headers[route])
	}
	sort.Strings(rec.Routes)
	return rec
}

func serve(h http.Handler) (addr string, stop func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	return ln.Addr().String(), func() { srv.Close() }, nil
}

func freeAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	return ln.Addr().String(), nil
}

// runClient runs one non-interactive Codex turn against base and returns
// whether the client exited cleanly and printed the original value. The
// client gets an empty home directory and a placeholder key, so it uses no
// saved login, user configuration or tool servers.
func runClient(ctx context.Context, codex, base, dir string) (ok, printed bool, err error) {
	home, work, last := filepath.Join(dir, "home"), filepath.Join(dir, "work"), filepath.Join(dir, "last.txt")
	for _, d := range []string{home, work} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return false, false, err
		}
	}
	provider := fmt.Sprintf(`model_providers.probe={name="probe",base_url="http://%s/v1",wire_api="responses",env_key="CLIENTPROBE_KEY",requires_openai_auth=false,supports_websockets=false}`, base)
	cmd := exec.CommandContext(ctx, codex, "exec", "--skip-git-repo-check", "--ephemeral", "-C", work,
		"-c", `model_provider="probe"`, "-c", provider, "-o", last,
		"Reply with exactly this line: host="+original)
	cmd.Env = append(os.Environ(), "CODEX_HOME="+home, "CLIENTPROBE_KEY=placeholder-not-a-credential")
	out, runErr := cmd.CombinedOutput()
	if werr := os.WriteFile(filepath.Join(dir, "client-output.txt"), out, 0o600); werr != nil {
		return false, false, werr
	}
	text, _ := os.ReadFile(last)
	return runErr == nil, bytes.Contains(text, []byte("host="+original)), nil
}

func startProxy(ctx context.Context, bin, upstream, dir string) (addr string, stop func(), err error) {
	addr, err = freeAddr()
	if err != nil {
		return "", nil, err
	}
	ui, err := freeAddr()
	if err != nil {
		return "", nil, err
	}
	cfg := filepath.Join(dir, "proxy.yaml")
	yaml := fmt.Sprintf("listen: %s\nui: { listen: %s }\nupstreams:\n  codex: http://%s/v1\npolicy: { on_error: block }\nvault: { mode: memory }\nrules:\n  - name: builtin\n    type: builtin\n", addr, ui, upstream)
	if err := os.WriteFile(cfg, []byte(yaml), 0o600); err != nil {
		return "", nil, err
	}
	logf, err := os.Create(filepath.Join(dir, "proxy-output.txt"))
	if err != nil {
		return "", nil, err
	}
	cmd := exec.CommandContext(ctx, bin, "serve", "-config", cfg)
	cmd.Stdout, cmd.Stderr, cmd.Dir = logf, logf, dir
	if err := cmd.Start(); err != nil {
		logf.Close()
		return "", nil, err
	}
	stop = func() { cmd.Process.Kill(); cmd.Wait(); logf.Close() }
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			c.Close()
			return addr, stop, nil
		}
	}
	stop()
	return "", nil, errors.New("proxy did not start listening; see proxy-output.txt")
}

func clientVersion(ctx context.Context, codex string) (string, error) {
	out, err := exec.CommandContext(ctx, codex, "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func run() (failed bool, err error) {
	codex := flag.String("codex", "codex", "Codex client to run")
	proxy := flag.String("proxy", "bin/cui-proxy", "cui-proxy binary to test")
	baseline := flag.String("baseline", "", "recorded shape file to compare with")
	write := flag.String("write", "", "write the recorded shape to this file")
	keep := flag.String("keep", "", "directory for client and proxy output (default: a temporary directory, removed)")
	timeout := flag.Duration("timeout", 3*time.Minute, "limit for the whole probe")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	dir := *keep
	if dir == "" {
		if dir, err = os.MkdirTemp("", "clientprobe-"); err != nil {
			return false, err
		}
		defer os.RemoveAll(dir)
	}
	bin, err := filepath.Abs(*proxy)
	if err != nil {
		return false, err
	}
	version, err := clientVersion(ctx, *codex)
	if err != nil {
		return false, fmt.Errorf("client version: %w", err)
	}
	fmt.Println("client:", version)

	direct := newStandIn()
	addr, stop, err := serve(direct)
	if err != nil {
		return false, err
	}
	dOK, dPrinted, err := runClient(ctx, *codex, addr, filepath.Join(dir, "direct"))
	stop()
	if err != nil {
		return false, err
	}

	proxied := newStandIn()
	addr, stop, err = serve(proxied)
	if err != nil {
		return false, err
	}
	defer stop()
	pdir := filepath.Join(dir, "proxied")
	if err := os.MkdirAll(pdir, 0o700); err != nil {
		return false, err
	}
	paddr, stopProxy, err := startProxy(ctx, bin, addr, pdir)
	if err != nil {
		return false, err
	}
	pOK, pPrinted, err := runClient(ctx, *codex, paddr, pdir)
	stopProxy()
	if err != nil {
		return false, err
	}

	rec, prec := direct.record(version), proxied.record(version)
	check := func(ok bool, what string) {
		state := "ok  "
		if !ok {
			state, failed = "FAIL", true
		}
		fmt.Println(state, what)
	}
	check(dOK && dPrinted, "direct run: client finished and printed the fabricated value")
	check(len(direct.problems) == 0, "direct run: every request body was one JSON document")
	check(direct.routes["POST /v1/responses"] > 0, "direct run: client used POST /v1/responses")
	check(direct.sawValue, "direct run: the stand-in saw the fabricated value (control for the check below)")
	if direct.toolSent {
		check(direct.toolBack, "direct run: client returned an output for the exec tool call")
		check(proxied.toolBack, "proxied run: client returned an output for the exec tool call")
	} else {
		fmt.Println("note the client did not offer a free-form exec tool; no tool turn was exercised")
	}
	check(pOK, "proxied run: client finished")
	check(pPrinted, "proxied run: client printed the restored value")
	check(!proxied.sawValue, "proxied run: the fabricated value did not reach the stand-in")
	var refused []string
	for _, route := range rec.Routes {
		if proxied.routes[route] == 0 {
			refused = append(refused, route)
		}
	}
	check(len(refused) == 0, "proxied run: every route the client used was forwarded")
	for _, route := range refused {
		fmt.Println("     not forwarded:", route)
	}
	changed := 0
	for route, shape := range rec.Shapes {
		added, removed := clientprobe.Diff(shape, prec.Shapes[route])
		changed += len(added) + len(removed)
	}
	check(changed == 0, "proxied run: forwarded request bodies have the same structure as direct ones")
	fmt.Printf("routes: %d direct, %d proxied; shape entries: %d\n", len(rec.Routes), len(prec.Routes), entries(rec))

	if *baseline != "" {
		data, err := os.ReadFile(*baseline)
		if err != nil {
			return false, err
		}
		var base record
		if err := json.Unmarshal(data, &base); err != nil {
			return false, fmt.Errorf("baseline: %w", err)
		}
		added, removed := clientprobe.Diff(flatten(base), flatten(rec))
		fmt.Printf("compared with %s: %d added, %d removed\n", base.Client, len(added), len(removed))
		for _, e := range added {
			fmt.Println("  +", e)
		}
		for _, e := range removed {
			fmt.Println("  -", e)
		}
		check(len(added) == 0, "no request shape that the baseline lacks")
	}
	if *write != "" {
		data, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			return false, err
		}
		if err := os.WriteFile(*write, append(data, '\n'), 0o644); err != nil {
			return false, err
		}
	}
	return failed, nil
}

func entries(r record) int { return len(flatten(r)) }

// flatten turns a record into comparable lines, one per route, header name
// and shape entry.
func flatten(r record) []string {
	var out []string
	for _, route := range r.Routes {
		out = append(out, "route "+route)
	}
	for route, names := range r.Headers {
		for _, n := range names {
			out = append(out, "header "+route+" "+n)
		}
	}
	for route, shape := range r.Shapes {
		for _, e := range shape {
			out = append(out, "body "+route+" "+e)
		}
	}
	sort.Strings(out)
	return out
}

func main() {
	failed, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clientprobe:", err)
		os.Exit(2)
	}
	if failed {
		os.Exit(1)
	}
}
