//go:build !windows

package selfupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallerHandoffAndCleanup(t *testing.T) {
	for _, success := range []bool{true, false} {
		dir, err := os.MkdirTemp(t.TempDir(), "download-")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "installer")
		script := "#!/bin/sh\n[ \"$#\" = 1 ] && [ \"$1\" = install-update ]\n"
		if !success {
			script += "exit 7\n"
		}
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := install(path, dir); (err == nil) != success {
			t.Fatalf("install success=%v: %v", success, err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("download not cleaned up: %v", err)
		}
	}
}
