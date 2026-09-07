package selfupdate

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
)

func startBackground(binary, log string) error {
	const label = "com.qbt-proton-guard.update"
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	// An ephemeral plist without KeepAlive is genuinely one-shot; launchctl
	// submit implicitly enables KeepAlive and would retry failed updates forever.
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>%s</string><string>update</string></array><key>RunAtLoad</key><true/><key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string></dict></plist>`, label, html.EscapeString(binary), html.EscapeString(log), html.EscapeString(log))
	path := filepath.Join(filepath.Dir(log), "update-job.plist")
	if err := os.WriteFile(path, []byte(plist), 0o600); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", domain+"/"+label).Run()
	return exec.Command("launchctl", "bootstrap", domain, path).Run()
}
