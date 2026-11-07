package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cui-proxy/internal/adapter"
	_ "cui-proxy/internal/adapter/codex" // registers the "codex" adapter
	"cui-proxy/internal/audit"
	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/metrics"
	"cui-proxy/internal/proxy"
	"cui-proxy/internal/synth"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/ui"
	"cui-proxy/internal/vault"
)

// defaultSaveInterval is how often a file-mode vault is saved when it changed.
var defaultSaveInterval = 30 * time.Second

const auditCapacity = 500

// AppOptions configure NewApp.
type AppOptions struct {
	ConfigPath string
	Adapter    string // default "codex"
	Version    string
	Logf       func(format string, args ...any) // nil: log.Printf; never given values

	// Test hooks.
	saveInterval time.Duration          // 0: defaultSaveInterval
	onSave       func(n int, err error) // called after each periodic save attempt
}

// ruleset is the hot-reloadable part of the app. It is immutable once
// stored; a reload builds a new one and swaps the pointer, so a request that
// loaded a ruleset finishes with it.
type ruleset struct {
	cfg       *config.Config // effective config (restart-only fields pinned)
	detectors []detect.Detector
	sanitizer *transform.Sanitizer // nil sink; the proxy attaches per-request sinks
	restorer  *transform.Restorer
}

// App wires config, rules, vault, proxy and dashboard together.
type App struct {
	opts     AppOptions
	logf     func(string, ...any)
	cfgPath  string // absolute
	baseDir  string // relative dictionary and vault paths resolve here
	adapter  adapter.Adapter
	upstream *url.URL
	vault    *vault.Vault
	vaultAt  string // file-mode vault path; "" in memory mode
	metrics  *metrics.Registry
	audit    *audit.Log

	cur      atomic.Pointer[ruleset]
	reloadMu sync.Mutex

	listen                func(network, addr string) (net.Listener, error)
	proxySrv, uiSrv       *http.Server
	proxyAddr, uiAddr     string
	proxyBase, uiBase     context.Context
	proxyCancel, uiCancel context.CancelFunc
	errs                  chan error

	saveStop chan struct{}
	saveDone chan struct{}
	savedAt  uint64 // vault.Changes at the last successful save (or at open)

	stopOnce sync.Once
	stopErr  error
}

// NewApp loads and validates the config, builds the rules, opens the vault
// and prepares (but does not bind) the servers.
func NewApp(opts AppOptions) (*App, error) {
	if opts.Adapter == "" {
		opts.Adapter = "codex"
	}
	if opts.Logf == nil {
		opts.Logf = log.Printf
	}
	if opts.saveInterval <= 0 {
		opts.saveInterval = defaultSaveInterval
	}
	abs, err := filepath.Abs(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("config path: %w", err)
	}
	a := &App{
		opts: opts, logf: opts.Logf, cfgPath: abs, baseDir: filepath.Dir(abs),
		metrics: metrics.New(), audit: audit.New(auditCapacity),
		listen: net.Listen, errs: make(chan error, 2),
	}
	cfg, ds, err := loadRules(abs)
	if err != nil {
		return nil, err
	}
	ad, ok := adapter.Get(opts.Adapter)
	if !ok {
		return nil, fmt.Errorf("unknown adapter %q", opts.Adapter)
	}
	a.adapter = ad
	// Every upstream is checked, not only the selected adapter's: a bad
	// unused entry must not be loaded (and shown) just because it is unused.
	if cfg.Upstreams[ad.Name()] == "" {
		return nil, fmt.Errorf("no upstream configured for adapter %q", ad.Name())
	}
	ups, err := parseUpstreams(cfg.Upstreams)
	if err != nil {
		return nil, err
	}
	a.upstream = ups[ad.Name()]
	if err := a.openVault(cfg.Vault); err != nil {
		return nil, err
	}
	a.cur.Store(a.newRuleset(cfg, ds))
	return a, nil
}

// loadRules reads, validates (including synth classes) and builds the rules
// of the config at path.
func loadRules(path string) (*config.Config, []detect.Detector, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, err
	}
	if err := cfg.ValidateWith(synth.Known); err != nil {
		return nil, nil, err
	}
	ds, err := detect.Build(cfg.Rules, filepath.Dir(path))
	if err != nil {
		return nil, nil, err
	}
	return cfg, ds, nil
}

