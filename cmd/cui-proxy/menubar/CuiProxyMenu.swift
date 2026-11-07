// CuiProxyMenu: a menu-bar app that runs `cui-proxy serve` in the background
// and gives you a one-click way to open the dashboard.
//
// It is compiled on your own Mac by `cui-proxy menubar install` (needs the
// Xcode Command Line Tools for `swiftc`). It only talks to 127.0.0.1 and only
// runs the cui-proxy binary you installed.
import Cocoa

let bundleID = "io.github.rozdolsky33.cui-proxy-menu"

// MARK: - Locations

func defaultConfigPath() -> String {
    let env = ProcessInfo.processInfo.environment
    if let x = env["XDG_CONFIG_HOME"], !x.isEmpty { return x + "/cui-proxy/config.yaml" }
    return NSHomeDirectory() + "/.config/cui-proxy/config.yaml"
}

func infoString(_ key: String) -> String? {
    guard let v = Bundle.main.object(forInfoDictionaryKey: key) as? String, !v.isEmpty else { return nil }
    return v
}

func findBinary() -> String? {
    var candidates: [String] = []
    if let e = ProcessInfo.processInfo.environment["CUI_PROXY_BIN"] { candidates.append(e) }
    if let p = infoString("CUIProxyBinary") { candidates.append(p) }
    candidates += ["/opt/homebrew/bin/cui-proxy", "/usr/local/bin/cui-proxy", NSHomeDirectory() + "/.local/bin/cui-proxy"]
    return candidates.first { FileManager.default.isExecutableFile(atPath: $0) }
}

func findConfig() -> String {
    if let e = ProcessInfo.processInfo.environment["CUI_PROXY_CONFIG"], !e.isEmpty { return e }
    return infoString("CUIProxyConfig") ?? defaultConfigPath()
}

let logPath = NSHomeDirectory() + "/Library/Logs/cui-proxy/proxy.log"
let loginPlistPath = NSHomeDirectory() + "/Library/LaunchAgents/" + bundleID + ".plist"

// MARK: - Helpers

/// Runs a command and returns (exit status, stdout). Never blocks forever on a
/// hung child: callers only use it for short cui-proxy subcommands.
func runSync(_ exe: String, _ args: [String]) -> (Int32, String) {
    let p = Process()
    p.executableURL = URL(fileURLWithPath: exe)
    p.arguments = args
    let out = Pipe()
    p.standardOutput = out
    p.standardError = FileHandle.nullDevice
    do { try p.run() } catch { return (-1, "") }
    let data = out.fileHandleForReading.readDataToEndOfFile()
    p.waitUntilExit()
    return (p.terminationStatus, String(data: data, encoding: .utf8) ?? "")
}

func syncRequest(_ req: URLRequest) -> Data? {
    let sem = DispatchSemaphore(value: 0)
    var result: Data?
    URLSession.shared.dataTask(with: req) { data, resp, _ in
        if let h = resp as? HTTPURLResponse, (200..<300).contains(h.statusCode) { result = data }
        sem.signal()
    }.resume()
    _ = sem.wait(timeout: .now() + req.timeoutInterval + 1)
    return result
}

func httpGet(_ url: URL, timeout: TimeInterval = 1.0) -> Data? {
    syncRequest(URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: timeout))
}

func httpPostJSON(_ url: URL, timeout: TimeInterval = 5.0) -> Data? {
    var req = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: timeout)
    req.httpMethod = "POST"
    req.setValue("application/json", forHTTPHeaderField: "Content-Type")
    req.httpBody = Data("{}".utf8)
    return syncRequest(req)
}

struct Info {
    var proxy: URL
    var dashboard: URL
    var version: String
}

