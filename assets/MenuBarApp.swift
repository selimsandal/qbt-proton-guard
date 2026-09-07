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
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate, UNUserNotificationCenterDelegate {
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    private let menu = NSMenu()
    private var timer: Timer?
    private var currentState: GuardState?
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
            currentState = nil
            updateIcon(healthy: false)
            buildUnavailableMenu()
            return
        }
        currentState = state
        let fresh = Date().timeIntervalSince(state.updatedAt) < 10
        updateIcon(healthy: state.healthy && fresh)
        buildMenu(state: state, fresh: fresh)
    }

    private func updateIcon(healthy: Bool) {
        let symbol = healthy ? "checkmark.shield.fill" : "exclamationmark.shield.fill"
        let image = NSImage(systemSymbolName: symbol, accessibilityDescription: healthy ? "Protected" : "Attention")
        image?.isTemplate = true
        statusItem.button?.image = image
    }

    private func buildMenu(state: GuardState, fresh: Bool) {
        menu.removeAllItems()
        let title = state.healthy && fresh ? "Protected" : "Needs attention"
        addInfo(title, bold: true)
        addInfo(state.message)
        menu.addItem(.separator())
        addInfo("Service: \(fresh ? "Running" : "Heartbeat stale")")
        if state.protonConnected == true {
            addInfo("Proton VPN: \(state.protonInterface ?? "Connected") • \(state.protonAddress ?? "")")
        } else {
            addInfo("Proton VPN: Unavailable")
        }
        if let port = state.forwardedPort, state.portForwardingError == nil {
            addInfo("Forwarded port: \(port)")
        } else {
            addInfo("Forwarded port: Unavailable")
        }
        addInfo("qBittorrent: \(state.qbittorrentRunning == true ? "Running" : "Stopped")")
        if let port = state.qbittorrentPort {
            addInfo("Listening: \(state.qbittorrentAddress ?? "—"):\(port)")
        }
        addInfo("Last checked: \(relativeAge(state.updatedAt))")
        menu.addItem(.separator())
        addAction("Copy full status", action: #selector(copyStatus))
        addAction("Open guard log", action: #selector(openLog))
        menu.addItem(.separator())
        addAction("Quit Status Icon", action: #selector(quit))
    }

    private func buildUnavailableMenu() {
        menu.removeAllItems()
        addInfo("Guard status unavailable", bold: true)
        addInfo("The protection service may not be running.")
        menu.addItem(.separator())
        addAction("Open guard log", action: #selector(openLog))
        addAction("Quit Status Icon", action: #selector(quit))
    }

    private func addInfo(_ title: String, bold: Bool = false) {
        let label = NSTextField(labelWithString: title)
        label.font = bold ? .boldSystemFont(ofSize: 13) : .menuFont(ofSize: 0)
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

    private func relativeAge(_ date: Date) -> String {
        let seconds = max(0, Int(Date().timeIntervalSince(date)))
        if seconds < 2 { return "just now" }
        if seconds < 60 { return "\(seconds)s ago" }
        return "\(seconds / 60)m ago"
    }

    private func deliverQueuedNotifications() {
        let directory = cacheDirectory.appendingPathComponent("notifications", isDirectory: true)
        guard let files = try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)
            .filter({ $0.pathExtension == "pending" }).sorted(by: { $0.lastPathComponent < $1.lastPathComponent }) else { return }
        for file in files {
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
        guard let state = currentState else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(state.message, forType: .string)
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
        completionHandler([.banner, .sound])
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
