import AppKit
import UserNotifications

struct GuardState: Decodable {
    let updatedAt: Date
    let pid: Int
    let message: String
    let healthy: Bool
    let forwardedPort: UInt16?
    let portForwardingError: String?
    let error: String?
    let protonConnected: Bool?
    let protonInterface: String?
    let protonAddress: String?
    let qbittorrentRunning: Bool?
    let qbittorrentInterface: String?
    let qbittorrentAddress: String?
    let qbittorrentPort: UInt16?
    let localPeerDiscoveryDisabled: Bool?
    let routerPortForwardingDisabled: Bool?
    let protonDetectionError: String?
    let qbittorrentDetectionError: String?
    let safetyReadError: String?

    enum CodingKeys: String, CodingKey {
        case updatedAt = "updated_at"
        case pid, message, healthy
        case forwardedPort = "forwarded_port"
        case portForwardingError = "port_forwarding_error"
        case error
        case protonConnected = "proton_connected"
        case protonInterface = "proton_interface"
        case protonAddress = "proton_address"
        case qbittorrentRunning = "qbittorrent_running"
        case qbittorrentInterface = "qbittorrent_interface"
        case qbittorrentAddress = "qbittorrent_address"
        case qbittorrentPort = "qbittorrent_port"
        case localPeerDiscoveryDisabled = "local_peer_discovery_disabled"
        case routerPortForwardingDisabled = "router_port_forwarding_disabled"
        case protonDetectionError = "proton_detection_error"
        case qbittorrentDetectionError = "qbittorrent_detection_error"
        case safetyReadError = "safety_read_error"
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate, UNUserNotificationCenterDelegate {
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    private let menu = NSMenu()
    private var timer: Timer?
    private var showAtLogin: Bool?
    private var coloredIcon: Bool {
        UserDefaults.standard.bool(forKey: "coloredIcon")
    }
    private var notificationsEnabled: Bool {
        UserDefaults.standard.object(forKey: "notificationsEnabled") as? Bool ?? true
    }
    private let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { value in
            let container = try value.singleValueContainer()
            let string = try container.decode(String.self)
            let fractional = ISO8601DateFormatter()
            fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = fractional.date(from: string) { return date }
            let standard = ISO8601DateFormatter()
            standard.formatOptions = [.withInternetDateTime]
            if let date = standard.date(from: string) { return date }
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Invalid ISO 8601 date")
        }
        return decoder
    }()

    private var cacheDirectory: URL {
        FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("qbt-proton-guard", isDirectory: true)
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        menu.autoenablesItems = false
        UNUserNotificationCenter.current().delegate = self
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
        statusItem.button?.toolTip = "qbt-proton-guard"
        statusItem.menu = menu
        showAtLogin = (try? runGuard(["icon-login", "status"])) .flatMap { Bool($0.trimmingCharacters(in: .whitespacesAndNewlines)) }
        refresh()
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            self?.refresh()
            self?.deliverQueuedNotifications()
        }
        deliverQueuedNotifications()
    }

    private func refresh() {
        let stateURL = cacheDirectory.appendingPathComponent("state.json")
        guard let data = try? Data(contentsOf: stateURL), let state = try? decoder.decode(GuardState.self, from: data) else {
            updateIcon(symbol: "exclamationmark.shield.fill", description: "Status unavailable")
            buildUnavailableMenu()
            return
        }
        let fresh = Date().timeIntervalSince(state.updatedAt) < 10
        let inspectionFailed = [state.protonDetectionError, state.qbittorrentDetectionError, state.safetyReadError].contains { !($0 ?? "").isEmpty }
        if !fresh || inspectionFailed || !(state.error ?? "").isEmpty || (state.qbittorrentRunning == true && !state.healthy) {
            updateIcon(symbol: "exclamationmark.shield.fill", description: "Needs attention")
        } else if state.qbittorrentRunning == true && state.protonConnected == true {
            updateIcon(symbol: "checkmark.shield.fill", description: "Protected")
        } else {
            updateIcon(symbol: "shield", description: "Idle")
        }
        buildMenu(state: state, fresh: fresh)
    }

