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
	"cui-proxy/internal/decisionlog"
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

// Config parsers may quote sensitive literals. Daemon errors stay value-free;
// an operator can explicitly request detailed diagnostics with check-config.
var errLoadRules = errors.New("configuration or rule files could not be loaded; run cui-proxy check-config locally for details")

// ruleLoadError preserves diagnostics for callers while letting serve suppress
// parser text before writing its daemon log.
type ruleLoadError struct{ error }

func (e *ruleLoadError) Unwrap() error { return e.error }

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
	opts      AppOptions
	logf      func(string, ...any)
	cfgPath   string // absolute
	baseDir   string // relative dictionary and vault paths resolve here
	adapter   adapter.Adapter
	upstream  *url.URL
	vault     *vault.Vault
	vaultAt   string   // file-mode vault path; "" in memory mode
	vaultLock *os.File // held from before opening the vault through the final save
	metrics   *metrics.Registry
	audit     *audit.Log
	// decisions is the persisted decision log, nil when audit is disabled
	// (the proxy then gets decisionlog.Discard).
	decisions *decisionlog.Log

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
	ready := false
	defer func() {
		if !ready && a.vaultLock != nil {
			a.vaultLock.Close()
		}
	}()
	cfg, ds, err := loadRules(abs)
	if err != nil {
		return nil, &ruleLoadError{err}
	}
	ad, upstream, err := adapterUpstream(opts.Adapter, cfg.Upstreams)
	if err != nil {
		return nil, err
	}
	a.adapter, a.upstream = ad, upstream
	if err := a.openVault(cfg.Vault); err != nil {
		return nil, err
	}
	// Last, so no later step can fail with the log open.
	if err := a.openDecisions(cfg); err != nil {
		return nil, err
	}
	a.metrics.SetCorrelationEnabled(cfg.Observability.Correlate)
	a.cur.Store(a.newRuleset(cfg, ds))
	ready = true
	return a, nil
}

// openDecisions opens the decision log (audit.file). A damaged log refuses
// to start when a failed record would block (audit.on_error: block, always
// under strict); under warn it starts a new segment and logs a WARN.
func (a *App) openDecisions(cfg *config.Config) error {
	if !cfg.Audit.IsEnabled() {
		return nil
	}
	if cfg.Audit.File == "" {
		return errors.New("audit: audit.file is not set and no home directory was found to put the decision log in")
	}
	l, err := decisionlog.Open(decisionlog.Options{
		Path:               cfg.Audit.File,
		MaxBytes:           cfg.Audit.MaxBytes(),
		RetentionDays:      cfg.Audit.Retention(),
		NewSegmentOnDamage: !cfg.AuditBlocks(),
		Logf:               a.logf,
	})
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	a.decisions = l
	return nil
}

// recorder is what the proxy records decisions with.
func (a *App) recorder() decisionlog.Recorder {
	if a.decisions == nil {
		return decisionlog.Discard
	}
	return a.decisions
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
	ds, err := detect.BuildForProfile(cfg.Rules, filepath.Dir(path), cfg.Policy.Profile)
	if err != nil {
		return nil, nil, err
	}
	resolveAuditFile(cfg, path)
	return cfg, ds, nil
}

// resolveAuditFile makes a relative audit.file absolute against the config
// file's directory, like the vault and dictionary paths. "~" is not expanded.
func resolveAuditFile(cfg *config.Config, cfgPath string) {
	if f := cfg.Audit.File; f != "" && !filepath.IsAbs(f) {
		cfg.Audit.File = filepath.Join(filepath.Dir(cfgPath), f)
	}
}

func (a *App) newRuleset(cfg *config.Config, ds []detect.Detector) *ruleset {
	return &ruleset{
		cfg:       cfg,
		detectors: ds,
		sanitizer: transform.NewSanitizer(ds, a.vault, nil),
		restorer:  transform.NewRestorer(a.vault),
	}
}

// adapterUpstream selects a registered adapter and validates its upstream,
// including every unused entry. Startup and check-config share these checks.
func adapterUpstream(name string, raw map[string]string) (adapter.Adapter, *url.URL, error) {
	if name == "" {
		name = "codex"
	}
	ad, ok := adapter.Get(name)
	if !ok {
		return nil, nil, fmt.Errorf("unknown adapter %q", name)
	}
	if raw[ad.Name()] == "" {
		return nil, nil, fmt.Errorf("no upstream configured for adapter %q", ad.Name())
	}
	ups, err := parseUpstreams(raw)
	if err != nil {
		return nil, nil, err
	}
	return ad, ups[ad.Name()], nil
}

// parseUpstreams checks every configured upstream, in name order.
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
	lock, err := vault.LockFile(path)
	if err != nil {
		return err
	}
	a.vaultLock = lock
	_, err = os.Stat(path)
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
			VaultLen:  a.vault.Len,
			Metrics:   a.metrics,
			Audit:     a.audit,
			Decisions: a.recorder(),
			Logf:      a.logf,
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
	a.logPolicy(cfg)
	a.logAudit(cfg)
	a.policyWarnings(cfg)
	if cfg.AllowNonLoopback {
		a.logf("WARN allow_non_loopback=true: listeners may bind non-loopback addresses")
	}
}

// Audit warnings, logged at startup.
const (
	warnAuditOff  = "WARN audit.enabled=false: no decision log is kept"
	warnAuditWarn = "WARN audit.on_error=warn: UNSAFE: a request is forwarded even when its decision cannot be recorded"
)

