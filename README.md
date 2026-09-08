# qbt-proton-guard

Keeps qBittorrent on Proton VPN and syncs its listening port with Proton's port forwarding. Runs at login on macOS, Linux, and Windows. No Web UI or special qBittorrent launcher needed.

## How it works

- Binds qBittorrent to Proton's network interface and IP address.
- Sets a nonexistent interface when Proton is disconnected or can't be detected, so qBittorrent 5.x won't use another connection.
- Stops qBittorrent before changing an incorrect or outdated binding, and only restarts it when Proton is connected.
- Disables Local Peer Discovery and router UPnP/NAT-PMP in qBittorrent.

For port forwarding, enable **Port Forwarding** in Proton VPN and reconnect to a P2P server. The guard requests and renews the port automatically. If forwarding is unavailable, qBittorrent stays bound to the VPN.

Linux requires the official Proton VPN app using NetworkManager. Windows supports Proton's WireGuard and OpenVPN adapters.

## Install

Start and quit qBittorrent once to create its configuration. On macOS, also install Apple's Command Line Tools with `xcode-select --install`; they're needed to build the menu bar app, including during updates.

Download your platform's executable and `SHA256SUMS` from the [latest release](https://github.com/selimsandal/qbt-proton-guard/releases/latest). Compare its SHA-256 with the checksum file:

- macOS: `shasum -a 256 FILE`
- Linux: `sha256sum FILE`
- PowerShell: `Get-FileHash FILE -Algorithm SHA256`

Run the downloaded executable with `install`. On macOS and Linux, run `chmod +x FILE` first.

Or build from source with Go 1.24 or newer:

```sh
go build -o qbt-proton-guard ./cmd/qbt-proton-guard
./qbt-proton-guard install
```

Installation starts a per-user background service and a menu bar or tray icon. No administrator privileges are required.

## Use

The status menu shows qBittorrent, Proton VPN, the guard, and the forwarded port. Use **Details…** or **Copy full status** for diagnostics.

- **Colored icon** changes the icon style.
- **Notifications** toggles banners and sounds, not protection.
- **Show icon at login** controls whether the icon opens at login.
- **Quit Status Icon** closes the icon but leaves the guard running.

Preferences are saved across restarts and updates. On Linux, Details requires `xdg-open`; copying requires `wl-copy` on Wayland or `xclip` on X11.

## Update

Choose **Update…** from the status menu, or run:

```sh
qbt-proton-guard update --check  # Check for an update
qbt-proton-guard update          # Download and install it
```

Updates verify release signatures and checksums before installing. They run only when requested, not automatically. On Windows, installation finishes after the command exits; a popup reports the result.

If the command isn't on your PATH, use `~/.local/bin/qbt-proton-guard` on macOS/Linux, or `& "$env:LOCALAPPDATA\qbt-proton-guard\qbt-proton-guard.exe"` in PowerShell.

## Uninstall

```sh
qbt-proton-guard uninstall
```

This leaves qBittorrent's last interface binding in place. To use qBittorrent without Proton afterward, change its network interface in **Settings → Advanced**.

## Commands

```text
qbt-proton-guard run [--interval 500ms]  Run in the foreground
qbt-proton-guard once                   Apply settings once, then exit
qbt-proton-guard status                 Show current status
qbt-proton-guard details                Show the last diagnostic snapshot
qbt-proton-guard ui                     Open the status icon (Linux/Windows)
qbt-proton-guard icon-login status|on|off
qbt-proton-guard install
qbt-proton-guard uninstall
qbt-proton-guard update [--check]
qbt-proton-guard version
```

## Development

```sh
go test ./...
go vet ./...
```

Pushes to `main` that pass tests publish a release. Publishing requires the `RELEASE_SIGNING_KEY` Actions secret to match `internal/selfupdate/release-public-key.pem`.

The maintainer's original private signing key is stored in Google Drive at `~/Google Drive/My Drive/qbt-proton-guard/signing-keys/release-signing-key.pem` (moved from `~/.config/qbt-proton-guard/` on 2026-09-07). GitHub Actions retains its copy in `RELEASE_SIGNING_KEY`. Keep this Drive folder private and confirm Google Drive has finished syncing before formatting or replacing the computer. Only this location—not the private key contents—belongs in the repository.
