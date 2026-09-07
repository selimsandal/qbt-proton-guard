package selfupdate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateStatusLifecycle(t *testing.T) {
	for _, key := range []string{"HOME", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
		t.Setenv(key, t.TempDir())
	}
	if err := WriteStatus("Downloading update…", true, ""); err != nil {
		t.Fatal(err)
	}
	if s := ReadStatus(); !s.Busy || s.Message != "Downloading update…" {
		t.Fatalf("progress: %+v", s)
	}
	// Starting again while busy must not spawn another updater.
	if err := StartBackground(); err != nil {
		t.Fatal(err)
	}
	Finish(errors.New("checksum mismatch"))
	if s := ReadStatus(); s.Busy || s.Error != "checksum mismatch" {
		t.Fatalf("failure: %+v", s)
	}
	Finish(nil)
	if s := ReadStatus(); s.Busy || s.Error != "" || s.Message != "Update installed" {
		t.Fatalf("success: %+v", s)
	}
	dir, err := statusDirectory()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(Status{Busy: true, UpdatedAt: time.Now().Add(-11 * time.Minute).Unix()})
	if err := os.WriteFile(filepath.Join(dir, "update-status.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if s := ReadStatus(); s.Busy || s.Message != "Update interrupted — try again" {
		t.Fatalf("interrupted: %+v", s)
	}
}

func TestResultPopupIsRequestedOnceAndSurvivesRestart(t *testing.T) {
	for _, key := range []string{"HOME", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
		t.Setenv(key, t.TempDir())
	}
	Finish(nil)
	if _, ok := TakeResult(); ok {
		t.Fatal("unrequested result produced a popup")
	}
	dir, err := statusDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "update-popup.pending"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteStatus("Installing update…", true, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := TakeResult(); ok {
		t.Fatal("busy result consumed")
	}
	Finish(nil)
	// No in-memory UI state is required to retrieve completion after restart.
	if result, ok := TakeResult(); !ok || result.Message != "Update installed" {
		t.Fatalf("completion: %+v, %v", result, ok)
	}
	if _, ok := TakeResult(); ok {
		t.Fatal("result replayed")
	}
}
