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

Requirements: Go 1.24 or newer and an existing qBittorrent configuration. Start and quit qBittorrent once if it has never run.

```sh
go build -o qbt-proton-guard ./cmd/qbt-proton-guard
./qbt-proton-guard status
./qbt-proton-guard install
```

`install` first stops and removes any previous guard and status processes, enforces a safe binding, then installs a per-user background service and status app:

- macOS: LaunchAgents and a native AppKit menu bar app
- Linux: systemd user services and a StatusNotifierItem tray app
- Windows: Task Scheduler logon tasks and a Win32 notification-area app

The status icon shows protection health, Proton's interface and address, the current forwarded port, qBittorrent's process/listener state, and the daemon heartbeat. It also owns all native notifications and uses the bundled qbt-proton-guard logo. Platform single-instance controls and service managers prevent duplicate status processes. No administrator privileges are required. The installed service follows the interactive user's qBittorrent configuration and Proton session.

To remove the service:

```sh
qbt-proton-guard uninstall
```

Uninstalling intentionally leaves qBittorrent's last safe interface binding in place.

## Commands

```text
qbt-proton-guard run [--interval 500ms]  Run the guard in the foreground
qbt-proton-guard once                    Enforce once, then exit
qbt-proton-guard status                  Show detected and configured state
qbt-proton-guard install                 Install and start at login
qbt-proton-guard uninstall               Remove the login service
qbt-proton-guard version                 Print the version
```

`status` reports the startup service, daemon heartbeat, last guard transition, Proton tunnel, forwarded-port lease, qBittorrent process, interface/address binding, listening port, Local Peer Discovery, and router port-mapping settings.

The background service sends a native desktop notification through the status app whenever its state changes, including VPN loss/recovery, binding corrections, qBittorrent state changes, forwarded-port acquisition/change, renewal failures, and configuration errors. Repeated polling results are deduplicated. No AppleScript notification path is used.

## Development

```sh
go test ./...
go vet ./...

GOOS=darwin  GOARCH=arm64 go build ./cmd/qbt-proton-guard
GOOS=linux   GOARCH=amd64 go build ./cmd/qbt-proton-guard
GOOS=windows GOARCH=amd64 go build ./cmd/qbt-proton-guard
```