func (a *App) newRuleset(cfg *config.Config, ds []detect.Detector) *ruleset {
	return &ruleset{
		cfg:       cfg,
		detectors: ds,
		sanitizer: transform.NewSanitizer(ds, a.vault, nil),
		restorer:  transform.NewRestorer(a.vault),
	}
}

// parseUpstreams checks every configured upstream, in name order; serve and
// check-config share it.
func parseUpstreams(raw map[string]string) (map[string]*url.URL, error) {
	out := make(map[string]*url.URL, len(raw))
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		u, err := parseUpstream(name, raw[name])
		if err != nil {
			return nil, err
		}
		out[name] = u
	}
	return out, nil
}

// parseUpstream checks an upstream base URL: absolute, https (plain http
// only for a loopback host), no credentials, query or fragment. Errors never
// include the URL, which may hold credentials.
func parseUpstream(name, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return nil, fmt.Errorf("upstream for adapter %q is not an absolute URL", name)
	}
	if u.User != nil {
		return nil, fmt.Errorf("upstream for adapter %q must not contain credentials", name)
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, fmt.Errorf("upstream for adapter %q must not contain a query or fragment", name)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !loopbackName(u.Hostname()) {
			return nil, fmt.Errorf("upstream for adapter %q must use https (plain http is allowed only for a loopback host)", name)
		}
	default:
		return nil, fmt.Errorf("upstream for adapter %q must use https", name)
	}
	return u, nil
}

func loopbackName(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// resolve makes a config-relative path absolute.
func (a *App) resolve(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(a.baseDir, p)
}

// openVault creates the process vault. Memory mode uses a random key that
// never touches disk. File mode never starts empty over an existing file it
// cannot read: a wrong key, an old version or a corrupt file is an error.
func (a *App) openVault(vc config.VaultConfig) error {
	if vc.Mode != "file" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return fmt.Errorf("vault: generate key: %w", err)
		}
		a.vault = vault.New(key)
		return nil
	}
	keyPath, path := a.resolve(vc.KeyFile), a.resolve(vc.File)
	_, err := os.Stat(path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("vault: %w", err)
	}
	if exists {
		if _, err := os.Stat(keyPath); errors.Is(err, fs.ErrNotExist) {
			// Creating a fresh key would make the existing vault unreadable.
			return fmt.Errorf("vault: %s exists but its key file %s is missing", path, keyPath)
		}
	}
	key, err := vault.LoadOrCreateKey(keyPath)
	if err != nil {
		return err
	}
	if exists {
		if a.vault, err = vault.Open(path, key); err != nil {
			var pe *fs.PathError
			if errors.As(err, &pe) {
				return err // the OS error already names the path
			}
			return fmt.Errorf("%w (%s)", err, path)
		}
	} else {
		a.vault = vault.New(key)
	}
	a.vaultAt = path
	a.savedAt = a.vault.Changes()
	return nil
}

