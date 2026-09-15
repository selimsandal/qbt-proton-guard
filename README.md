# qbt-proton-guard

Keeps qBittorrent on Proton VPN and syncs its listening port with Proton port forwarding. It runs at login on macOS, Linux, and Windows. You do not need a web UI or a special qBittorrent launcher.

## How it works

- Binds qBittorrent to Proton's network interface and IP address.
- Sets a nonexistent interface when Proton is disconnected or unavailable, which prevents qBittorrent 5.x from using another connection.
- Stops qBittorrent before changing an incorrect or outdated binding. It restarts qBittorrent when Proton is connected.
- Disables Local Peer Discovery and router UPnP/NAT-PMP in qBittorrent.

Enable **Port Forwarding** in Proton VPN, then reconnect to a P2P server. The guard requests and renews the port automatically. If forwarding is unavailable, qBittorrent remains bound to the VPN.

Linux requires the official Proton VPN app using NetworkManager. Windows supports Proton's WireGuard and OpenVPN adapters.

## Install

Start and quit qBittorrent once to create its configuration. On macOS, install Apple's Command Line Tools with `xcode-select --install`. The menu bar app needs them during installation and updates.

Download your platform's executable and `SHA256SUMS` from the [latest release](https://github.com/selimsandal/qbt-proton-guard/releases/latest). Compare its SHA-256 with the checksum file:

- macOS: `shasum -a 256 FILE`
- Linux: `sha256sum FILE`
- PowerShell: `Get-FileHash FILE -Algorithm SHA256`

Run the downloaded executable with `install`. On macOS and Linux, first run `chmod +x FILE`.

Or build from source with Go 1.24 or newer:

```sh
go build -o qbt-proton-guard ./cmd/qbt-proton-guard
./qbt-proton-guard install
```

Installation starts a per-user background service and a menu bar or tray icon. It does not require administrator privileges.

## Use

The status menu shows qBittorrent, Proton VPN, the guard, and the forwarded port. Use **Details…** or **Copy full status** for diagnostics.

- **Colored icon** changes the icon style.
- **Notifications** controls banners and sounds. It does not affect protection.
- **Automatically check for updates** checks daily for a new release without installing it.
- **Show icon at login** controls whether the icon opens at login.
- **Quit Status Icon** closes the icon but leaves the guard running.

Preferences persist across restarts and updates. On Linux, **Details…** requires `xdg-open`. Copying requires `wl-copy` on Wayland or `xclip` on X11.

## Update

Choose **Update…** from the status menu, or run:

```sh
qbt-proton-guard update --check  # Check for an update
qbt-proton-guard update          # Download and install it
```

Updates verify the release signature and checksums before installation. You can disable automatic checks in the status menu. Automatic checks only report available releases; choose **Update…** to install one. The menu shows checking, up to date, update available, installed, and failed states. On Windows, installation continues after the command exits and reports the result in a popup.

If the command isn't on your PATH, use `~/.local/bin/qbt-proton-guard` on macOS/Linux, or `& "$env:LOCALAPPDATA\qbt-proton-guard\qbt-proton-guard.exe"` in PowerShell.

## Uninstall

```sh
qbt-proton-guard uninstall
```

Uninstall leaves qBittorrent's last interface binding in place. To use qBittorrent outside Proton afterward, change its network interface in **Settings → Advanced**.

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

Pushes to `main` publish a release after the test workflow passes. Publishing requires the `RELEASE_SIGNING_KEY` Actions secret to match `internal/selfupdate/release-public-key.pem`.

The maintainer's original private signing key is stored at `~/Google Drive/My Drive/qbt-proton-guard/signing-keys/release-signing-key.pem`. It moved from `~/.config/qbt-proton-guard/` on 2026-09-07. GitHub Actions stores a copy in `RELEASE_SIGNING_KEY`. Keep the Drive folder private and confirm that Google Drive has finished syncing before formatting or replacing the computer. The repository records this location only; it never contains private-key material.
