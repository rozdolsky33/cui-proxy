package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/synth"
	"cui-proxy/internal/vault"
)

// TestProxySmoke: one request through the real proxy listener: the upstream
// sees only synthetics, the client gets the original text back.
func TestProxySmoke(t *testing.T) {
	up := newUpstream(t, nil)
	_, cfg := testDir(t, cfgSpec{Upstream: up.url()})
	logs := &logBuf{}
	a := startApp(t, cfg, logs, nil)
	px, _ := a.Addrs()

	text := "connect to " + hostA + " at " + realIP + " for " + realOCID
	status, got := ask(t, px, text)
	seen := up.next(t)
	if status != 200 {
		t.Fatalf("status %d", status)
	}
	assertNoValues(t, "upstream body", seen, hostA, realIP, realOCID)
	if got != text {
		t.Fatalf("client got %q, want the original text", got)
	}
	assertNoValues(t, "logs", logs.String(), hostA, realIP, realOCID)
}

func TestReloadSwapsRulesForNewRequests(t *testing.T) {
	up := newUpstream(t, nil)
	spec := cfgSpec{Upstream: up.url()}
	_, cfg := testDir(t, spec)
	a := startApp(t, cfg, &logBuf{}, nil)
	px, _ := a.Addrs()

	text := hostA + " and " + hostB
	ask(t, px, text)
	if seen := up.next(t); strings.Contains(seen, hostA) || !strings.Contains(seen, hostB) {
		t.Fatalf("before reload: want hostA replaced and hostB untouched, upstream saw %q", seen)
	}

	spec.Rules = rulesB
	writeFile(t, cfg, spec.render())
	if err := a.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	ask(t, px, text)
	if seen := up.next(t); !strings.Contains(seen, hostA) || strings.Contains(seen, hostB) {
		t.Fatalf("after reload: want hostB replaced and hostA untouched, upstream saw %q", seen)
	}
}

