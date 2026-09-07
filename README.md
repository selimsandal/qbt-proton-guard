# qbt-proton-guard

`qbt-proton-guard` keeps qBittorrent bound exclusively to the currently connected Proton VPN tunnel on macOS, Linux, and Windows. It runs at login and protects qBittorrent even when qBittorrent is opened normally rather than through a special launcher.

## Safety model

The guard maintains this invariant in qBittorrent's native configuration:

- **Proton connected:** qBittorrent's network interface and optional bind address are the verified Proton tunnel and its assigned IP.
- **Proton disconnected, changing, or undetectable:** qBittorrent's network interface is `qbt-proton-guard-disabled`, a deliberately nonexistent interface.

qBittorrent 5.x treats a nonempty invalid interface as fail-closed. It passes that invalid interface to libtorrent and does not fall back to wildcard listeners. The guard stops a running qBittorrent process before correcting an unsafe or stale binding, verifies the persisted result, and only restarts it when Proton is connected.

Binding both values also prevents a stale interface name such as `utun5` from becoming usable if the operating system later reassigns that name to another tunnel. This approach protects normal Finder, Start-menu, desktop, terminal, and login launches because qBittorrent itself reads the fail-closed setting before opening BitTorrent sockets. It does not require the Web UI, credentials, split tunneling, or broad firewall rules.

The guard also disables Local Peer Discovery and qBittorrent's router-facing UPnP/NAT-PMP feature. These can create LAN-facing sockets and conflict with Proton's port forwarding.

## Port forwarding

When Proton Port Forwarding is enabled on a P2P server, the guard:

1. Requests matching UDP and TCP mappings directly from Proton's NAT-PMP gateway over the verified tunnel address.
2. Writes the randomized public port to qBittorrent's listening port.
3. Renews the lease after 75% of its server-provided lifetime.
4. Reacquires and resynchronizes the port when Proton changes tunnels or ports.

If Proton rejects or does not answer the mapping request, qBittorrent remains safely interface-bound and the guard logs that port forwarding is unavailable. Enable **Port Forwarding** in Proton VPN and reconnect to a P2P server to make a mapping available.

The guard is intended to prevent accidental leaks. A user or administrator who deliberately disables the service or replaces the program/configuration can bypass it; no program running under the same account can defend against that threat model.

## Proton detection

| Platform | Detection |
| --- | --- |
| macOS | `scutil --nc status ProtonVPN`, including its authoritative `InterfaceName` |
| Linux | An activated NetworkManager `ProtonVPN …` WireGuard/VPN connection and its device |
| Windows | Proton's fixed WireGuard GUIDs or OpenVPN adapter identities, plus an assigned IPv4 address and active VPN route |

Linux currently requires the official Proton VPN application using NetworkManager. Windows supports Proton's WireGuard UDP/TCP/TLS, OpenVPN TUN, and OpenVPN TAP adapters.

## Build and install

Requirements: an existing qBittorrent configuration, and Go 1.24 or newer when building from source. Start and quit qBittorrent once if it has never run. macOS installation and updates also require Apple's Command Line Tools (`xcode-select --install`) to build the native menu bar app.