func loadInfo(binary: String, config: String) -> Info? {
    let (code, out) = runSync(binary, ["info", "-config", config])
    guard code == 0 else { return nil }
    var kv: [String: String] = [:]
    for line in out.split(separator: "\n") {
        if let i = line.firstIndex(of: "=") {
            kv[String(line[..<i])] = String(line[line.index(after: i)...])
        }
    }
    guard let p = kv["proxy_url"], let pu = URL(string: p),
          let d = kv["dashboard_url"], let du = URL(string: d) else { return nil }
    return Info(proxy: pu, dashboard: du, version: kv["version"] ?? "?")
}

struct Counters {
    var requests = 0
    var blocked = 0
    var errors = 0
    var downgraded = 0
}

func parseCounters(_ data: Data) -> Counters? {
    guard let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
    func n(_ k: String) -> Int { (obj[k] as? NSNumber)?.intValue ?? 0 }
    return Counters(requests: n("requests"), blocked: n("blocked"), errors: n("errors"), downgraded: n("downgraded"))
}

// MARK: - State

enum ProxyState: Equatable {
    case needsSetup          // no config file yet
    case binaryMissing       // cui-proxy not found
    case stopped
    case starting
    case running(external: Bool)   // external: started by someone else (e.g. a terminal)
    case failed(String)
}

// MARK: - App

final class AppDelegate: NSObject, NSApplicationDelegate, NSMenuDelegate {
    private var statusItem: NSStatusItem!
    private let menu = NSMenu()

    private var binary: String?
    private var config = findConfig()
    private var info: Info?

    private var process: Process?
    private var startedAt = Date.distantPast
    private var stoppedByUser = false
    private var recentStarts: [Date] = []

    private var state: ProxyState = .stopped
    private var counters = Counters()
    private var timer: Timer?
    private let pollQueue = DispatchQueue(label: "cui-proxy-menu.poll")

    // MARK: lifecycle