// logAudit states where decisions are recorded and what a failed write does.
func (a *App) logAudit(cfg *config.Config) {
	if a.decisions == nil {
		a.logf(warnAuditOff)
		return
	}
	retention := "kept forever"
	if d := cfg.Audit.Retention(); d > 0 {
		retention = fmt.Sprintf("kept %d days", d)
	}
	onError := config.AuditWarn
	if cfg.AuditBlocks() {
		onError = config.AuditBlock
	}
	a.logf("audit: decision log %s (on_error=%s, rotated at %d MB, rotated files %s)", cfg.Audit.File, onError, cfg.Audit.MaxBytes()>>20, retention)
	if onError == config.AuditWarn {
		a.logf(warnAuditWarn)
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
		// YAML and regexp errors can quote sensitive configuration values.
		// Keep those details in the operator's explicit check-config command,
		// never in persistent server logs or the dashboard reload response.
		err = errLoadRules
		a.logf("reload failed; keeping the current rules: %v", err)
		return err
	}
	old := a.cur.Load()
	// The audit settings are pinned and a reload cannot open a log that is
	// off, so a reload to strict without a running decision log is refused:
	// strict never runs unrecorded. (With a log, a failed write blocks under
	// strict whatever on_error says: see config.AuditBlocks.)
	if profileOf(cfg) == config.ProfileStrict && a.decisions == nil {
		err := errors.New(`reload refused: policy.profile "strict" needs the decision log, which was disabled at startup (audit.enabled: false); restart to apply`)
		a.logf("reload failed; keeping the current rules: %v", err)
		return err
	}
	if changed := pinRestartOnly(cfg, old.cfg); len(changed) > 0 {
		a.logf("WARN restart required for: %s", strings.Join(changed, ", "))
	}
	a.metrics.SetCorrelationEnabled(cfg.Observability.Correlate)
	a.cur.Store(a.newRuleset(cfg, ds))
	a.logf("reload ok: %d rules (%d detectors)", len(cfg.Rules), len(ds))
	a.logPolicy(cfg)
	a.warnIfWeakened(old.cfg, cfg)
	a.policyWarnings(cfg)
	return nil
}

func profileOf(cfg *config.Config) string {
	if cfg.Policy.Profile == "" {
		return config.ProfileSanitize
	}
	return cfg.Policy.Profile
}

// blockingRules counts the rules whose effective action is block.
func blockingRules(cfg *config.Config) int {
	n := 0
	for _, r := range cfg.Rules {
		if config.EffectiveAction(cfg.Policy.Profile, r) == config.ActionBlock {
			n++
		}
	}
	return n
}

// logPolicy states the active profile and how many rules block, at every
// start and reload, so a weaker policy is never silent.
func (a *App) logPolicy(cfg *config.Config) {
	a.logf("policy: profile=%s blocking_rules=%d of %d", profileOf(cfg), blockingRules(cfg), len(cfg.Rules))
}

// blockingNames is the set of rule names whose effective action is block.
func blockingNames(cfg *config.Config) map[string]bool {
	m := map[string]bool{}
	for _, r := range cfg.Rules {
		if config.EffectiveAction(cfg.Policy.Profile, r) == config.ActionBlock {
			m[r.Name] = true
		}
	}
	return m
}

// warnIfWeakened logs an explicit WARN when a reload lowers the policy: the
// profile goes from strict to anything else, or a rule that blocked no longer
// does (relaxed or removed), whatever the counts. A reload is a local operator
// action and may do this; it must not be silent.
func (a *App) warnIfWeakened(old, next *config.Config) {
	if op, np := profileOf(old), profileOf(next); op == config.ProfileStrict && np != config.ProfileStrict {
		a.logf("WARN policy weakened by reload: profile %s -> %s", op, np)
		return
	}
	was, now := blockingNames(old), blockingNames(next)
	var gone []string
	for name := range was {
		if !now[name] {
			gone = append(gone, cleanName(name))
		}
	}
	if len(gone) > 0 {
		slices.Sort(gone)
		a.logf("WARN policy weakened by reload: no longer blocking: %s", strings.Join(gone, ", "))
	}
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
	na, ca := next.Audit, cur.Audit
	if na.IsEnabled() != ca.IsEnabled() {
		changed = append(changed, "audit.enabled")
	}
	if na.File != ca.File {
		changed = append(changed, "audit.file")
	}
	if na.ExplicitOnError() != ca.ExplicitOnError() {
		changed = append(changed, "audit.on_error")
	}
	if na.MaxBytes() != ca.MaxBytes() {
		changed = append(changed, "audit.max_size_mb")
	}
	if na.Retention() != ca.Retention() {
		changed = append(changed, "audit.retention_days")
	}
	next.Listen, next.UI.Listen = cur.Listen, cur.UI.Listen
	next.Upstreams = maps.Clone(cur.Upstreams)
	next.Vault = cur.Vault
	next.AllowNonLoopback = cur.AllowNonLoopback
	next.Audit = cur.Audit
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
	if a.decisions != nil {
		if cerr := a.decisions.Close(); cerr != nil && err == nil {
			err = errors.New("shutdown: decision log close failed")
		}
	}
	if a.vaultLock != nil {
		if cerr := a.vaultLock.Close(); cerr != nil && err == nil {
			err = errors.New("shutdown: vault lock close failed")
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
