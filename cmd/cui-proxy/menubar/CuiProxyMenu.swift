// CuiProxyMenu: a menu-bar app that runs `cui-proxy serve` in the background
// and gives you a one-click way to open the dashboard.
//
// It is compiled on your own Mac by `cui-proxy menubar install` (needs the
// Xcode Command Line Tools for `swiftc`). It talks only to loopback addresses
// (it refuses a config whose proxy or dashboard listens elsewhere) and only
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
    if let e = ProcessInfo.processInfo.environment["CUI_PROXY_BIN"], !e.isEmpty { candidates.append(e) }
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

/// Runs a short command and returns (exit status, stdout). A command that
/// runs longer than `timeout` seconds is killed (status -1).
func runSync(_ exe: String, _ args: [String], timeout: TimeInterval = 5) -> (Int32, String) {
    let p = Process()
    p.executableURL = URL(fileURLWithPath: exe)
    p.arguments = args
    let out = Pipe()
    p.standardOutput = out
    p.standardError = FileHandle.nullDevice
    p.standardInput = FileHandle.nullDevice
    do { try p.run() } catch { return (-1, "") }
    let killer = DispatchWorkItem { if p.isRunning { p.terminate() } }
    DispatchQueue.global().asyncAfter(deadline: .now() + timeout, execute: killer)
    let data = out.fileHandleForReading.readDataToEndOfFile()
    p.waitUntilExit()
    killer.cancel()
    return (p.terminationStatus, String(data: data, encoding: .utf8) ?? "")
}

/// One HTTP exchange: (status code or 0, body).
func syncRequestFull(_ req: URLRequest) -> (Int, Data?) {
    let sem = DispatchSemaphore(value: 0)
    var status = 0
    var body: Data?
    URLSession.shared.dataTask(with: req) { data, resp, _ in
        if let h = resp as? HTTPURLResponse { status = h.statusCode; body = data }
        sem.signal()
    }.resume()
    _ = sem.wait(timeout: .now() + req.timeoutInterval + 1)
    return (status, body)
}

func httpGet(_ url: URL, timeout: TimeInterval = 1.0) -> Data? {
    let (status, body) = syncRequestFull(URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: timeout))
    return (200..<300).contains(status) ? body : nil
}

func httpPostJSON(_ url: URL, timeout: TimeInterval = 5.0) -> (Int, Data?) {
    var req = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: timeout)
    req.httpMethod = "POST"
    req.setValue("application/json", forHTTPHeaderField: "Content-Type")
    req.httpBody = Data("{}".utf8)
    return syncRequestFull(req)
}

/// True if something accepts TCP connections on host:port.
func tcpOpen(host: String, port: Int) -> Bool {
    var hints = addrinfo(ai_flags: 0, ai_family: AF_UNSPEC, ai_socktype: SOCK_STREAM, ai_protocol: 0,
                         ai_addrlen: 0, ai_canonname: nil, ai_addr: nil, ai_next: nil)
    var res: UnsafeMutablePointer<addrinfo>?
    guard getaddrinfo(host, String(port), &hints, &res) == 0, let first = res else { return false }
    defer { freeaddrinfo(res) }
    var cur: UnsafeMutablePointer<addrinfo>? = first
    while let ai = cur {
        let fd = socket(ai.pointee.ai_family, ai.pointee.ai_socktype, ai.pointee.ai_protocol)
        if fd >= 0 {
            let ok = connect(fd, ai.pointee.ai_addr, ai.pointee.ai_addrlen) == 0
            close(fd)
            if ok { return true }
        }
        cur = ai.pointee.ai_next
    }
    return false
}

/// Does a listener on this port belong to the current user? nil when we cannot
/// tell (lsof unavailable). lsof run without root lists only your own
/// processes' sockets, so "nothing found" means the listener is someone else's.
func listenerOwnedByMe(port: Int) -> Bool? {
    guard FileManager.default.isExecutableFile(atPath: "/usr/sbin/lsof") else { return nil }
    let (_, out) = runSync("/usr/sbin/lsof", ["-nP", "-iTCP:\(port)", "-sTCP:LISTEN", "-Fu"], timeout: 3)
    let uids = out.split(separator: "\n").filter { $0.hasPrefix("u") }.compactMap { Int($0.dropFirst()) }
    if uids.isEmpty { return false }
    return uids.allSatisfy { $0 == Int(getuid()) }
}

func isLoopback(_ host: String?) -> Bool {
    guard let h = host?.lowercased() else { return false }
    return h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "[::1]"
}