    func applicationDidFinishLaunching(_ notification: Notification) {
        // One instance only.
        let mine = ProcessInfo.processInfo.processIdentifier
        let others = NSRunningApplication.runningApplications(withBundleIdentifier: bundleID).filter { $0.processIdentifier != mine }
        if !others.isEmpty { NSApp.terminate(nil); return }

        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        menu.delegate = self
        menu.autoenablesItems = false
        statusItem.menu = menu
        refreshIcon()

        reloadSettings()
        startProxy()
        timer = Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in self?.poll() }
        poll()
    }

    func applicationWillTerminate(_ notification: Notification) {
        stopProxyAndWait()
    }

    private func reloadSettings() {
        binary = findBinary()
        config = findConfig()
        info = nil
        guard let b = binary else { state = .binaryMissing; return }
        if !FileManager.default.fileExists(atPath: config) { state = .needsSetup; return }
        info = loadInfo(binary: b, config: config)
    }

    // MARK: proxy process

    private func startProxy() {
        guard let b = binary else { state = .binaryMissing; refreshIcon(); return }
        guard FileManager.default.fileExists(atPath: config) else { state = .needsSetup; refreshIcon(); return }
        if info == nil { info = loadInfo(binary: b, config: config) }
        guard let inf = info else { state = .failed("the config is not valid (run: cui-proxy check-config)"); refreshIcon(); return }
        if let p = process, p.isRunning { return }

        // Something is already serving (for example a terminal): don't start a second one.
        if httpGet(inf.dashboard.appendingPathComponent("healthz"), timeout: 0.5) != nil {
            state = .running(external: true); refreshIcon(); return
        }

        let now = Date()
        recentStarts = recentStarts.filter { now.timeIntervalSince($0) < 60 }
        recentStarts.append(now)

        let fm = FileManager.default
        let logDir = (logPath as NSString).deletingLastPathComponent
        try? fm.createDirectory(atPath: logDir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        if let size = (try? fm.attributesOfItem(atPath: logPath))?[.size] as? Int, size > 5_000_000 {
            try? fm.removeItem(atPath: logPath + ".1")
            try? fm.moveItem(atPath: logPath, toPath: logPath + ".1")
        }
        if !fm.fileExists(atPath: logPath) { fm.createFile(atPath: logPath, contents: nil, attributes: [.posixPermissions: 0o600]) }
        let log = FileHandle(forWritingAtPath: logPath)
        _ = try? log?.seekToEnd()

        let p = Process()
        p.executableURL = URL(fileURLWithPath: b)
        p.arguments = ["serve", "-config", config]
        p.standardOutput = log
        p.standardError = log
        p.standardInput = FileHandle.nullDevice
        stoppedByUser = false
        p.terminationHandler = { [weak self] proc in
            DispatchQueue.main.async { self?.proxyExited(proc) }
        }
        do {
            try p.run()
            process = p
            startedAt = Date()
            state = .starting
        } catch {
            state = .failed("could not start cui-proxy")
        }
        refreshIcon()
    }

    private func proxyExited(_ proc: Process) {
        if process === proc { process = nil }
        if stoppedByUser { state = .stopped; refreshIcon(); return }
        state = .failed("cui-proxy exited (code \(proc.terminationStatus)); see the log")
        refreshIcon()
        // Restart a crashed proxy, but not in a tight loop.
        if recentStarts.count < 3 {
            DispatchQueue.main.asyncAfter(deadline: .now() + 2) { [weak self] in
                guard let self = self, !self.stoppedByUser, self.process == nil else { return }
                self.startProxy()
            }
        }
    }

    private func stopProxy() {
        guard let p = process, p.isRunning else { return }
        stoppedByUser = true
        p.terminate()
        DispatchQueue.main.asyncAfter(deadline: .now() + 12) {
            if p.isRunning { kill(p.processIdentifier, SIGKILL) }
        }
    }

    /// Used on quit: stop the child we started and wait (up to ~11 s) so
    /// in-flight requests drain and the vault is saved.
    private func stopProxyAndWait() {
        guard let p = process, p.isRunning else { return }
        stoppedByUser = true
        p.terminate()
        let deadline = Date().addingTimeInterval(11)
        while p.isRunning && Date() < deadline { Thread.sleep(forTimeInterval: 0.1) }
        if p.isRunning { kill(p.processIdentifier, SIGKILL) }
    }

    // MARK: polling

    private func poll() {
        guard let inf = info else { updateAfterPoll(alive: false, counters: nil); return }
        pollQueue.async { [weak self] in
            let alive = httpGet(inf.dashboard.appendingPathComponent("healthz")) != nil
            var c: Counters?
            if alive, let d = httpGet(inf.dashboard.appendingPathComponent("api/metrics")) { c = parseCounters(d) }
            DispatchQueue.main.async { self?.updateAfterPoll(alive: alive, counters: c) }
        }
    }

    private func updateAfterPoll(alive: Bool, counters c: Counters?) {
        if let c = c { counters = c }
        if info == nil, binary != nil, FileManager.default.fileExists(atPath: config) {
            // The config appeared (or became valid) since launch.
            reloadSettings()
            if info != nil, process == nil { startProxy() }
        }
        switch state {
        case .needsSetup, .binaryMissing:
            break
        default:
            if let p = process, p.isRunning {
                if alive { state = .running(external: false) }
                else if Date().timeIntervalSince(startedAt) > 20 { state = .failed("cui-proxy is not responding; see the log") }
                else { state = .starting }
            } else if alive {
                state = .running(external: true)
            } else if case .failed = state {
                // keep the failure visible until the user acts
            } else {
                state = .stopped
            }
        }
        refreshIcon()
    }

    // MARK: icon and menu

    private func refreshIcon() {
        guard let button = statusItem?.button else { return }
        let (symbol, fallback, tip): (String, String, String)
        switch state {
        case .running: (symbol, fallback, tip) = ("checkmark.shield", "CUI", "cui-proxy: running")
        case .starting: (symbol, fallback, tip) = ("shield", "CUI…", "cui-proxy: starting")
        case .stopped: (symbol, fallback, tip) = ("shield.slash", "CUI ✕", "cui-proxy: stopped")
        case .needsSetup: (symbol, fallback, tip) = ("exclamationmark.shield", "CUI !", "cui-proxy: needs a config")
        case .binaryMissing: (symbol, fallback, tip) = ("exclamationmark.shield", "CUI !", "cui-proxy: binary not found")
        case .failed: (symbol, fallback, tip) = ("exclamationmark.shield", "CUI !", "cui-proxy: problem")
        }
        if let img = NSImage(systemSymbolName: symbol, accessibilityDescription: tip) {
            img.isTemplate = true
            button.image = img
            button.title = ""
        } else {
            button.image = nil
            button.title = fallback
        }
        button.toolTip = tip
    }

    private func statusLine() -> String {
        switch state {
        case .running(let ext): return ext ? "Running (started elsewhere)" : "Running"
        case .starting: return "Starting…"
        case .stopped: return "Stopped"
        case .needsSetup: return "Needs a config: choose “Create starter config”"
        case .binaryMissing: return "cui-proxy not found (brew install rozdolsky33/tap/cui-proxy)"
        case .failed(let why): return why
        }
    }

    private func item(_ title: String, _ action: Selector?, key: String = "", enabled: Bool = true) -> NSMenuItem {
        let it = NSMenuItem(title: title, action: action, keyEquivalent: key)
        it.target = self
        it.isEnabled = enabled
        return it
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        menu.removeAllItems()
        let running: Bool
        if case .running = state { running = true } else { running = false }
        var ours = false
        if let p = process, p.isRunning { ours = true }

        menu.addItem(item("cui-proxy — " + statusLine(), nil, enabled: false))
        if running {
            menu.addItem(item("Requests \(counters.requests) · Blocked \(counters.blocked) · Errors \(counters.errors)", nil, enabled: false))
        }
        menu.addItem(.separator())

        if state == .needsSetup {
            menu.addItem(item("Create starter config…", #selector(createConfig)))
            menu.addItem(.separator())
        }
        menu.addItem(item("Open Dashboard", #selector(openDashboard), key: "d", enabled: running))
        menu.addItem(item("Copy Codex command", #selector(copyCodex), key: "c", enabled: info != nil))
        menu.addItem(.separator())

        if ours || (running == false && state != .needsSetup && state != .binaryMissing) {
            let canStart = !ours && state != .starting
            menu.addItem(item(ours ? "Stop proxy" : "Start proxy", ours ? #selector(stopAction) : #selector(startAction), enabled: ours || canStart))
        }
        menu.addItem(item("Reload rules", #selector(reloadRules), enabled: running))
        menu.addItem(.separator())

        menu.addItem(item("Open Config Folder", #selector(openConfigFolder)))
        menu.addItem(item("Open Log", #selector(openLog)))
        let login = item("Start at Login", #selector(toggleLogin))
        login.state = FileManager.default.fileExists(atPath: loginPlistPath) ? .on : .off
        menu.addItem(login)
        menu.addItem(.separator())
        menu.addItem(item("About cui-proxy \(info?.version ?? "")", nil, enabled: false))
        menu.addItem(item("Quit", #selector(quit), key: "q"))
    }

    // MARK: actions

    @objc private func openDashboard() {
        if let u = info?.dashboard { NSWorkspace.shared.open(u) }
    }

    @objc private func copyCodex() {
        guard let inf = info else { return }
        var base = inf.proxy.absoluteString
        while base.hasSuffix("/") { base.removeLast() }
        let cmd = "codex -c 'model_provider=\"cui-proxy\"' \\\n" +
            "      -c 'model_providers.cui-proxy={name=\"cui-proxy\",base_url=\"\(base)/v1\",wire_api=\"responses\",requires_openai_auth=true}'"
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(cmd, forType: .string)
    }

    @objc private func startAction() {
        recentStarts.removeAll()
        if case .failed = state { state = .stopped }
        startProxy()
    }

    @objc private func stopAction() { stopProxy() }

    @objc private func reloadRules() {
        guard let inf = info else { return }
        let url = inf.dashboard.appendingPathComponent("api/reload")
        pollQueue.async {
            let ok = httpPostJSON(url) != nil
            DispatchQueue.main.async {
                let a = NSAlert()
                a.messageText = ok ? "Rules reloaded" : "Reload failed"
                a.informativeText = ok ? "The proxy is using the current config and dictionary."
                                       : "The config or a dictionary file is invalid, so the running rules were kept. Run: cui-proxy check-config -config " + self.config
                a.alertStyle = ok ? .informational : .warning
                NSApp.activate(ignoringOtherApps: true)
                a.runModal()
            }
        }
    }

    @objc private func createConfig() {
        guard let b = binary else { return }
        let dir = (config as NSString).deletingLastPathComponent
        let (code, _) = runSync(b, ["init", "-dir", dir])
        if code == 0 {
            NSWorkspace.shared.open(URL(fileURLWithPath: dir + "/hosts.txt"))
            reloadSettings()
            startProxy()
        } else {
            state = .failed("could not create the config (it may already exist; run: cui-proxy init)")
            refreshIcon()
        }
    }

    @objc private func openConfigFolder() {
        let dir = (config as NSString).deletingLastPathComponent
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        NSWorkspace.shared.open(URL(fileURLWithPath: dir))
    }

    @objc private func openLog() {
        if FileManager.default.fileExists(atPath: logPath) { NSWorkspace.shared.open(URL(fileURLWithPath: logPath)) }
    }

    @objc private func toggleLogin() {
        let fm = FileManager.default
        if fm.fileExists(atPath: loginPlistPath) {
            try? fm.removeItem(atPath: loginPlistPath)
            return
        }
        guard let exe = Bundle.main.executablePath else { return }
        let plist: [String: Any] = [
            "Label": bundleID,
            "ProgramArguments": [exe],
            "RunAtLoad": true,
            "KeepAlive": false,
            "ProcessType": "Interactive",
            "LimitLoadToSessionType": "Aqua",
        ]
        try? fm.createDirectory(atPath: (loginPlistPath as NSString).deletingLastPathComponent, withIntermediateDirectories: true)
        if let data = try? PropertyListSerialization.data(fromPropertyList: plist, format: .xml, options: 0) {
            try? data.write(to: URL(fileURLWithPath: loginPlistPath), options: .atomic)
        }
    }

    /// Stop the proxy we started (draining in-flight requests, saving the
    /// vault), then exit. Also runs on SIGTERM/SIGINT/SIGHUP.
    func shutdown() {
        stopProxyAndWait()
        NSApp.terminate(nil)
    }

    @objc private func quit() { shutdown() }
}

// MARK: - Entry point

if CommandLine.arguments.contains("--check") {
    // Headless self-check used by `cui-proxy menubar status` and the tests.
    let b = findBinary()
    let c = findConfig()
    print("binary=\(b ?? "NOT FOUND")")
    print("config=\(c)")
    print("config_exists=\(FileManager.default.fileExists(atPath: c))")
    if let b = b, let inf = loadInfo(binary: b, config: c) {
        print("dashboard_url=\(inf.dashboard.absoluteString)")
        print("proxy_url=\(inf.proxy.absoluteString)")
        print("proxy_responding=\(httpGet(inf.dashboard.appendingPathComponent("healthz"), timeout: 1.0) != nil)")
    }
    exit(b == nil ? 1 : 0)
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.accessory)

// A signal (for example from `cui-proxy menubar install` replacing us) must
// stop the proxy we started, not orphan it.
var signalSources: [DispatchSourceSignal] = []
for sig in [SIGTERM, SIGINT, SIGHUP] {
    signal(sig, SIG_IGN)
    let src = DispatchSource.makeSignalSource(signal: sig, queue: .main)
    src.setEventHandler { delegate.shutdown() }
    src.resume()
    signalSources.append(src)
}

app.run()
