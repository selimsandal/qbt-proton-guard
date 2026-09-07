//go:build !windows

package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
)

func install(path, dir string) error {
	defer os.RemoveAll(dir)
	cmd := exec.Command(path, "install-update")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("update installer: %w", err)
	}
	fmt.Println("Update installed.")
	return nil
}