struct Info {
    var proxy: URL
    var dashboard: URL
    var version: String
    var proxyPort: Int { proxy.port ?? 80 }
    var dashboardPort: Int { dashboard.port ?? 80 }
}

enum InfoResult {
    case ok(Info)
    case failed(String)
}

func loadInfo(binary: String, config: String) -> InfoResult {
    let (code, out) = runSync(binary, ["info", "-config", config])
    if code == 2 { return .failed("this cui-proxy is too old for the menu-bar app (needs v0.1.1 or newer); run: brew upgrade cui-proxy") }
    guard code == 0 else { return .failed("the config is not valid (run: cui-proxy check-config -config \(config))") }
    var kv: [String: String] = [:]
    for line in out.split(separator: "\n") {
        if let i = line.firstIndex(of: "=") {
            kv[String(line[..<i])] = String(line[line.index(after: i)...])
        }
    }
    guard let p = kv["proxy_url"], let pu = URL(string: p),
          let d = kv["dashboard_url"], let du = URL(string: d) else {
        return .failed("unexpected output from cui-proxy info (is the installed binary older than this app?)")
    }
    guard isLoopback(pu.host), isLoopback(du.host) else {
        return .failed("the menu-bar app only supports loopback addresses; set listen and ui.listen to 127.0.0.1")
    }
    return .ok(Info(proxy: pu, dashboard: du, version: kv["version"] ?? "?"))
}

struct Counters {
    var requests = 0
    var blocked = 0
    var errors = 0
    var downgraded = 0
}

/// Parses /api/metrics. Returns nil unless it has the shape cui-proxy serves,
/// so another program that happens to answer 200 is not mistaken for it.
func parseCounters(_ data: Data) -> Counters? {
    guard let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
          obj["blocked_by_reason"] is [String: Any], obj["requests"] is NSNumber else { return nil }
    func n(_ k: String) -> Int { (obj[k] as? NSNumber)?.intValue ?? 0 }
    return Counters(requests: n("requests"), blocked: n("blocked"), errors: n("errors"), downgraded: n("downgraded"))
}

/// What a quick look at the ports shows.
struct Probe {
    var identityOK = false        // the dashboard answers like cui-proxy
    var proxyOpen = false         // the proxy port accepts connections
    var dashboardBusy = false     // something accepts connections on the dashboard port
    var owned: Bool?              // the dashboard listener is ours (nil: unknown)
    var counters: Counters?
    var alive: Bool { identityOK && proxyOpen }
}

