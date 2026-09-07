//go:build darwin

package guard

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/selimsandal/qbt-proton-guard/assets"
)

const serviceLabel = "com.qbt-proton-guard.agent"
const statusLabel = "com.qbt-proton-guard.status"

var installerEnforce = Enforce
var installerSleep = time.Sleep

func Install() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	login := true
	statusPlistPath := filepath.Join(home, "Library", "LaunchAgents", statusLabel+".plist")
	if _, statErr := os.Stat(statusPlistPath); statErr == nil {
		login, err = StatusAtLogin()
		if err != nil {
			return err
		}
	}
	binary := filepath.Join(home, ".local", "bin", "qbt-proton-guard")
	stage, err := os.MkdirTemp(home, ".qbt-proton-guard-upgrade-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	stagedBinary := filepath.Join(stage, "qbt-proton-guard")
	stagedApp := filepath.Join(stage, "qbt-proton-guard.app")
	if err := copySelf(stagedBinary); err != nil {
		return err
	}
	if err := installNotifier(stagedApp); err != nil {
		return err
	}
	logs := filepath.Join(home, "Library", "Logs")
	plistPath := filepath.Join(home, "Library", "LaunchAgents", serviceLabel+".plist")
	stagedServicePlist := filepath.Join(stage, serviceLabel+".plist")
	stagedStatusPlist := filepath.Join(stage, statusLabel+".plist")
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
	statusBinary := filepath.Join(home, "Applications", "qbt-proton-guard.app", "Contents", "MacOS", "qbt-proton-guard")
	statusPlist := statusLaunchPlist(statusBinary, logs, login)
	for _, item := range []struct{ path, data string }{{stagedServicePlist, plist}, {stagedStatusPlist, statusPlist}} {
		if err := os.WriteFile(item.path, []byte(item.data), 0o644); err != nil {
			return err
		}
		if output, err := installerCommand("plutil", "-lint", item.path).CombinedOutput(); err != nil {
			return fmt.Errorf("validate %s: %w: %s", filepath.Base(item.path), err, output)
		}
	}
	was := map[string]bool{}
	wasRunning := map[string]bool{}
	for _, label := range []string{serviceLabel, statusLabel} {
		output, err := installerCommand("launchctl", "print", domain+"/"+label).CombinedOutput()
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 113 {
				continue
			}
			return fmt.Errorf("inspect LaunchAgent %s: %w: %s", label, err, output)
		}
		was[label] = true
		wasRunning[label] = strings.Contains(string(output), "state = running")
	}
	if !was[serviceLabel] {
		if _, err := installerEnforce(context.Background()); err != nil {
			return fmt.Errorf("initial fail-closed enforcement: %w", err)
		}
	}
	stopped := []string{}
	for _, label := range []string{serviceLabel, statusLabel} {
		if !was[label] {
			continue
		}
		if output, stopErr := installerCommand("launchctl", "bootout", domain+"/"+label).CombinedOutput(); stopErr != nil {
			restoreErr := restartDarwin(domain, stopped, plistPath, statusPlistPath, wasRunning)
			return errors.Join(fmt.Errorf("stop LaunchAgent %s: %w: %s", label, stopErr, output), restoreErr)
		}
		stopped = append(stopped, label)
	}
	// Finder-opened copies are not owned by the LaunchAgent. Use the validated
	// staged app to terminate only copies at the installed executable path.
	output, stopErr := installerCommand(filepath.Join(stagedApp, "Contents", "MacOS", "qbt-proton-guard"), "--stop-running", statusBinary).CombinedOutput()
	if stopErr != nil {
		return errors.Join(fmt.Errorf("stop standalone status app: %w: %s", stopErr, output), restartDarwin(domain, stopped, plistPath, statusPlistPath, wasRunning))
	}
	if strings.TrimSpace(string(output)) == "true" {
		was[statusLabel], wasRunning[statusLabel] = true, true
	}
	destinations := []string{binary, filepath.Join(home, "Applications", "qbt-proton-guard.app"), plistPath, statusPlistPath}
	staged := []string{stagedBinary, stagedApp, stagedServicePlist, stagedStatusPlist}
	replacements := []fileReplacement{}
	started := []string{}
	rollback := func(cause error) error {
		var recovery []error
		for i := len(started) - 1; i >= 0; i-- {
			label := started[i]
			if out, e := installerCommand("launchctl", "bootout", domain+"/"+label).CombinedOutput(); e != nil {
				return errors.Join(cause, fmt.Errorf("stop replacement %s: %w: %s; upgrade backups retained beside installed files", label, e, out))
			}
		}
		for i := len(replacements) - 1; i >= 0; i-- {
			if e := replacements[i].rollback(); e != nil {
				recovery = append(recovery, e)
			}
		}
		if e := restartDarwin(domain, labelsLoaded(was), plistPath, statusPlistPath, wasRunning); e != nil {
			recovery = append(recovery, e)
		}
		return errors.Join(append([]error{cause}, recovery...)...)
	}
	for i := range staged {
		r, e := replaceFile(staged[i], destinations[i])
		if e != nil {
			return rollback(e)
		}
		replacements = append(replacements, r)
	}
	if err := os.MkdirAll(logs, 0o755); err != nil {
		return rollback(err)
	}
	for _, item := range []struct{ label, path string }{{serviceLabel, plistPath}, {statusLabel, statusPlistPath}} {
		startStatus := item.label == statusLabel && (login || wasRunning[statusLabel])
		if item.label == statusLabel && !startStatus {
			continue
		}
		if output, e := installerCommand("launchctl", "bootstrap", domain, item.path).CombinedOutput(); e != nil {
			return rollback(fmt.Errorf("start %s: %w: %s", item.label, e, output))
		}
		started = append(started, item.label)
		// A disabled status plist is loaded to preserve an already-running icon, but
		// RunAtLoad=false intentionally needs an explicit one-time start.
		if item.label == statusLabel && !login {
			if output, e := installerCommand("launchctl", "kickstart", domain+"/"+statusLabel).CombinedOutput(); e != nil {
				return rollback(fmt.Errorf("restart current status icon: %w: %s", e, output))
			}
		}
		if !waitDarwinRunning(domain + "/" + item.label) {
			return rollback(fmt.Errorf("%s did not remain running", item.label))
		}
	}
	for _, r := range replacements {
		if e := r.commit(); e != nil {
			return e
		}
	}
	fmt.Printf("Installed and started %s and menu bar app\n", serviceLabel)
	return nil
}