Prebuilt executables for macOS, Linux, and Windows (Intel/AMD and ARM64) are available on the [latest release page](https://github.com/selimsandal/qbt-proton-guard/releases/latest). Download your platform's executable and `SHA256SUMS` from the same release. Verify the executable's SHA-256 against that file (`shasum -a 256 FILE` on macOS, `sha256sum FILE` on Linux, or `Get-FileHash FILE -Algorithm SHA256` in PowerShell), then run the downloaded executable with `install`. On macOS/Linux, first make it executable with `chmod +x FILE`.

To build from source:

```sh
go build -o qbt-proton-guard ./cmd/qbt-proton-guard
./qbt-proton-guard status
./qbt-proton-guard install
```

`install` stages the replacement files before stopping the existing services. On macOS, it also compiles and verifies the signed app and validates the LaunchAgent plists first. It checks service-stop and startup results, retains the previous files until startup succeeds, and attempts rollback if the upgrade fails. Your icon and notification preferences are preserved. Installation uses a per-user background service and status app:

- macOS: LaunchAgents and a native AppKit menu bar app
- Linux: systemd user services and a StatusNotifierItem tray app
- Windows: Task Scheduler logon tasks and a Win32 notification-area app

The status menu shows qBittorrent's running state, Proton's connection state, the guard service, and the forwarded port. Closed apps are normal idle states; outdated observations and failed inspections show “Unknown”, not a false disconnected or stopped state. **Details…** and **Copy full status** provide a timestamped diagnostic snapshot with interfaces, addresses, safety settings, and errors without cluttering the menu. On Linux, Details uses the default text-file viewer (`xdg-open`); copying requires `wl-copy` on Wayland or `xclip` on X11.

Use **Colored icon** in the status menu to switch between colored and monochrome icons. The preference is saved across restarts and reinstalls. On macOS, colored shields are green when protected, blue when idle, and orange when attention is needed; the default is the system monochrome style. Windows and Linux use the bundled logo in color by default, with a grayscale option.

Uncheck **Notifications** to mute banners and sounds without stopping protection or logging. This preference also persists across restarts and reinstalls. Messages received while muted are discarded, not replayed when notifications are enabled again. Notifications are enabled by default and remain subject to the operating system's notification permissions.

**Quit Status Icon** leaves the background guard running and does not immediately relaunch the icon. **Show icon at login** independently controls whether the icon appears at future logins, without stopping the current icon or protection. Reopen the app to restore the icon (or run `qbt-proton-guard ui` on Linux/Windows).

The status app also owns native notifications. Platform single-instance controls and service managers prevent duplicate status processes. No administrator privileges are required. The installed service follows the interactive user's qBittorrent configuration and Proton session.

To remove the service:

```sh
qbt-proton-guard uninstall
```

Uninstalling intentionally leaves qBittorrent's last safe interface binding in place.

## Updates and automatic releases

Choose **Update…** from the menu bar or system tray to update silently in the background—no terminal opens. The action is disabled while busy, and a one-time popup reports completion or failure instead of leaving result text in the menu. The result survives the status app's restart and isn't replayed at future logins. These user-requested responses aren't muted by the ordinary notification setting (Linux uses the desktop notification service). Detailed diagnostics are saved to `qbt-proton-guard/update.log` in the user's cache directory. You can still update directly:

```sh
qbt-proton-guard update --check  # Check only; changes nothing
qbt-proton-guard update          # Download, verify, and install the latest release
```

If the command isn't on your PATH, use `~/.local/bin/qbt-proton-guard` on macOS/Linux. In PowerShell, use `& "$env:LOCALAPPDATA\qbt-proton-guard\qbt-proton-guard.exe" update`.

The updater downloads only assets from this repository's latest GitHub release, verifies the signed checksum manifest and the executable's SHA-256 before executing anything, and reuses the staged installer and its rollback behavior. Preferences are preserved. Releases older than your installed release timestamp are not installed. On Windows the installer continues after the update command exits, so it can replace the CLI executable; a popup reports completion, or check `version` afterwards. Network or verification failures leave the installation untouched. Updates run only when explicitly requested; there are no scheduled automatic checks or installations.

Every push to `main` that passes tests on macOS, Linux, and Windows publishes a release with all seven binaries, signed checksums, and generated release notes: macOS and Windows on AMD64/ARM64, and Linux on AMD64/ARM64/RISC-V 64 (`riscv64`). The updater selects the matching architecture automatically. Pull requests and other branches only run tests. Versions use Harness's `0.0.<epoch>-g<commit>` format, with the commit's UTC epoch and seven-character SHA so reruns have the same identity. Published releases aren't overwritten. Only the current `main` tip is promoted to Latest. Source builds report `dev`; release builds embed the generated version in the CLI and macOS app metadata.

### Update authenticity

HTTPS certificate and hostname verification remain enabled. Requests and redirects are restricted to exact GitHub API, GitHub, and GitHub release-storage hostnames; HTTP downgrades, lookalike domains, URL credentials, and nonstandard ports are rejected. An Ed25519 public key embedded in the installed app verifies `SHA256SUMS.sig` before trusting any checksum. The signed payload is `qbt-proton-guard-release\n<tag>\n` followed by the exact `SHA256SUMS` bytes, binding hashes to a release identity. Missing/forged signatures, modified checksums, relabelled releases, and altered binaries are rejected before execution—even if an attacker can replace HTTPS responses through a locally trusted interception proxy. Attackers can still block updates; compromise of the signing key or the local installed app is outside this protection.

Release maintainers must configure the repository Actions secret **RELEASE_SIGNING_KEY** with the Ed25519 private-key PEM corresponding to `internal/selfupdate/release-public-key.pem`. Publication fails if the secret is absent or doesn't match. Keep the private key outside the repository and back it up securely; never include it in release assets. A first installation still requires a trusted source for the initial executable/public key.

## Commands

```text
qbt-proton-guard run [--interval 500ms]  Run the guard in the foreground
qbt-proton-guard once                    Enforce once, then exit
qbt-proton-guard status                  Show detected and configured state
qbt-proton-guard details                 Show the last diagnostic snapshot
qbt-proton-guard ui                      Open the status icon (Linux/Windows)
qbt-proton-guard icon-login status|on|off Show or change icon login behavior
qbt-proton-guard install                 Install and start at login
qbt-proton-guard uninstall               Remove the login service
qbt-proton-guard update [--check]         Install or check the latest release
qbt-proton-guard version                 Print the version
```

`status` reports the startup service, daemon heartbeat, last guard transition, Proton tunnel, forwarded-port lease, qBittorrent process, interface/address binding, listening port, Local Peer Discovery, and router port-mapping settings.

Ordinary startup, app closures, and short inspection failures are silent. The guard notifies when it intervenes, when a protection or port-forwarding failure persists for three seconds, and when a previously reported failure recovers. Repeated polling results are deduplicated. No AppleScript notification path is used.

## Development

```sh
go test ./...
go vet ./...

GOOS=darwin  GOARCH=arm64 go build ./cmd/qbt-proton-guard
GOOS=linux   GOARCH=amd64 go build ./cmd/qbt-proton-guard
GOOS=windows GOARCH=amd64 go build ./cmd/qbt-proton-guard
```
