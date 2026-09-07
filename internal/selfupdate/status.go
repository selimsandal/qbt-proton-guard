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
	data, err := json.Marshal(Status{message, detail, busy, time.Now().Unix()})
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

// TakeResult consumes only results requested through the status menu. The marker
// survives replacement of the icon, but prevents old results replaying at login.
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

// StartBackground launches outside the status service so its replacement cannot
// terminate the updater. Progress is shared with the old and new status icons.
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
	err = startBackground(binary, filepath.Join(dir, "update.log"))
	if err != nil {
		Finish(err)
		_ = os.Remove(marker) // The initiating UI displays launch errors directly.
	}
	return err
}
