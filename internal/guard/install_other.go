//go:build !darwin && !linux && !windows

package guard

import (
	"fmt"
	"runtime"
)

func Install() error {
	return fmt.Errorf("service installation is unsupported on %s", runtime.GOOS)
}

func Uninstall() error {
	return fmt.Errorf("service installation is unsupported on %s", runtime.GOOS)
}

func ServiceStatus() (string, error) {
	return "unsupported", nil
}

func StatusAtLogin() (bool, error) {
	return false, fmt.Errorf("status icon login is unsupported on %s", runtime.GOOS)
}

func SetStatusAtLogin(bool) error {
	return fmt.Errorf("status icon login is unsupported on %s", runtime.GOOS)
}
