//go:build darwin

package guard

import (
	"context"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/selimsandal/qbt-proton-guard/assets"
)

const serviceLabel = "com.qbt-proton-guard.agent"
const statusLabel = "com.qbt-proton-guard.status"

func Install() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+serviceLabel).Run()
	_ = exec.Command("launchctl", "bootout", domain+"/"+statusLabel).Run()
	binary := filepath.Join(home, ".local", "bin", "qbt-proton-guard")
	if err := copySelf(binary); err != nil {
		return err
	}
	if err := installNotifier(home); err != nil {
		return err
	}
	if _, err := Enforce(context.Background()); err != nil {
		return fmt.Errorf("initial fail-closed enforcement: %w", err)
	}
	logs := filepath.Join(home, "Library", "Logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		return err
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", serviceLabel+".plist")
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string><string>run</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>2</integer>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, serviceLabel, html.EscapeString(binary), html.EscapeString(filepath.Join(logs, "qbt-proton-guard.log")), html.EscapeString(filepath.Join(logs, "qbt-proton-guard.log")))
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write LaunchAgent: %w", err)
	}
	if output, err := exec.Command("launchctl", "bootstrap", domain, plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("start LaunchAgent: %w: %s", err, output)
	}
	statusPlistPath := filepath.Join(home, "Library", "LaunchAgents", statusLabel+".plist")
	statusBinary := filepath.Join(home, "Applications", "qbt-proton-guard.app", "Contents", "MacOS", "qbt-proton-guard")
	statusPlist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>2</integer>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, statusLabel, html.EscapeString(statusBinary), html.EscapeString(filepath.Join(logs, "qbt-proton-guard-status.log")), html.EscapeString(filepath.Join(logs, "qbt-proton-guard-status.log")))
	if err := os.WriteFile(statusPlistPath, []byte(statusPlist), 0o644); err != nil {
		return fmt.Errorf("write status LaunchAgent: %w", err)
	}
	if output, err := exec.Command("launchctl", "bootstrap", domain, statusPlistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("start status LaunchAgent: %w: %s", err, output)
	}
	fmt.Printf("Installed and started %s and menu bar app\n", serviceLabel)
	return nil
}

func Uninstall() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+serviceLabel).Run()
	_ = exec.Command("launchctl", "bootout", domain+"/"+statusLabel).Run()
	if err := os.Remove(filepath.Join(home, "Library", "LaunchAgents", serviceLabel+".plist")); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(filepath.Join(home, "Library", "LaunchAgents", statusLabel+".plist")); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(filepath.Join(home, ".local", "bin", "qbt-proton-guard")); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.RemoveAll(filepath.Join(home, "Applications", "qbt-proton-guard.app")); err != nil {
		return err
	}
	fmt.Printf("Uninstalled %s; the current fail-closed qBittorrent binding was retained\n", serviceLabel)
	return nil
}

func installNotifier(home string) error {
	applications := filepath.Join(home, "Applications")
	if err := os.MkdirAll(applications, 0o755); err != nil {
		return fmt.Errorf("create Applications directory: %w", err)
	}
	app := filepath.Join(applications, "qbt-proton-guard.app")
	if err := os.RemoveAll(app); err != nil {
		return fmt.Errorf("replace native notifier: %w", err)
	}
	macOS := filepath.Join(app, "Contents", "MacOS")
	resources := filepath.Join(app, "Contents", "Resources")
	if err := os.MkdirAll(macOS, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(resources, 0o755); err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "qbt-proton-guard-app-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	source := filepath.Join(temp, "MenuBarApp.swift")
	if err := os.WriteFile(source, assets.MacOSMenuBarSource, 0o600); err != nil {
		return err
	}
	output, err := exec.Command("swiftc", source, "-o", filepath.Join(macOS, "qbt-proton-guard"), "-framework", "AppKit", "-framework", "UserNotifications").CombinedOutput()
	if err != nil {
		return fmt.Errorf("compile menu bar app: %w: %s", err, output)
	}
	if err := buildAppIcon(temp, resources); err != nil {
		return err
	}
	info := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleExecutable</key><string>qbt-proton-guard</string>
  <key>CFBundleIdentifier</key><string>com.qbt-proton-guard.status</string>
  <key>CFBundleName</key><string>qbt-proton-guard</string>
  <key>CFBundleDisplayName</key><string>qbt-proton-guard</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.4.0</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>LSUIElement</key><true/>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(info), 0o644); err != nil {
		return err
	}
	if output, err := exec.Command("codesign", "--force", "--sign", "-", app).CombinedOutput(); err != nil {
		return fmt.Errorf("sign menu bar app: %w: %s", err, output)
	}
	return nil
}

func buildAppIcon(temp, resources string) error {
	source := filepath.Join(temp, "AppIcon-1024.png")
	if err := os.WriteFile(source, assets.AppIconPNG, 0o600); err != nil {
		return err
	}
	iconset := filepath.Join(temp, "AppIcon.iconset")
	if err := os.MkdirAll(iconset, 0o755); err != nil {
		return err
	}
	sizes := []struct {
		pixels int
		name   string
	}{
		{16, "icon_16x16.png"}, {32, "icon_16x16@2x.png"},
		{32, "icon_32x32.png"}, {64, "icon_32x32@2x.png"},
		{128, "icon_128x128.png"}, {256, "icon_128x128@2x.png"},
		{256, "icon_256x256.png"}, {512, "icon_256x256@2x.png"},
		{512, "icon_512x512.png"}, {1024, "icon_512x512@2x.png"},
	}
	for _, size := range sizes {
		output, err := exec.Command("sips", "-z", fmt.Sprint(size.pixels), fmt.Sprint(size.pixels), source, "--out", filepath.Join(iconset, size.name)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("render app icon: %w: %s", err, output)
		}
	}
	output, err := exec.Command("iconutil", "-c", "icns", iconset, "-o", filepath.Join(resources, "AppIcon.icns")).CombinedOutput()
	if err != nil {
		return fmt.Errorf("build app icon: %w: %s", err, output)
	}
	return nil
}

func ServiceStatus() (string, error) {
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), serviceLabel)
	output, err := exec.Command("launchctl", "print", target).CombinedOutput()
	if err != nil {
		return "not installed or stopped", nil
	}
	if strings.Contains(string(output), "state = running") {
		return "running", nil
	}
	return "loaded but not running", nil
}
