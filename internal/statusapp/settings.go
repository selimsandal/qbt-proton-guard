package statusapp

import (
	"os"
	"path/filepath"
	"strconv"
)

// Windows/Linux retain their existing colored icons and notifications by default.
func preferenceEnabled(name string) bool {
	path, err := preferencePath(name)
	if err != nil {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	enabled, err := strconv.ParseBool(string(data))
	return err != nil || enabled
}

func savePreference(name string, enabled bool) error {
	path, err := preferencePath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.FormatBool(enabled)), 0o600)
}

func preferencePath(name string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "qbt-proton-guard", name), nil
}
