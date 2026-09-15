package selfupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Status struct {
	Message   string `json:"message"`
	Error     string `json:"error"`
	Busy      bool   `json:"busy"`
	UpdatedAt int64  `json:"updated_at"`
	CheckedAt int64  `json:"checked_at"`
}

func statusDirectory() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "qbt-proton-guard")
	return dir, os.MkdirAll(dir, 0o700)
}

func WriteStatus(message string, busy bool, detail string) error {
	dir, err := statusDirectory()
	if err != nil {
		return err
	}
	data, err := json.Marshal(Status{Message: message, Error: detail, Busy: busy, UpdatedAt: time.Now().Unix()})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".update-status-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(dir, "update-status.json"))
}

func ReadStatus() Status {
	dir, err := statusDirectory()
	if err != nil {
		return Status{}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "update-status.json"))
	var state Status
	_ = json.Unmarshal(data, &state)
	if state.Busy && time.Now().Unix()-state.UpdatedAt > 600 {
		return Status{Message: "Update interrupted — try again"}
	}
	return state
}

func Finish(err error) {
	if err != nil {
		_ = WriteStatus("Update failed — try again", false, err.Error())
	} else {
		_ = WriteStatus("Update installed", false, "")
	}
}

func FinishCheck(err error, available bool) {
	now := time.Now().Unix()
	if err != nil {
		_ = writeStatus(Status{Message: "Update check failed — try again", Error: err.Error(), CheckedAt: now, UpdatedAt: now})
		return
	}
	message := "Up to date"
	if available {
		message = "Update available"
	}
	_ = writeStatus(Status{Message: message, CheckedAt: now, UpdatedAt: now})
}

// TakeResult returns a result requested from the status menu once. Its marker
// survives icon replacement and prevents an old result from appearing at login.
func TakeResult() (Status, bool) {
	state := ReadStatus()
	if state.Busy || state.Message == "" {
		return Status{}, false
	}
	dir, err := statusDirectory()
	if err != nil {
		return Status{}, false
	}
	if err := os.Remove(filepath.Join(dir, "update-popup.pending")); err != nil {
		return Status{}, false
	}
	return state, true
}

// StartBackground runs outside the status service so replacing that service does
// not terminate the updater. Both status icons read the same progress.
func StartBackground() error {
	if ReadStatus().Busy {
		return nil
	}
	dir, err := statusDirectory()
	if err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	if err := WriteStatus("Checking for updates…", true, ""); err != nil {
		return err
	}
	marker := filepath.Join(dir, "update-popup.pending")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		Finish(err)
		return err
	}
	err = startBackground(binary, filepath.Join(dir, "update.log"), "update")
	if err != nil {
		Finish(err)
		_ = os.Remove(marker) // The initiating UI displays launch errors directly.
	}
	return err
}

// StartCheckIfDue checks for releases at most once a day. It reports releases;
// the user chooses whether to install one.
func StartCheckIfDue() error {
	status := ReadStatus()
	if status.Busy || time.Since(time.Unix(status.CheckedAt, 0)) < 24*time.Hour {
		return nil
	}
	dir, err := statusDirectory()
	if err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	if err := WriteStatus("Checking for updates…", true, ""); err != nil {
		return err
	}
	if err := startBackground(binary, filepath.Join(dir, "update.log"), "update", "--check"); err != nil {
		FinishCheck(err, false)
		return err
	}
	return nil
}

func writeStatus(state Status) error {
	dir, err := statusDirectory()
	if err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".update-status-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(dir, "update-status.json"))
}