    private func updateIcon(symbol: String, description: String) {
        var image = NSImage(systemSymbolName: symbol, accessibilityDescription: description)
        if coloredIcon {
            let color: NSColor = symbol == "checkmark.shield.fill" ? .systemGreen : (symbol == "shield" ? .systemBlue : .systemOrange)
            image = image?.withSymbolConfiguration(.init(paletteColors: [color]))
        }
        image?.isTemplate = !coloredIcon
        statusItem.button?.image = image
    }

    private func buildMenu(state: GuardState, fresh: Bool) {
        menu.removeAllItems()
        let qbit = !fresh || state.qbittorrentRunning == nil || !(state.qbittorrentDetectionError ?? "").isEmpty ? "Unknown" : (state.qbittorrentRunning == true ? "Running" : "Not running")
        let vpn = !fresh || state.protonConnected == nil || !(state.protonDetectionError ?? "").isEmpty ? "Unknown" : (state.protonConnected == true ? "Connected" : "Disconnected")
        let service = !fresh ? "Status unavailable" : ((state.error ?? "").isEmpty && (state.safetyReadError ?? "").isEmpty ? "Running" : "Error — check log")
        var port = "Unknown"
        if fresh && state.protonConnected != nil && (state.protonDetectionError ?? "").isEmpty {
            port = "Not active"
            if state.protonConnected == true {
                if !(state.portForwardingError ?? "").isEmpty {
                    port = "Unavailable"
                } else if let forwardedPort = state.forwardedPort, forwardedPort != 0 {
                    port = String(forwardedPort)
                }
            }
        }
        addInfo("qBittorrent: \(qbit)")
        addInfo("Proton VPN: \(vpn)")
        addInfo("Guard: \(service)")
        addInfo("Forwarded port: \(port)")
        menu.addItem(.separator())
        addAction("Details…", action: #selector(showDetails))
        addAction("Copy full status", action: #selector(copyStatus))
        addAction("Open guard log", action: #selector(openLog))
        menu.addItem(.separator())
        addSettings()
        addAction("Quit Status Icon", action: #selector(quit))
    }

    private func buildUnavailableMenu() {
        menu.removeAllItems()
        addInfo("qBittorrent: Unknown")
        addInfo("Proton VPN: Unknown")
        addInfo("Guard: Status unavailable")
        addInfo("Forwarded port: Unknown")
        menu.addItem(.separator())
        addAction("Details…", action: #selector(showDetails))
        addAction("Copy full status", action: #selector(copyStatus))
        addAction("Open guard log", action: #selector(openLog))
        menu.addItem(.separator())
        addSettings()
        addAction("Quit Status Icon", action: #selector(quit))
    }

    private func addSettings() {
        let item = NSMenuItem(title: "Colored icon", action: #selector(toggleColoredIcon), keyEquivalent: "")
        item.target = self
        item.state = coloredIcon ? .on : .off
        menu.addItem(item)
        let notifications = NSMenuItem(title: "Notifications", action: #selector(toggleNotifications), keyEquivalent: "")
        notifications.target = self
        notifications.state = notificationsEnabled ? .on : .off
        menu.addItem(notifications)
        let login = NSMenuItem(title: showAtLogin == nil ? "Show icon at login (unavailable)" : "Show icon at login", action: #selector(toggleLogin), keyEquivalent: "")
        login.target = self
        login.state = showAtLogin == true ? .on : .off
        login.isEnabled = showAtLogin != nil
        menu.addItem(login)
    }

    @objc private func toggleColoredIcon() {
        UserDefaults.standard.set(!coloredIcon, forKey: "coloredIcon")
        refresh()
    }

    @objc private func toggleNotifications() {
        // Drain muted messages before re-enabling, including arrivals since the last tick.
        if !notificationsEnabled { deliverQueuedNotifications() }
        UserDefaults.standard.set(!notificationsEnabled, forKey: "notificationsEnabled")
        refresh()
    }

    private func addInfo(_ title: String) {
        let label = NSTextField(labelWithString: title)
        label.font = .menuFont(ofSize: 0)
        label.textColor = .labelColor
        let width = max(260, label.intrinsicContentSize.width + 28)
        label.frame = NSRect(x: 14, y: 3, width: width - 28, height: 18)
        let view = NSView(frame: NSRect(x: 0, y: 0, width: width, height: 22))
        view.addSubview(label)
        let item = NSMenuItem()
        item.view = view
        menu.addItem(item)
    }

    private func addAction(_ title: String, action: Selector) {
        let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
        item.target = self
        menu.addItem(item)
    }

    private func deliverQueuedNotifications() {
        let directory = cacheDirectory.appendingPathComponent("notifications", isDirectory: true)
        guard let files = try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)
            .filter({ $0.pathExtension == "pending" }).sorted(by: { $0.lastPathComponent < $1.lastPathComponent }) else { return }
        for file in files {
            if !notificationsEnabled {
                try? FileManager.default.removeItem(at: file)
                continue
            }
            guard let message = try? String(contentsOf: file, encoding: .utf8) else { continue }
            let content = UNMutableNotificationContent()
            content.title = "qbt-proton-guard"
            content.body = message
            content.sound = .default
            let request = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
            UNUserNotificationCenter.current().add(request)
            try? FileManager.default.removeItem(at: file)
        }
    }

    @objc private func copyStatus() {
        do {
            let report = try runGuard(["details"])
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(report, forType: .string)
        } catch { showError(error) }
    }

    @objc private func showDetails() {
        do {
            let report = try runGuard(["details"])
            let alert = NSAlert()
            alert.messageText = "qbt-proton-guard Details"
            alert.informativeText = "A snapshot of the last check. Protection continues in the background."
            let scroll = NSScrollView(frame: NSRect(x: 0, y: 0, width: 560, height: 380))
            scroll.hasVerticalScroller = true
            scroll.borderType = .bezelBorder
            let text = NSTextView(frame: scroll.bounds)
            text.isEditable = false
            text.isSelectable = true
            text.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
            text.textContainerInset = NSSize(width: 8, height: 8)
            text.isVerticallyResizable = true
            text.autoresizingMask = [.width]
            text.textContainer?.widthTracksTextView = true
            text.string = report
            scroll.documentView = text
            alert.accessoryView = scroll
            alert.addButton(withTitle: "Close")
            alert.addButton(withTitle: "Copy full status")
            NSApp.activate(ignoringOtherApps: true)
            if alert.runModal() == .alertSecondButtonReturn {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(report, forType: .string)
            }
        } catch { showError(error) }
    }

    @objc private func toggleLogin() {
        guard let enabled = showAtLogin else { return }
        do {
            _ = try runGuard(["icon-login", enabled ? "off" : "on"])
            showAtLogin = !enabled
            refresh()
        } catch { showError(error) }
    }

    private func runGuard(_ arguments: [String]) throws -> String {
        let process = Process()
        process.executableURL = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".local/bin/qbt-proton-guard")
        process.arguments = arguments
        let output = Pipe()
        process.standardOutput = output
        process.standardError = output
        try process.run()
        let data = output.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        let text = String(decoding: data, as: UTF8.self)
        guard process.terminationStatus == 0 else {
            throw NSError(domain: "qbt-proton-guard", code: Int(process.terminationStatus), userInfo: [NSLocalizedDescriptionKey: text])
        }
        return text
    }

    private func showError(_ error: Error) {
        NSApp.activate(ignoringOtherApps: true)
        NSAlert(error: error).runModal()
    }

    @objc private func openLog() {
        let log = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Logs/qbt-proton-guard.log")
        NSWorkspace.shared.open(log)
    }

    @objc private func quit() {
        NSApp.terminate(nil)
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification, withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler(notificationsEnabled ? [.banner, .sound] : [])
    }
}

// The staged installer uses this before replacing an app reopened from Finder,
// which is not owned by the status LaunchAgent.
if CommandLine.arguments.count == 3 && CommandLine.arguments[1] == "--stop-running" {
    let installed = URL(fileURLWithPath: CommandLine.arguments[2]).standardizedFileURL
    let running = NSWorkspace.shared.runningApplications.filter {
        $0.executableURL?.standardizedFileURL == installed
    }
    for application in running {
        if !application.terminate() && !application.isTerminated { exit(1) }
    }
    let deadline = Date().addingTimeInterval(5)
    while running.contains(where: { !$0.isTerminated }) && Date() < deadline {
        RunLoop.current.run(until: Date().addingTimeInterval(0.1))
    }
    if running.contains(where: { !$0.isTerminated }) { exit(1) }
    print(!running.isEmpty)
    exit(0)
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
