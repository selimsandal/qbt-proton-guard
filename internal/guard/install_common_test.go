package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileReplacementRollbackRestoresPreviousFile(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "installed")
	staged := filepath.Join(dir, "staged")
	if err := os.WriteFile(destination, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := replaceFile(staged, destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.rollback(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "old" {
		t.Fatalf("rollback content = %q, want old", b)
	}
}

func TestFileReplacementCommitRemovesBackup(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "installed")
	staged := filepath.Join(dir, "staged")
	_ = os.WriteFile(destination, []byte("old"), 0o755)
	_ = os.WriteFile(staged, []byte("new"), 0o755)
	r, err := replaceFile(staged, destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.backup); !os.IsNotExist(err) {
		t.Fatalf("backup still exists: %v", err)
	}
	b, _ := os.ReadFile(destination)
	if string(b) != "new" {
		t.Fatalf("installed content = %q, want new", b)
	}
}