func statusLaunchPlist(statusBinary, logs string, login bool) string {
	run, keepAlive := "<false/>", ""
	if login {
		run, keepAlive = "<true/>", "  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>\n"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string></array>
  <key>RunAtLoad</key>%s
%s  <key>ProcessType</key><string>Interactive</string>
  <key>ThrottleInterval</key><integer>2</integer>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, statusLabel, html.EscapeString(statusBinary), run, keepAlive, html.EscapeString(filepath.Join(logs, "qbt-proton-guard-status.log")), html.EscapeString(filepath.Join(logs, "qbt-proton-guard-status.log")))
}

func labelsLoaded(m map[string]bool) []string {
	var out []string
	for _, l := range []string{serviceLabel, statusLabel} {
		if m[l] {
			out = append(out, l)
		}
	}
	return out
}
func restartDarwin(domain string, labels []string, servicePath, statusPath string, wasRunning map[string]bool) error {
	var es []error
	for _, l := range labels {
		p := servicePath
		if l == statusLabel {
			p = statusPath
		}
		if out, e := installerCommand("launchctl", "bootstrap", domain, p).CombinedOutput(); e != nil {
			es = append(es, fmt.Errorf("restore %s: %w: %s", l, e, out))
			continue
		}
		if wasRunning[l] {
			if out, e := installerCommand("launchctl", "kickstart", domain+"/"+l).CombinedOutput(); e != nil {
				es = append(es, fmt.Errorf("restart previous %s: %w: %s", l, e, out))
			}
		}
	}
	return errors.Join(es...)
}
func waitDarwinRunning(target string) bool {
	consecutive := 0
	for i := 0; i < 20; i++ {
		out, e := installerCommand("launchctl", "print", target).CombinedOutput()
		if e == nil && strings.Contains(string(out), "state = running") {
			consecutive++
			if consecutive >= 5 {
				return true
			}
		} else {
			consecutive = 0
		}
		installerSleep(100 * time.Millisecond)
	}
	return false
}

// StatusAtLogin reports the status icon LaunchAgent's RunAtLoad preference.
func StatusAtLogin() (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	path := filepath.Join(home, "Library", "LaunchAgents", statusLabel+".plist")
	out, err := installerCommand("plutil", "-extract", "RunAtLoad", "raw", path).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("read status login setting: %w: %s", err, out)
	}
	switch strings.TrimSpace(string(out)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("invalid RunAtLoad value %q", strings.TrimSpace(string(out)))
}

// SetStatusAtLogin changes future-login behavior without stopping the current app.
func SetStatusAtLogin(enabled bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, "Library", "LaunchAgents", statusLabel+".plist")
	if _, err := os.Stat(path); err != nil {
		return err
	}
	logs := filepath.Join(home, "Library", "Logs")
	binary := filepath.Join(home, "Applications", "qbt-proton-guard.app", "Contents", "MacOS", "qbt-proton-guard")
	tmp, err := os.CreateTemp(filepath.Dir(path), ".status-plist-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = tmp.WriteString(statusLaunchPlist(binary, logs, enabled)); err == nil {
		err = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if err != nil {
		return err
	}
	if out, err := installerCommand("plutil", "-lint", tmpPath).CombinedOutput(); err != nil {
		return fmt.Errorf("validate status LaunchAgent: %w: %s", err, out)
	}
	r, err := replaceFile(tmpPath, path)
	if err != nil {
		return err
	}
	return r.commit()
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

func installNotifier(app string) error {
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
	output, err := installerCommand("swiftc", source, "-o", filepath.Join(macOS, "qbt-proton-guard"), "-framework", "AppKit", "-framework", "UserNotifications").CombinedOutput()
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
	if output, err := installerCommand("codesign", "--force", "--sign", "-", app).CombinedOutput(); err != nil {
		return fmt.Errorf("sign menu bar app: %w: %s", err, output)
	}
	if output, err := installerCommand("codesign", "--verify", "--deep", "--strict", app).CombinedOutput(); err != nil {
		return fmt.Errorf("verify menu bar app: %w: %s", err, output)
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
		output, err := installerCommand("sips", "-z", fmt.Sprint(size.pixels), fmt.Sprint(size.pixels), source, "--out", filepath.Join(iconset, size.name)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("render app icon: %w: %s", err, output)
		}
	}
	output, err := installerCommand("iconutil", "-c", "icns", iconset, "-o", filepath.Join(resources, "AppIcon.icns")).CombinedOutput()
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