func probe(_ inf: Info, checkOwner: Bool, timeout: TimeInterval = 1.0) -> Probe {
    var pr = Probe()
    let health = httpGet(inf.dashboard.appendingPathComponent("healthz"), timeout: timeout)
    if let h = health, String(data: h, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) == "ok",
       let m = httpGet(inf.dashboard.appendingPathComponent("api/metrics"), timeout: timeout),
       let c = parseCounters(m) {
        pr.identityOK = true
        pr.counters = c
    }
    pr.proxyOpen = tcpOpen(host: inf.proxy.host ?? "127.0.0.1", port: inf.proxyPort)
    pr.dashboardBusy = pr.identityOK || tcpOpen(host: inf.dashboard.host ?? "127.0.0.1", port: inf.dashboardPort)
    if checkOwner && pr.identityOK { pr.owned = listenerOwnedByMe(port: inf.dashboardPort) }
    return pr
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
    private var sticky: String?        // a failure that stays until the user acts

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
        if info != nil { startProxy() }
        refreshIcon()
        timer = Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in self?.poll() }
        poll()
    }

    func applicationWillTerminate(_ notification: Notification) {
        stopProxyAndWait()
    }

    /// Re-reads the binary, config and `cui-proxy info`. Sets `state` only when
    /// something is wrong; success leaves it for the caller.
    private func reloadSettings() {
        binary = findBinary()
        config = findConfig()
        info = nil
        guard let b = binary else { state = .binaryMissing; return }
        if !FileManager.default.fileExists(atPath: config) { state = .needsSetup; return }
        switch loadInfo(binary: b, config: config) {
        case .ok(let i):
            info = i
            sticky = nil
            if state == .needsSetup || state == .binaryMissing { state = .stopped }
        case .failed(let why):
            sticky = why
            state = .failed(why)
        }
    }

    // MARK: proxy process

    private func startProxy() {
        guard let b = binary else { state = .binaryMissing; refreshIcon(); return }
        guard let inf = info else { reloadSettings(); refreshIcon(); if info == nil { return }; startProxy(); return }
        if let p = process, p.isRunning { return }

        // Something already serves these ports? Attach to it if it is cui-proxy
        // and ours; otherwise do not start a second one (it could not bind).
        let pr = probe(inf, checkOwner: true, timeout: 0.5)
        if pr.alive {
            if pr.owned == false { state = .failed("port \(inf.dashboardPort) is served by another user's process"); refreshIcon(); return }
            state = .running(external: true); refreshIcon(); return
        }
        if pr.dashboardBusy {
            state = .failed(pr.identityOK ? "the dashboard answers but the proxy port \(inf.proxyPort) is not accepting connections"
                                          : "port \(inf.dashboardPort) is in use by something that is not cui-proxy")
            refreshIcon(); return
        }

        recentStarts.append(Date())

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
            sticky = "could not start cui-proxy"
            state = .failed("could not start cui-proxy")
        }
        refreshIcon()
    }

    private func proxyExited(_ proc: Process) {
        if process === proc { process = nil }
        if stoppedByUser { state = .stopped; refreshIcon(); return }
        let why = "cui-proxy exited (code \(proc.terminationStatus)); see the log"
        sticky = why
        state = .failed(why)
        refreshIcon()
        // Restart a crashed proxy automatically: the initial start plus up to
        // three restarts inside any one-minute window, so a crash loop stops.
        let now = Date()
        recentStarts = recentStarts.filter { now.timeIntervalSince($0) < 60 }
        if recentStarts.count <= 3 {
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
        guard let inf = info else { updateAfterPoll(nil); return }
        let ours = process?.isRunning ?? false
        pollQueue.async { [weak self] in
            let pr = probe(inf, checkOwner: !ours)
            DispatchQueue.main.async { self?.updateAfterPoll(pr) }
        }
    }

    private func updateAfterPoll(_ pr: Probe?) {
        if let c = pr?.counters { counters = c }

        // Not set up yet (or the binary / config changed): try again each poll,
        // so installing the binary or creating the config recovers on its own.
        if info == nil {
            if findBinary() != nil, FileManager.default.fileExists(atPath: config) {
                reloadSettings()
                if info != nil, process == nil { startProxy() }
            } else {
                binary = findBinary()
                if binary == nil { state = .binaryMissing }
                else if !FileManager.default.fileExists(atPath: config) { state = .needsSetup }
            }
            refreshIcon()
            return
        }
        guard let pr = pr, let inf = info else { refreshIcon(); return }

        if let p = process, p.isRunning {
            if pr.alive { state = .running(external: false); sticky = nil }
            else if Date().timeIntervalSince(startedAt) > 20 { state = .failed("cui-proxy is not responding; see the log") }
            else { state = .starting }
        } else if pr.alive {
            if pr.owned == false { state = .failed("port \(inf.dashboardPort) is served by another user's process") }
            else { state = .running(external: true); sticky = nil }
        } else if pr.identityOK {
            state = .failed("the dashboard answers but the proxy port \(inf.proxyPort) is not accepting connections")
        } else if pr.dashboardBusy {
            state = .failed("port \(inf.dashboardPort) is in use by something that is not cui-proxy")
        } else if let s = sticky {
            state = .failed(s)
        } else {
            state = .stopped
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
        case .needsSetup: return "Needs a config: choose “Create starter config…”"
        case .binaryMissing: return "cui-proxy not found (brew install rozdolsky33/tap/cui-proxy); checking again…"
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
        let ours = process?.isRunning ?? false
        let canManage = state != .needsSetup && state != .binaryMissing

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
        menu.addItem(item("Open Documentation", #selector(openDocumentation), key: "?"))
        menu.addItem(item("Online Documentation", #selector(openOnlineDocumentation)))
        menu.addItem(item("Copy Codex command", #selector(copyCodex), key: "c", enabled: info != nil))
        menu.addItem(.separator())

        if canManage && !running {
            menu.addItem(item("Start proxy", #selector(startAction), enabled: !ours && state != .starting))
        }
        if ours {
            menu.addItem(item("Stop proxy", #selector(stopAction)))
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

    @objc private func openDocumentation() {
        if let url = documentationURL(), NSWorkspace.shared.open(url) { return }
        let alert = NSAlert()
        alert.messageText = "Could not open local documentation"
        alert.informativeText = "Reinstall the bundled manual with: cui-proxy menubar install. If the manual is present, check your default browser."
        alert.alertStyle = .warning
        NSApp.activate(ignoringOtherApps: true)
        alert.runModal()
    }

    @objc private func openOnlineDocumentation() {
        NSWorkspace.shared.open(onlineDocumentationURL())
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
        sticky = nil
        if case .failed = state { state = .stopped }
        reloadSettings()
        if info != nil { startProxy() }
        refreshIcon()
    }

    @objc private func stopAction() { stopProxy() }

    @objc private func reloadRules() {
        guard let inf = info else { return }
        let url = inf.dashboard.appendingPathComponent("api/reload")
        pollQueue.async {
            let (status, body) = httpPostJSON(url)
            var detail = ""
            if let b = body, let obj = try? JSONSerialization.jsonObject(with: b) as? [String: Any] {
                if let e = obj["error"] as? String { detail = String(e.prefix(500)) }
                else if let e = obj["error"] as? [String: Any], let m = e["message"] as? String { detail = String(m.prefix(500)) }
            }
            DispatchQueue.main.async {
                let ok = (200..<300).contains(status)
                let a = NSAlert()
                a.messageText = ok ? "Rules reloaded" : "Reload failed"
                if ok {
                    a.informativeText = "The proxy is using the current config and dictionary."
                } else if status == 0 {
                    a.informativeText = "The dashboard did not answer."
                } else {
                    a.informativeText = (detail.isEmpty ? "HTTP \(status)." : detail)
                        + "\nThe running rules were kept. Check with: cui-proxy check-config -config " + self.config
                }
                a.alertStyle = ok ? .informational : .warning
                NSApp.activate(ignoringOtherApps: true)
                a.runModal()
            }
        }
    }

    @objc private func createConfig() {
        guard let b = binary else { return }
        let dir = (config as NSString).deletingLastPathComponent
        // `cui-proxy init` always writes config.yaml, so only offer this for that name.
        guard (config as NSString).lastPathComponent == "config.yaml" else {
            sticky = "create \(config) yourself: cui-proxy init writes config.yaml, then rename it"
            state = .failed(sticky!)
            refreshIcon()
            return
        }
        let (code, _) = runSync(b, ["init", "-dir", dir])
        if code == 0 {
            NSWorkspace.shared.open(URL(fileURLWithPath: dir + "/hosts.txt"))
            reloadSettings()
            if info != nil { startProxy() }
        } else {
            sticky = "could not create the config (it may already exist; run: cui-proxy init)"
            state = .failed(sticky!)
        }
        refreshIcon()
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
            do { try fm.removeItem(atPath: loginPlistPath) } catch { NSSound.beep() }
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
        do {
            try fm.createDirectory(atPath: (loginPlistPath as NSString).deletingLastPathComponent, withIntermediateDirectories: true)
            let data = try PropertyListSerialization.data(fromPropertyList: plist, format: .xml, options: 0)
            try data.write(to: URL(fileURLWithPath: loginPlistPath), options: .atomic)
        } catch {
            NSSound.beep()
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

// This works with no config, proxy process, network, or source checkout.
func documentationURL() -> URL? {
    guard let resource = Bundle.main.resourceURL else { return nil }
    let url = resource.appendingPathComponent("Manual", isDirectory: true).appendingPathComponent("index.html")
    guard FileManager.default.isReadableFile(atPath: url.path) else { return nil }
    return url
}

// The published manual for the installed version, written to Info.plist by
// `cui-proxy menubar install`. Falls back to the latest release.
func onlineDocumentationURL() -> URL {
    if let s = infoString("CUIProxyOnlineDocs"), let u = URL(string: s), u.scheme == "https" { return u }
    return URL(string: "https://rozdolsky33.github.io/cui-proxy/latest/")!
}

// MARK: - Entry point

if CommandLine.arguments.contains("--check") {
    // Headless self-check used by `cui-proxy menubar status` and the tests.
    let b = findBinary()
    let c = findConfig()
    print("binary=\(b ?? "NOT FOUND")")
    print("config=\(c)")
    print("documentation_url=\(documentationURL()?.absoluteString ?? "NOT FOUND")")
    print("online_documentation_url=\(onlineDocumentationURL().absoluteString)")
    print("config_exists=\(FileManager.default.fileExists(atPath: c))")
    if let b = b {
        switch loadInfo(binary: b, config: c) {
        case .ok(let inf):
            print("dashboard_url=\(inf.dashboard.absoluteString)")
            print("proxy_url=\(inf.proxy.absoluteString)")
            let pr = probe(inf, checkOwner: true, timeout: 1.0)
            print("proxy_responding=\(pr.alive)")
            if pr.dashboardBusy && !pr.identityOK { print("warning=the dashboard port is in use by something that is not cui-proxy") }
        case .failed(let why):
            print("problem=\(why)")
        }
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