func TestReloadInvalidKeepsOldRules(t *testing.T) {
	up := newUpstream(t, nil)
	spec := cfgSpec{Upstream: up.url()}
	_, cfg := testDir(t, spec)
	logs := &logBuf{}
	a := startApp(t, cfg, logs, nil)
	px, uiAddr := a.Addrs()
	before := a.cur.Load()

	bad := []string{
		"listen: [unterminated\n", // YAML error
		strings.Replace(spec.render(), "synth: hostname", "synth: nonsense", 1), // unknown synth class
		strings.Replace(spec.render(), "file: a.txt", "file: missing.txt", 1),   // dictionary file missing
	}
	for i, content := range bad {
		writeFile(t, cfg, content)
		if err := a.Reload(); err == nil {
			t.Fatalf("case %d: Reload succeeded on an invalid config", i)
		}
		if a.cur.Load() != before {
			t.Fatalf("case %d: rule set swapped on a failed reload", i)
		}
	}

	// The dashboard trigger reports the failure too.
	req, _ := http.NewRequest("POST", "http://"+uiAddr+"/api/reload", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	res, err := testClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusUnprocessableEntity || out.OK || out.Error == "" {
		t.Fatalf("POST /api/reload on bad config: status %d, %+v", res.StatusCode, out)
	}

	ask(t, px, "use "+hostA)
	if seen := up.next(t); strings.Contains(seen, hostA) {
		t.Fatalf("old rules not in force after failed reloads: upstream saw %q", seen)
	}
	if !strings.Contains(logs.String(), "reload failed") {
		t.Errorf("failed reload not logged:\n%s", logs)
	}
	assertNoValues(t, "logs", logs.String(), hostA, hostB)
}

func TestReloadViaDashboard(t *testing.T) {
	up := newUpstream(t, nil)
	spec := cfgSpec{Upstream: up.url()}
	_, cfg := testDir(t, spec)
	a := startApp(t, cfg, &logBuf{}, nil)
	_, uiAddr := a.Addrs()
	before := a.cur.Load()

	spec.Rules = rulesB
	writeFile(t, cfg, spec.render())
	req, _ := http.NewRequest("POST", "http://"+uiAddr+"/api/reload", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	res, err := testClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("POST /api/reload: status %d", res.StatusCode)
	}
	if a.cur.Load() == before {
		t.Fatal("dashboard reload did not swap the rule set")
	}
}

func TestReloadKeepsVaultMappings(t *testing.T) {
	up := newUpstream(t, nil)
	spec := cfgSpec{Upstream: up.url()}
	_, cfg := testDir(t, spec)
	a := startApp(t, cfg, &logBuf{}, nil)
	px, _ := a.Addrs()

	ask(t, px, realIP)
	first := up.next(t)
	if first == realIP || first == "" {
		t.Fatalf("not sanitized: %q", first)
	}
	spec.OnError = "passthrough" // a real change, so the set is rebuilt
	writeFile(t, cfg, spec.render())
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	_, got := ask(t, px, realIP)
	if second := up.next(t); second != first {
		t.Fatalf("mapping changed across reload: %q then %q", first, second)
	}
	if got != realIP {
		t.Fatalf("restore after reload: got %q", got)
	}
}

func TestInFlightStreamUsesOldRules(t *testing.T) {
	gate := make(chan struct{})
	up := newUpstream(t, func(u *upstream) { u.gate = gate })
	spec := cfgSpec{Upstream: up.url()}
	_, cfg := testDir(t, spec)
	a := startApp(t, cfg, &logBuf{}, nil)
	px, _ := a.Addrs()
	old := a.cur.Load()

	text := "deploy to " + hostA
	type result struct {
		status int
		got    string
	}
	done := make(chan result, 1)
	go func() {
		s, g := ask(t, px, text)
		done <- result{s, g}
	}()
	if seen := up.next(t); strings.Contains(seen, hostA) {
		t.Fatalf("in-flight request not sanitized with the old rules: %q", seen)
	}

	// The request is now in flight (upstream holds its stream open).
	spec.Rules = rulesB
	writeFile(t, cfg, spec.render())
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	if a.cur.Load() == old {
		t.Fatal("reload did not swap the set")
	}
	close(gate)

	select {
	case r := <-done:
		if r.status != 200 || r.got != text {
			t.Fatalf("in-flight stream after reload: status %d, got %q, want %q", r.status, r.got, text)
		}
	case <-time.After(waitLimit):
		t.Fatal("in-flight request did not finish")
	}
	// New requests use the new set (hosts-a is gone).
	ask(t, px, text)
	if seen := up.next(t); !strings.Contains(seen, hostA) {
		t.Fatalf("new request still uses the old rules: %q", seen)
	}
}

func TestReloadRestartRequiredFields(t *testing.T) {
	up := newUpstream(t, nil)
	spec := cfgSpec{Upstream: up.url()}
	_, cfg := testDir(t, spec)
	logs := &logBuf{}
	a := startApp(t, cfg, logs, nil)
	before := *a.cur.Load().cfg

	spec.Listen = "127.0.0.1:18787"
	spec.UIListen = "127.0.0.1:18788"
	spec.Upstream = "https://other.example/codex"
	spec.OnError = "passthrough"
	writeFile(t, cfg, spec.render())
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	eff := a.cur.Load().cfg
	if eff.Listen != before.Listen || eff.UI.Listen != before.UI.Listen || eff.Upstreams["codex"] != before.Upstreams["codex"] {
		t.Fatalf("restart-only settings applied on reload: %+v", eff)
	}
	if eff.Policy.OnError != "passthrough" {
		t.Fatalf("policy.on_error not applied on reload: %q", eff.Policy.OnError)
	}
	l := logs.String()
	if !strings.Contains(l, "WARN restart required for: listen, ui.listen, upstreams") {
		t.Fatalf("missing restart-required warning:\n%s", l)
	}
	assertNoValues(t, "logs", l, "other.example", "18787")
}

func TestReloadConcurrent(t *testing.T) {
	up := newUpstream(t, nil)
	spec := cfgSpec{Upstream: up.url()}
	_, cfg := testDir(t, spec)
	a := startApp(t, cfg, &logBuf{}, nil)
	px, _ := a.Addrs()

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if err := a.Reload(); err != nil {
					errs <- err
				}
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := testClient.Post("http://"+px+"/v1/responses", "application/json", strings.NewReader(userBody(realIP)))
			if err != nil {
				errs <- err
				return
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			select {
			case <-up.texts:
			case <-time.After(waitLimit):
				errs <- errors.New("upstream received no request")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestStartupRejectsBadUpstream(t *testing.T) {
	for _, tc := range []struct{ name, upstreams, want string }{
		{"missing", "upstreams: { other: https://x.example }", `no upstream configured for adapter "codex"`},
		{"plain http", "upstreams: { codex: http://chatgpt.example/codex }", "https"},
		{"userinfo", "upstreams: { codex: https://u:p@chatgpt.example/codex }", "credentials"},
		{"relative", "upstreams: { codex: /codex }", "absolute"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, cfg := testDir(t, cfgSpec{})
			b, _ := os.ReadFile(cfg)
			s := strings.Replace(string(b), "upstreams:\n  codex: http://127.0.0.1:1/codex", tc.upstreams, 1)
			writeFile(t, cfg, s)
			_, err := NewApp(AppOptions{ConfigPath: cfg, Logf: (&logBuf{}).logf})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "u:p") {
				t.Fatal("error leaks upstream credentials")
			}
		})
	}
}

func TestUnknownAdapter(t *testing.T) {
	_, cfg := testDir(t, cfgSpec{})
	if _, err := NewApp(AppOptions{ConfigPath: cfg, Adapter: "nope", Logf: (&logBuf{}).logf}); err == nil || !strings.Contains(err.Error(), `unknown adapter "nope"`) {
		t.Fatalf("err = %v", err)
	}
}

// ---- vault file mode ----

const fileVault = "{ mode: file, key_file: keys/vault.key, file: vault.bin }"

func TestVaultFileWrongKeyOrVersionIsStartupError(t *testing.T) {
	dir, cfg := testDir(t, cfgSpec{Vault: fileVault})
	keyPath := filepath.Join(dir, "keys", "vault.key")
	vaultPath := filepath.Join(dir, "vault.bin")

	// A vault saved under another key.
	other := bytes.Repeat([]byte{7}, 32)
	v := vault.New(other)
	if _, err := v.Pseudonym("ipv4", realIP); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(vaultPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, keyPath, string(bytes.Repeat([]byte{9}, 32)))

	check := func(label string) {
		t.Helper()
		orig, _ := os.ReadFile(vaultPath)
		a, err := NewApp(AppOptions{ConfigPath: cfg, Logf: (&logBuf{}).logf})
		if err == nil {
			a.Close()
			t.Fatalf("%s: NewApp accepted an unreadable vault file", label)
		}
		now, _ := os.ReadFile(vaultPath)
		if !bytes.Equal(orig, now) {
			t.Fatalf("%s: vault file modified", label)
		}
	}
	check("wrong key")

	writeFile(t, vaultPath, "CUIV1"+strings.Repeat("x", 64))
	check("old version")

	// A vault file without its key file: never create a fresh key for it.
	os.Remove(keyPath)
	check("missing key")
	if _, err := os.Stat(keyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a new key file was created for an existing vault")
	}
}

func TestVaultFileSavedOnShutdownAndRestored(t *testing.T) {
	up := newUpstream(t, nil)
	dir, cfg := testDir(t, cfgSpec{Upstream: up.url(), Vault: fileVault})
	a := startApp(t, cfg, &logBuf{}, nil)
	px, _ := a.Addrs()
	ask(t, px, realIP)
	syn := up.next(t)

	ctx, cancel := context.WithTimeout(context.Background(), waitLimit)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	key, err := os.ReadFile(filepath.Join(dir, "keys", "vault.key"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(filepath.Join(dir, "vault.bin"), key)
	if err != nil {
		t.Fatalf("Open after shutdown: %v", err)
	}
	if r, ok := v.Real(syn); !ok || r != realIP {
		t.Fatal("saved vault lacks the mapping")
	}

	// A restarted app restores the old synthetic and maps the value the same way.
	a2 := startApp(t, cfg, &logBuf{}, nil)
	px2, _ := a2.Addrs()
	_, got := ask(t, px2, realIP)
	if again := up.next(t); again != syn {
		t.Fatalf("mapping not stable across restart: %q then %q", syn, again)
	}
	if got != realIP {
		t.Fatalf("restore after restart: %q", got)
	}
}

func TestVaultFilePeriodicSave(t *testing.T) {
	up := newUpstream(t, nil)
	dir, cfg := testDir(t, cfgSpec{Upstream: up.url(), Vault: fileVault})
	saved := make(chan int, 16)
	a := startApp(t, cfg, &logBuf{}, func(o *AppOptions) {
		o.saveInterval = time.Millisecond
		o.onSave = func(n int, err error) {
			if err == nil {
				saved <- n
			}
		}
	})
	px, _ := a.Addrs()
	ask(t, px, realIP)
	syn := up.next(t)
	select {
	case n := <-saved:
		if n < 1 {
			t.Fatalf("saved %d mappings", n)
		}
	case <-time.After(waitLimit):
		t.Fatal("no periodic save")
	}
	key, _ := os.ReadFile(filepath.Join(dir, "keys", "vault.key"))
	v, err := vault.Open(filepath.Join(dir, "vault.bin"), key)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := v.Real(syn); !ok || r != realIP {
		t.Fatal("periodically saved vault lacks the mapping")
	}
}

// ---- listeners, logging, shutdown ----

// fakeListener reports a non-loopback address.
type fakeListener struct {
	net.Listener
	closed bool
}

func (f *fakeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 8787}
}
func (f *fakeListener) Close() error { f.closed = true; return f.Listener.Close() }

func TestStartRejectsNonLoopbackBoundAddress(t *testing.T) {
	_, cfg := testDir(t, cfgSpec{})
	var fakes []*fakeListener
	a, err := NewApp(AppOptions{ConfigPath: cfg, Logf: (&logBuf{}).logf})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.listen = func(network, addr string) (net.Listener, error) {
		l, err := net.Listen(network, addr)
		if err != nil {
			return nil, err
		}
		f := &fakeListener{Listener: l}
		fakes = append(fakes, f)
		return f, nil
	}
	err = a.Start()
	if err == nil || !strings.Contains(err.Error(), "not loopback") {
		t.Fatalf("Start err = %v, want a loopback error", err)
	}
	for _, f := range fakes {
		if !f.closed {
			t.Fatal("listener left open after the bound-address check failed")
		}
	}

	if err := checkBound(&net.TCPAddr{IP: net.ParseIP("192.0.2.10")}, true); err != nil {
		t.Fatalf("allow_non_loopback: %v", err)
	}
	if err := checkBound(&net.TCPAddr{IP: net.ParseIP("::1")}, false); err != nil {
		t.Fatalf("::1: %v", err)
	}
}

func TestStartupLogLines(t *testing.T) {
	up := newUpstream(t, nil)
	_, cfg := testDir(t, cfgSpec{Upstream: up.url(), OnError: "passthrough", Extra: "allow_non_loopback: true"})
	logs := &logBuf{}
	a := startApp(t, cfg, logs, nil)
	px, uiAddr := a.Addrs()
	l := logs.String()
	for _, want := range []string{
		"cui-proxy test",
		cfg,
		px,
		uiAddr,
		"adapter: codex",
		"upstream: " + up.srv.URL + "\n", // scheme and host only, not the path
		"rules: 2",
		"vault: memory",
		"WARN policy.on_error=passthrough",
		"UNSAFE: failures forward the original request",
		"WARN allow_non_loopback=true",
	} {
		if !strings.Contains(l, want) {
			t.Errorf("startup log lacks %q:\n%s", want, l)
		}
	}
	if strings.Contains(l, "/codex") {
		t.Errorf("startup log contains the upstream path:\n%s", l)
	}
	assertNoValues(t, "logs", l, hostA, hostB)
}

func TestShutdownWithOpenStreams(t *testing.T) {
	up := newUpstream(t, func(u *upstream) { u.hang = true })
	_, cfg := testDir(t, cfgSpec{Upstream: up.url()})
	a := startApp(t, cfg, &logBuf{}, nil)
	px, uiAddr := a.Addrs()

	// An open dashboard feed.
	feed, err := testClient.Get("http://" + uiAddr + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer feed.Body.Close()
	// An open proxy stream (upstream never finishes it).
	client := &http.Client{} // no timeout: the stream is meant to stay open
	res, err := client.Post("http://"+px+"/v1/responses", "application/json", strings.NewReader(userBody("hi")))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	up.next(t)

	drain := 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- a.Shutdown(ctx) }()
	select {
	case <-done:
	case <-time.After(waitLimit):
		t.Fatal("Shutdown blocked past the drain")
	}
	if el := time.Since(start); el > drain+5*time.Second {
		t.Fatalf("Shutdown took %v", el)
	}
	// Both streams are ended.
	readEnd := func(name string, r io.Reader) {
		ch := make(chan struct{})
		go func() { io.Copy(io.Discard, r); close(ch) }()
		select {
		case <-ch:
		case <-time.After(waitLimit):
			t.Fatalf("%s stream still open after shutdown", name)
		}
	}
	readEnd("dashboard", feed.Body)
	readEnd("proxy", res.Body)
}

func TestExamplesConfigValid(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "config.yaml")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateWith(synth.Known); err != nil {
		t.Fatal(err)
	}
	if _, err := detect.Build(cfg.Rules, filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:8787" || cfg.UI.Listen != "127.0.0.1:8788" ||
		cfg.Upstreams["codex"] != "https://chatgpt.com/backend-api/codex" ||
		cfg.Policy.OnError != "block" || cfg.Policy.MaxBodyBytes != 20971520 || cfg.Vault.Mode != "memory" {
		t.Fatalf("examples/config.yaml drifted: %+v", cfg)
	}
	if _, err := parseUpstream("codex", cfg.Upstreams["codex"]); err != nil {
		t.Fatal(err)
	}
}
