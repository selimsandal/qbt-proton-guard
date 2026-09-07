package guard

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// installerCommand is replaceable by installer tests. Installers must use it for
// service-manager commands so tests never touch the host service manager.
var installerCommand = exec.Command

type fileReplacement struct {
	path, backup string
	existed      bool
}

func replaceFile(staged, destination string) (fileReplacement, error) {
	r := fileReplacement{path: destination}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return r, err
	}
	if _, err := os.Stat(destination); err == nil {
		r.existed = true
		placeholder, createErr := os.CreateTemp(filepath.Dir(destination), "."+filepath.Base(destination)+".upgrade-backup-*")
		if createErr != nil {
			return r, createErr
		}
		r.backup = placeholder.Name()
		if closeErr := placeholder.Close(); closeErr != nil {
			return r, closeErr
		}
		if removeErr := os.Remove(r.backup); removeErr != nil {
			return r, removeErr
		}
		if err := os.Rename(destination, r.backup); err != nil {
			return r, err
		}
	} else if !os.IsNotExist(err) {
		return r, err
	}
	if err := os.Rename(staged, destination); err != nil {
		if r.existed {
			if restoreErr := os.Rename(r.backup, destination); restoreErr != nil {
				return r, fmt.Errorf("install %s: %w; restore previous file: %v (backup retained at %s)", destination, err, restoreErr, r.backup)
			}
		}
		return r, err
	}
	return r, nil
}

func (r fileReplacement) rollback() error {
	if err := os.RemoveAll(r.path); err != nil {
		return fmt.Errorf("remove replacement %s: %w (backup retained at %s)", r.path, err, r.backup)
	}
	if r.existed {
		if err := os.Rename(r.backup, r.path); err != nil {
			return fmt.Errorf("restore %s: %w (backup retained at %s)", r.path, err, r.backup)
		}
	}
	return nil
}

func (r fileReplacement) commit() error {
	if !r.existed {
		return nil
	}
	if err := os.RemoveAll(r.backup); err != nil {
		return fmt.Errorf("remove upgrade backup %s: %w", r.backup, err)
	}
	return nil
}

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
