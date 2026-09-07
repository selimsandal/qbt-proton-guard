package guard

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

func copySelf(destination string) error {
	source, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate guard executable: %w", err)
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return fmt.Errorf("resolve guard executable: %w", err)
	}
	if source == destination {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create executable directory: %w", err)
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open guard executable: %w", err)
	}
	defer input.Close()
	temp, err := os.CreateTemp(filepath.Dir(destination), ".qbt-proton-guard-*")
	if err != nil {
		return fmt.Errorf("create temporary executable: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := io.Copy(temp, input); err != nil {
		temp.Close()
		return fmt.Errorf("copy guard executable: %w", err)
	}
	if err := temp.Chmod(0o755); err != nil {
		temp.Close()
		return fmt.Errorf("make guard executable: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close guard executable: %w", err)
	}
	if runtime.GOOS == "windows" {
		if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove previous guard executable: %w", err)
		}
	}
	if err := os.Rename(tempName, destination); err != nil {
		return fmt.Errorf("install guard executable: %w", err)
	}
	return nil
}
