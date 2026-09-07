package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func install(path, dir string) error {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	// The installed CLI must exit before Windows can replace its executable.
	// PowerShell waits for this process, runs the downloaded installer, then
	// removes the download after it has exited and released its file handle.
	script := fmt.Sprintf(`$p = Get-Process -Id %d -ErrorAction SilentlyContinue; if ($p) { $p | Wait-Process }; & %s install-update; $result = $LASTEXITCODE; Remove-Item -LiteralPath %s -Recurse -Force; exit $result`, os.Getpid(), quote(path), quote(dir))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return fmt.Errorf("start update installer: %w", err)
	}
	fmt.Println("Update installer will continue after this command exits. Run qbt-proton-guard version afterwards to confirm completion.")
	return nil
}
