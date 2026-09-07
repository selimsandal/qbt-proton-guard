//go:build linux

package guard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/selimsandal/qbt-proton-guard/assets"
)

const serviceName = "qbt-proton-guard.service"
const statusServiceName = "qbt-proton-guard-status.service"

func Install() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	_ = exec.Command("systemctl", "--user", "disable", "--now", serviceName, statusServiceName).Run()
	binary := filepath.Join(home, ".local", "bin", "qbt-proton-guard")
	if err := copySelf(binary); err != nil {
		return err
	}
	if _, err := Enforce(context.Background()); err != nil {
		return fmt.Errorf("initial fail-closed enforcement: %w", err)
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return err
	}
	unit := fmt.Sprintf(`[Unit]
Description=Keep qBittorrent bound to Proton VPN
After=network.target

[Service]
ExecStart=%s run
Restart=always
RestartSec=2

[Install]
WantedBy=default.target
`, binary)
	if err := os.WriteFile(filepath.Join(unitDir, serviceName), []byte(unit), 0o644); err != nil {
		return err
	}
	statusUnit := fmt.Sprintf(`[Unit]
Description=qbt-proton-guard status icon and notifications
After=graphical-session.target %s

[Service]
ExecStart=%s ui
Restart=always
RestartSec=2

[Install]
WantedBy=default.target
`, serviceName, binary)
	if err := os.WriteFile(filepath.Join(unitDir, statusServiceName), []byte(statusUnit), 0o644); err != nil {
		return err
	}
	iconDir := filepath.Join(home, ".local", "share", "icons", "hicolor", "256x256", "apps")
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(iconDir, "qbt-proton-guard.png"), assets.AppIcon256PNG, 0o644); err != nil {
		return err
	}
	applicationsDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(applicationsDir, 0o755); err != nil {
		return err
	}
	desktopEntry := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=qbt-proton-guard\nComment=Proton VPN protection status for qBittorrent\nExec=%s ui\nIcon=qbt-proton-guard\nNoDisplay=true\n", binary)
	if err := os.WriteFile(filepath.Join(applicationsDir, "qbt-proton-guard.desktop"), []byte(desktopEntry), 0o644); err != nil {
		return err
	}
	if output, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("reload systemd: %w: %s", err, output)
	}
	if output, err := exec.Command("systemctl", "--user", "enable", "--now", serviceName, statusServiceName).CombinedOutput(); err != nil {
		return fmt.Errorf("enable service: %w: %s", err, output)
	}
	fmt.Printf("Installed and started %s and the status icon\n", serviceName)
	return nil
}

func Uninstall() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	_ = exec.Command("systemctl", "--user", "disable", "--now", serviceName, statusServiceName).Run()
	for _, path := range []string{
		filepath.Join(home, ".config", "systemd", "user", serviceName),
		filepath.Join(home, ".config", "systemd", "user", statusServiceName),
		filepath.Join(home, ".local", "share", "applications", "qbt-proton-guard.desktop"),
		filepath.Join(home, ".local", "share", "icons", "hicolor", "256x256", "apps", "qbt-proton-guard.png"),
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	if err := os.Remove(filepath.Join(home, ".local", "bin", "qbt-proton-guard")); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Printf("Uninstalled %s; the current fail-closed qBittorrent binding was retained\n", serviceName)
	return nil
}

func ServiceStatus() (string, error) {
	output, err := exec.Command("systemctl", "--user", "is-active", serviceName).CombinedOutput()
	status := strings.TrimSpace(string(output))
	if err != nil && status == "" {
		return "", err
	}
	return status, nil
}
