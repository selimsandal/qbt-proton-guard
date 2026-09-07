//go:build windows

package guard

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	for _, name := range []string{taskName, statusTaskName} {
		_ = exec.Command("schtasks.exe", "/End", "/TN", name).Run()
		_ = exec.Command("schtasks.exe", "/Delete", "/F", "/TN", name).Run()
	}
	if err := copySelf(binaryPath); err != nil {
		return err
	}
	if err := writeWindowsIcon(filepath.Join(installDir, "qbt-proton-guard.ico")); err != nil {
		return err
	}
	if _, err := Enforce(context.Background()); err != nil {
		return fmt.Errorf("initial fail-closed enforcement: %w", err)
	}
	command := fmt.Sprintf(`"%s" run`, binaryPath)
	output, err := exec.Command("schtasks.exe", "/Create", "/F", "/SC", "ONLOGON", "/TN", taskName, "/TR", command).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create startup task: %w: %s", err, output)
	}
	statusCommand := fmt.Sprintf(`"%s" ui`, binaryPath)
	output, err = exec.Command("schtasks.exe", "/Create", "/F", "/SC", "ONLOGON", "/TN", statusTaskName, "/TR", statusCommand).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create status startup task: %w: %s", err, output)
	}
	output, err = exec.Command("schtasks.exe", "/Run", "/TN", taskName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start startup task: %w: %s", err, output)
	}
	output, err = exec.Command("schtasks.exe", "/Run", "/TN", statusTaskName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start status task: %w: %s", err, output)
	}
	fmt.Printf("Installed and started %s and the notification-area status icon\n", taskName)
	return nil
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
