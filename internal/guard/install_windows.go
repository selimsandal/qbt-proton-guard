//go:build windows

package guard

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/selimsandal/qbt-proton-guard/assets"
)

const taskName = "qbt-proton-guard"
const statusTaskName = "qbt-proton-guard-status"

func Install() error {
	localData := os.Getenv("LOCALAPPDATA")
	if localData == "" {
		return fmt.Errorf("LOCALAPPDATA is not set")
	}
	installDir := filepath.Join(localData, "qbt-proton-guard")
	binaryPath := filepath.Join(installDir, "qbt-proton-guard.exe")
	stage, err := os.MkdirTemp(localData, ".qbt-proton-guard-upgrade-")
	if err != nil {
		return err
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.RemoveAll(stage)
		}
	}()
	stagedBinary := filepath.Join(stage, "qbt-proton-guard.exe")
	stagedIcon := filepath.Join(stage, "qbt-proton-guard.ico")
	if err := copySelf(stagedBinary); err != nil {
		return err
	}
	if info, err := os.Stat(stagedBinary); err != nil || info.Size() == 0 {
		return fmt.Errorf("validate staged executable: %v", err)
	}
	if err := writeWindowsIcon(stagedIcon); err != nil {
		return err
	}
	taskFiles := []string{filepath.Join(stage, "guard.xml"), filepath.Join(stage, "status.xml")}
	for i, args := range [][]string{{binaryPath, "run"}, {binaryPath, "ui"}} {
		if err := os.WriteFile(taskFiles[i], []byte(windowsTaskXML(args[0], args[1], true)), 0o600); err != nil {
			return fmt.Errorf("stage task: %w", err)
		}
	}
	names := []string{taskName, statusTaskName}
	oldXML := make([][]byte, 2)
	existed := make([]bool, 2)
	running := make([]bool, 2)
	login := true
	for i, name := range names {
		exists, err := windowsTaskExists(name)
		if err != nil {
			return fmt.Errorf("query existing task %s: %w", name, err)
		}
		if !exists {
			continue
		}
		out, queryErr := exportWindowsTask(name)
		if queryErr != nil {
			return fmt.Errorf("export existing task %s: %w: %s", name, queryErr, out)
		}
		existed[i] = true
		oldXML[i] = out
		if err := os.WriteFile(filepath.Join(stage, fmt.Sprintf("old-%d.xml", i)), out, 0o600); err != nil {
			return err
		}
		running[i] = windowsTaskRunning(name)
	}
	if existed[1] {
		login, err = windowsTaskEnabled(oldXML[1])
		if err != nil {
			return err
		}
	}
	// Preserve a deliberately disabled status task in the newly staged definition.
	if !login {
		if err := os.WriteFile(taskFiles[1], []byte(windowsTaskXML(binaryPath, "ui", false)), 0o600); err != nil {
			return err
		}
	}
	for i, name := range names {
		if !running[i] {
			continue
		}
		if out, err := installerCommand("schtasks.exe", "/End", "/TN", name).CombinedOutput(); err != nil {
			for j := 0; j < i; j++ {
				if running[j] {
					_ = installerCommand("schtasks.exe", "/Run", "/TN", names[j]).Run()
				}
			}
			return fmt.Errorf("stop task %s: %w: %s", name, err, out)
		}
	}
	replacements := make([]fileReplacement, 0, 2)
	rollback := func(cause error) error {
		var recovery error
		for _, name := range names {
			if windowsTaskRunning(name) {
				if err := installerCommand("schtasks.exe", "/End", "/TN", name).Run(); err != nil {
					keepStage = true
					return errors.Join(cause, fmt.Errorf("stop replacement %s: %w; recovery files retained at %s", name, err, stage))
				}
			}
		}
		for i := len(replacements) - 1; i >= 0; i-- {
			recovery = errors.Join(recovery, replacements[i].rollback())
		}
		for i, name := range names {
			if existed[i] {
				p := filepath.Join(stage, fmt.Sprintf("old-%d.xml", i))
				recovery = errors.Join(recovery, installerCommand("schtasks.exe", "/Create", "/F", "/TN", name, "/XML", p).Run())
			} else if exists, err := windowsTaskExists(name); err != nil {
				recovery = errors.Join(recovery, err)
			} else if exists {
				recovery = errors.Join(recovery, installerCommand("schtasks.exe", "/Delete", "/F", "/TN", name).Run())
			}
			if running[i] {
				recovery = errors.Join(recovery, startWindowsTask(name, i == 1 && !login))
			}
		}
		if recovery != nil {
			keepStage = true
			recovery = fmt.Errorf("rollback incomplete; recovery files retained at %s: %w", stage, recovery)
		}
		return errors.Join(cause, recovery)
	}
	for _, pair := range [][2]string{{stagedBinary, binaryPath}, {stagedIcon, filepath.Join(installDir, "qbt-proton-guard.ico")}} {
		r, err := replaceFile(pair[0], pair[1])
		if err != nil {
			return rollback(err)
		}
		replacements = append(replacements, r)
	}
	for i, name := range names {
		if out, err := installerCommand("schtasks.exe", "/Create", "/F", "/TN", name, "/XML", taskFiles[i]).CombinedOutput(); err != nil {
			return rollback(fmt.Errorf("create task %s: %w: %s", name, err, out))
		}
	}
	toStart := []bool{true, login || running[1]}
	for i, name := range names {
		if toStart[i] {
			if err := startWindowsTask(name, i == 1 && !login); err != nil {
				return rollback(fmt.Errorf("start task %s: %w", name, err))
			}
		}
	}
	for i, name := range names {
		if toStart[i] && !waitWindowsTaskRunning(name) {
			return rollback(fmt.Errorf("task %s did not start", name))
		}
	}
	for _, r := range replacements {
		if err := r.commit(); err != nil {
			return err
		}
	}
	fmt.Printf("Installed and started %s and the notification-area status icon\n", taskName)
	return nil
}

