//go:build linux

package guard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/selimsandal/qbt-proton-guard/assets"
)

const serviceName = "qbt-proton-guard.service"
const statusServiceName = "qbt-proton-guard-status.service"

func Install() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	binary := filepath.Join(home, ".local", "bin", "qbt-proton-guard")
	stage, err := os.MkdirTemp(home, ".qbt-proton-guard-upgrade-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	stagedBinary := filepath.Join(stage, "qbt-proton-guard")
	if err := copySelf(stagedBinary); err != nil {
		return err
	}
	if info, err := os.Stat(stagedBinary); err != nil || info.Size() == 0 || info.Mode()&0o111 == 0 {
		return fmt.Errorf("validate staged executable: %v", err)
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	iconDir := filepath.Join(home, ".local", "share", "icons", "hicolor", "256x256", "apps")
	applicationsDir := filepath.Join(home, ".local", "share", "applications")
	paths := []string{binary, filepath.Join(unitDir, serviceName), filepath.Join(unitDir, statusServiceName), filepath.Join(iconDir, "qbt-proton-guard.png"), filepath.Join(applicationsDir, "qbt-proton-guard.desktop")}
	staged := []string{stagedBinary, filepath.Join(stage, serviceName), filepath.Join(stage, statusServiceName), filepath.Join(stage, "qbt-proton-guard.png"), filepath.Join(stage, "qbt-proton-guard.desktop")}
	unit := fmt.Sprintf("[Unit]\nDescription=Keep qBittorrent bound to Proton VPN\nAfter=network.target\n\n[Service]\nExecStart=%s run\nRestart=always\nRestartSec=2\n\n[Install]\nWantedBy=default.target\n", binary)
	statusUnit := fmt.Sprintf("[Unit]\nDescription=qbt-proton-guard status icon and notifications\nAfter=graphical-session.target %s\n\n[Service]\nExecStart=%s ui\nRestart=on-failure\nRestartSec=2\n\n[Install]\nWantedBy=default.target\n", serviceName, binary)
	desktopEntry := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=qbt-proton-guard\nComment=Proton VPN protection status for qBittorrent\nExec=%s ui\nIcon=qbt-proton-guard\n", binary)
	for i, data := range [][]byte{[]byte(unit), []byte(statusUnit), assets.AppIcon256PNG, []byte(desktopEntry)} {
		if err := os.WriteFile(staged[i+1], data, 0o644); err != nil {
			return fmt.Errorf("stage service asset: %w", err)
		}
	}
	login := true
	if _, err := os.Stat(filepath.Join(home, ".config", "systemd", "user", statusServiceName)); err == nil {
		login, err = StatusAtLogin()
		if err != nil {
			return fmt.Errorf("read status icon login preference: %w", err)
		}
	}
	wasGuard := systemdActive(serviceName)
	wasStatus := systemdActive(statusServiceName)
	guardEnabled := systemdEnabled(serviceName)
	for _, name := range []string{serviceName, statusServiceName} {
		if !systemdActive(name) {
			continue
		}
		if output, err := installerCommand("systemctl", "--user", "stop", name).CombinedOutput(); err != nil {
			return errors.Join(fmt.Errorf("stop %s: %w: %s", name, err, output), restartLinux(wasGuard, wasStatus))
		}
	}
	replacements := make([]fileReplacement, 0, len(paths))
	rollback := func(cause error) error {
		result := cause
		// Stop replacement processes before restoring their executables and units.
		for _, name := range []string{statusServiceName, serviceName} {
			if systemdActive(name) {
				if err := installerCommand("systemctl", "--user", "stop", name).Run(); err != nil {
					return errors.Join(result, fmt.Errorf("stop replacement %s: %w; upgrade backups retained", name, err))
				}
			}
		}
		for i := len(replacements) - 1; i >= 0; i-- {
			result = errors.Join(result, replacements[i].rollback())
		}
		result = errors.Join(result, installerCommand("systemctl", "--user", "daemon-reload").Run())
		for _, entry := range []struct {
			name    string
			enabled bool
		}{{serviceName, guardEnabled}, {statusServiceName, login}} {
			if _, err := os.Stat(filepath.Join(unitDir, entry.name)); err == nil {
				result = errors.Join(result, setLinuxEnabled(entry.name, entry.enabled))
			} else if os.IsNotExist(err) {
				// Remove startup symlinks created by a failed first installation.
				result = errors.Join(result, setLinuxEnabled(entry.name, false))
			} else {
				result = errors.Join(result, err)
			}
		}
		result = errors.Join(result, restartLinux(wasGuard, wasStatus))
		return result
	}
	for i := range paths {
		r, err := replaceFile(staged[i], paths[i])
		if err != nil {
			return rollback(fmt.Errorf("install %s: %w", filepath.Base(paths[i]), err))
		}
		replacements = append(replacements, r)
	}
	if output, err := installerCommand("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return rollback(fmt.Errorf("reload systemd: %w: %s", err, output))
	}
	if output, err := installerCommand("systemctl", "--user", "enable", serviceName).CombinedOutput(); err != nil {
		return rollback(fmt.Errorf("enable service: %w: %s", err, output))
	}
	if err := SetStatusAtLogin(login); err != nil {
		return rollback(err)
	}
	if output, err := installerCommand("systemctl", "--user", "start", serviceName).CombinedOutput(); err != nil {
		return rollback(fmt.Errorf("start service: %w: %s", err, output))
	}
	if login || wasStatus {
		if output, err := installerCommand("systemctl", "--user", "start", statusServiceName).CombinedOutput(); err != nil {
			return rollback(fmt.Errorf("start status icon: %w: %s", err, output))
		}
	}
	if !waitLinuxActive(serviceName) {
		return rollback(fmt.Errorf("service did not become active"))
	}
	if (login || wasStatus) && !waitLinuxActive(statusServiceName) {
		return rollback(fmt.Errorf("status icon did not become active"))
	}
	for _, r := range replacements {
		if err := r.commit(); err != nil {
			return err
		}
	}
	fmt.Printf("Installed and started %s and the status icon\n", serviceName)
	return nil
}

func systemdEnabled(name string) bool {
	return installerCommand("systemctl", "--user", "is-enabled", "--quiet", name).Run() == nil
}
func setLinuxEnabled(name string, enabled bool) error {
	action := "disable"
	if enabled {
		action = "enable"
	}
	return installerCommand("systemctl", "--user", action, name).Run()
}

func systemdActive(name string) bool {
	return installerCommand("systemctl", "--user", "is-active", "--quiet", name).Run() == nil
}

func waitLinuxActive(name string) bool {
	for i := 0; i < 20; i++ {
		if systemdActive(name) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func restartLinux(guardWasActive, statusWasActive bool) error {
	var result error
	if guardWasActive {
		result = errors.Join(result, installerCommand("systemctl", "--user", "start", serviceName).Run())
	}
	if statusWasActive {
		result = errors.Join(result, installerCommand("systemctl", "--user", "start", statusServiceName).Run())
	}
	return result
}

// StatusAtLogin reports whether the status icon is enabled for user login.
func StatusAtLogin() (bool, error) {
	output, err := installerCommand("systemctl", "--user", "is-enabled", statusServiceName).CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(output)) == "enabled", nil
	}
	text := strings.TrimSpace(string(output))
	if text == "disabled" || text == "not-found" {
		return false, nil
	}
	return false, fmt.Errorf("query status icon login: %w: %s", err, output)
}

// SetStatusAtLogin changes future-login behavior without starting or stopping the icon.
func SetStatusAtLogin(enabled bool) error {
	action := "disable"
	if enabled {
		action = "enable"
	}
	output, err := installerCommand("systemctl", "--user", action, statusServiceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s status icon at login: %w: %s", action, err, output)
	}
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