// Start binds the proxy and dashboard listeners and begins serving.
func (a *App) Start() error {
	rs := a.cur.Load()
	cfg := rs.cfg
	pl, err := a.bind(cfg.Listen, cfg.AllowNonLoopback)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	ul, err := a.bind(cfg.UI.Listen, cfg.AllowNonLoopback)
	if err != nil {
		pl.Close()
		return fmt.Errorf("ui.listen: %w", err)
	}
	a.proxyAddr, a.uiAddr = pl.Addr().String(), ul.Addr().String()

	a.proxyBase, a.proxyCancel = context.WithCancel(context.Background())
	a.uiBase, a.uiCancel = context.WithCancel(context.Background())
	errLog := log.New(suppressed{a.logf}, "", 0)
	a.proxySrv = &http.Server{
		Handler: proxy.New(proxy.Deps{
			Adapter:  a.adapter,
			Upstream: a.upstream,
			Snapshot: func() proxy.Snapshot {
				rs := a.cur.Load() // one load: policy and rules from the same reload
				return proxy.Snapshot{Cfg: rs.cfg, Sanitizer: rs.sanitizer, Restorer: rs.restorer}
			},
			VaultLen: a.vault.Len,
			Metrics:  a.metrics,
			Audit:    a.audit,
			Logf:     a.logf,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    256 << 10,
		BaseContext:       func(net.Listener) context.Context { return a.proxyBase },
		ErrorLog:          errLog,
	}
	a.uiSrv = &http.Server{
		Handler: ui.New(ui.Deps{
			Cfg:       func() *config.Config { return a.cur.Load().cfg },
			Metrics:   a.metrics,
			Audit:     a.audit,
			Detectors: func() []detect.Detector { return a.cur.Load().detectors },
			Reload:    a.Reload,
			Version:   a.opts.Version,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		BaseContext:       func(net.Listener) context.Context { return a.uiBase },
		ErrorLog:          errLog,
	}
	// The dashboard feed never ends on its own: end it when shutdown starts.
	// Proxy streams get the drain to finish; see Shutdown.
	a.uiSrv.RegisterOnShutdown(a.uiCancel)

	for _, s := range []struct {
		srv *http.Server
		l   net.Listener
	}{{a.proxySrv, pl}, {a.uiSrv, ul}} {
		go func() {
			if err := s.srv.Serve(s.l); err != nil && !errors.Is(err, http.ErrServerClosed) {
				a.errs <- err
			}
		}()
	}
	if a.vaultAt != "" {
		a.saveStop, a.saveDone = make(chan struct{}), make(chan struct{})
		go a.saveLoop()
	}
	a.logStartup(rs)
	return nil
}

// bind listens on addr and checks the address actually bound: a hostname
// such as localhost could resolve to a non-loopback address.
func (a *App) bind(addr string, allowNonLoopback bool) (net.Listener, error) {
	l, err := a.listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if err := checkBound(l.Addr(), allowNonLoopback); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

func checkBound(addr net.Addr, allowNonLoopback bool) error {
	if allowNonLoopback {
		return nil
	}
	if ta, ok := addr.(*net.TCPAddr); ok && ta.IP.IsLoopback() {
		return nil
	}
	return fmt.Errorf("bound address %s is not loopback (set allow_non_loopback to override)", addr)
}

func (a *App) logStartup(rs *ruleset) {
	cfg := rs.cfg
	a.logf("cui-proxy %s", a.opts.Version)
	a.logf("config: %s", a.cfgPath)
	a.logf("proxy listening on http://%s", a.proxyAddr)
	a.logf("dashboard on http://%s/", a.uiAddr)
	a.logf("adapter: %s", a.adapter.Name())
	a.logf("upstream: %s://%s", a.upstream.Scheme, a.upstream.Host)
	a.logf("rules: %d (%d detectors)", len(cfg.Rules), len(rs.detectors))
	if a.vaultAt != "" {
		a.logf("vault: file %s (%d mappings)", a.vaultAt, a.vault.Len())
	} else {
		a.logf("vault: memory (mappings are lost on restart)")
	}
	a.policyWarnings(cfg)
	if cfg.AllowNonLoopback {
		a.logf("WARN allow_non_loopback=true: listeners may bind non-loopback addresses")
	}
}

// warnPassthrough is logged by serve and printed by check-config when
// policy.on_error is passthrough.
const warnPassthrough = "WARN policy.on_error=passthrough: UNSAFE: failures forward the original request"

func (a *App) policyWarnings(cfg *config.Config) {
	if cfg.Policy.OnError == "passthrough" {
		a.logf(warnPassthrough)
	}
	if len(cfg.Rules) == 0 {
		a.logf("WARN no rules configured: nothing will be sanitized")
	}
}

// Addrs returns the bound proxy and dashboard addresses (after Start).
func (a *App) Addrs() (proxyAddr, uiAddr string) { return a.proxyAddr, a.uiAddr }

// Errors delivers a listener failure after Start.
func (a *App) Errors() <-chan error { return a.errs }

// Reload re-reads the config file and, only if it is valid and its rules
// build, swaps in the new rule set. The vault is kept. Settings that need a
// restart keep their running values. Reloads are serialized.
func (a *App) Reload() error {
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()
	cfg, ds, err := loadRules(a.cfgPath)
	if err != nil {
		a.logf("reload failed; keeping the current rules: %v", err)
		return err
	}
	old := a.cur.Load()
	if changed := pinRestartOnly(cfg, old.cfg); len(changed) > 0 {
		a.logf("WARN restart required for: %s", strings.Join(changed, ", "))
	}
	a.cur.Store(a.newRuleset(cfg, ds))
	a.logf("reload ok: %d rules (%d detectors)", len(cfg.Rules), len(ds))
	a.policyWarnings(cfg)
	return nil
}

// pinRestartOnly copies the settings that cannot change at runtime from cur
// into next and returns the names of those that differ.
func pinRestartOnly(next, cur *config.Config) []string {
	var changed []string
	if next.Listen != cur.Listen {
		changed = append(changed, "listen")
	}
	if next.UI.Listen != cur.UI.Listen {
		changed = append(changed, "ui.listen")
	}
	if !maps.Equal(next.Upstreams, cur.Upstreams) {
		changed = append(changed, "upstreams")
	}
	if next.Vault.Mode != cur.Vault.Mode {
		changed = append(changed, "vault.mode")
	}
	if next.Vault.KeyFile != cur.Vault.KeyFile {
		changed = append(changed, "vault.key_file")
	}
	if next.Vault.File != cur.Vault.File {
		changed = append(changed, "vault.file")
	}
	if next.AllowNonLoopback != cur.AllowNonLoopback {
		changed = append(changed, "allow_non_loopback")
	}
	next.Listen, next.UI.Listen = cur.Listen, cur.UI.Listen
	next.Upstreams = maps.Clone(cur.Upstreams)
	next.Vault = cur.Vault
	next.AllowNonLoopback = cur.AllowNonLoopback
	return changed
}

// Shutdown stops accepting connections and waits for in-flight requests
// until ctx is done; then it force-closes what is left. The dashboard feed
// is ended at once. A file-mode vault is saved last.
func (a *App) Shutdown(ctx context.Context) error {
	a.stopOnce.Do(func() { a.stopErr = a.stop(ctx) })
	return a.stopErr
}

// Close stops the app at once (no drain). Safe to call more than once and
// after Shutdown.
func (a *App) Close() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = a.Shutdown(ctx)
}

func (a *App) stop(ctx context.Context) error {
	var err error
	if a.proxySrv != nil {
		var wg sync.WaitGroup
		var perr, uerr error
		wg.Add(2)
		go func() { defer wg.Done(); perr = a.proxySrv.Shutdown(ctx) }()
		go func() { defer wg.Done(); uerr = a.uiSrv.Shutdown(ctx) }()
		wg.Wait()
		if perr != nil || uerr != nil {
			// Drain expired: cancel what is left (upstream requests included)
			// and close the connections.
			a.proxyCancel()
			a.uiCancel()
			a.proxySrv.Close()
			a.uiSrv.Close()
			if ctx.Err() != nil && ctx.Err() != context.Canceled {
				err = errors.New("shutdown: drain timed out; open connections closed")
			}
		}
		a.proxyCancel()
		a.uiCancel()
	}
	if a.saveStop != nil {
		close(a.saveStop)
		<-a.saveDone
	}
	if a.vaultAt != "" {
		if serr := a.saveIfChanged(); serr != nil && err == nil {
			err = errors.New("shutdown: vault save failed")
		}
	}
	return err
}

func (a *App) saveLoop() {
	defer close(a.saveDone)
	t := time.NewTicker(a.opts.saveInterval)
	defer t.Stop()
	for {
		select {
		case <-a.saveStop:
			return
		case <-t.C:
			if a.vault.Changes() == a.savedAt {
				continue
			}
			n := a.vault.Len()
			err := a.saveIfChanged()
			if a.opts.onSave != nil {
				a.opts.onSave(n, err)
			}
		}
	}
}

// saveIfChanged saves the vault if it changed (mappings or the overflow
// flag) since the last save. A failure is
// logged by kind only and retried on the next call.
func (a *App) saveIfChanged() error {
	n := a.vault.Changes() // read first: a change during Save is saved next time
	if n == a.savedAt {
		return nil
	}
	if err := a.vault.Save(a.vaultAt); err != nil {
		a.logf("WARN vault save failed (%s); will retry", fsErrKind(err))
		return err
	}
	a.savedAt = n
	return nil
}

func fsErrKind(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	case errors.Is(err, fs.ErrNotExist):
		return "directory missing"
	}
	return "i/o error"
}

// suppressed is the http.Server error log. Server errors can carry details
// of a malformed client request, so only a fixed line is logged.
type suppressed struct{ logf func(string, ...any) }

func (s suppressed) Write(p []byte) (int, error) {
	s.logf("http server error (details suppressed)")
	return len(p), nil
}