func windowsTaskXML(command, argument string, enabled bool) string {
	escape := func(s string) string { var b strings.Builder; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger></Triggers><Principals><Principal id="Author"><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><Enabled>%t</Enabled><ExecutionTimeLimit>PT0S</ExecutionTimeLimit></Settings><Actions Context="Author"><Exec><Command>%s</Command><Arguments>%s</Arguments></Exec></Actions></Task>`, enabled, escape(command), escape(argument))
}

func windowsTaskEnabled(data []byte) (bool, error) {
	var task struct {
		Settings struct {
			Enabled *bool `xml:"Enabled"`
		} `xml:"Settings"`
	}
	if err := xml.Unmarshal(data, &task); err != nil {
		return false, fmt.Errorf("decode task settings: %w", err)
	}
	return task.Settings.Enabled == nil || *task.Settings.Enabled, nil
}

func exportWindowsTask(name string) ([]byte, error) {
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[Text.Encoding]::UTF8; Export-ScheduledTask -TaskName '%s'`, strings.ReplaceAll(name, "'", "''"))
	data, err := installerCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	data = bytes.ReplaceAll(data, []byte(`encoding="UTF-16"`), []byte(`encoding="UTF-8"`))
	return data, nil
}

func startWindowsTask(name string, disabled bool) error {
	if disabled {
		if err := installerCommand("schtasks.exe", "/Change", "/TN", name, "/ENABLE").Run(); err != nil {
			return err
		}
	}
	err := installerCommand("schtasks.exe", "/Run", "/TN", name).Run()
	if disabled {
		err = errors.Join(err, installerCommand("schtasks.exe", "/Change", "/TN", name, "/DISABLE").Run())
	}
	return err
}
func windowsTaskExists(name string) (bool, error) {
	script := fmt.Sprintf(`if (Get-ScheduledTask -TaskName '%s' -ErrorAction SilentlyContinue) { '1' } else { '0' }`, strings.ReplaceAll(name, "'", "''"))
	out, err := installerCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "1", nil
}
func windowsTaskRunning(name string) bool {
	script := fmt.Sprintf(`if ((Get-ScheduledTask -TaskName '%s' -ErrorAction SilentlyContinue).State -eq 'Running') { '1' } else { '0' }`, strings.ReplaceAll(name, "'", "''"))
	out, err := installerCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	return err == nil && strings.TrimSpace(string(out)) == "1"
}
func waitWindowsTaskRunning(name string) bool {
	for i := 0; i < 20; i++ {
		if windowsTaskRunning(name) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func Uninstall() error {
	for _, name := range []string{taskName, statusTaskName} {
		_ = exec.Command("schtasks.exe", "/End", "/TN", name).Run()
		output, err := exec.Command("schtasks.exe", "/Delete", "/F", "/TN", name).CombinedOutput()
		if err != nil && !strings.Contains(strings.ToLower(string(output)), "cannot find") {
			return fmt.Errorf("delete startup task %s: %w: %s", name, err, output)
		}
	}
	fmt.Printf("Uninstalled %s; the installed executable and current fail-closed binding were retained\n", taskName)
	return nil
}

// StatusAtLogin reports whether the status icon scheduled task is enabled.
func StatusAtLogin() (bool, error) {
	output, err := exportWindowsTask(statusTaskName)
	if err != nil {
		return false, fmt.Errorf("query status startup task: %w: %s", err, output)
	}
	return windowsTaskEnabled(output)
}

// SetStatusAtLogin changes future-login behavior without ending or running the task.
func SetStatusAtLogin(enabled bool) error {
	change := "/DISABLE"
	if enabled {
		change = "/ENABLE"
	}
	output, err := installerCommand("schtasks.exe", "/Change", "/TN", statusTaskName, change).CombinedOutput()
	if err != nil {
		return fmt.Errorf("change status startup task: %w: %s", err, output)
	}
	return nil
}

func writeWindowsIcon(path string) error {
	var icon bytes.Buffer
	_ = binary.Write(&icon, binary.LittleEndian, uint16(0))
	_ = binary.Write(&icon, binary.LittleEndian, uint16(1))
	_ = binary.Write(&icon, binary.LittleEndian, uint16(1))
	icon.Write([]byte{0, 0, 0, 0}) // 256×256, no palette.
	_ = binary.Write(&icon, binary.LittleEndian, uint16(1))
	_ = binary.Write(&icon, binary.LittleEndian, uint16(32))
	_ = binary.Write(&icon, binary.LittleEndian, uint32(len(assets.AppIcon256PNG)))
	_ = binary.Write(&icon, binary.LittleEndian, uint32(22))
	icon.Write(assets.AppIcon256PNG)
	return os.WriteFile(path, icon.Bytes(), 0o644)
}

func ServiceStatus() (string, error) {
	output, err := exec.Command("schtasks.exe", "/Query", "/TN", taskName, "/FO", "LIST").CombinedOutput()
	if err != nil {
		return "not installed or stopped", nil
	}
	text := strings.ToLower(string(output))
	if strings.Contains(text, "status:") && strings.Contains(text, "running") {
		return "running", nil
	}
	return "installed but not running", nil
}
