package guard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type RuntimeState struct {
	UpdatedAt                    time.Time `json:"updated_at"`
	PID                          int       `json:"pid"`
	Message                      string    `json:"message"`
	Healthy                      bool      `json:"healthy"`
	ForwardedPort                uint16    `json:"forwarded_port,omitempty"`
	PortForwardingError          string    `json:"port_forwarding_error,omitempty"`
	Error                        string    `json:"error,omitempty"`
	ProtonConnected              bool      `json:"proton_connected"`
	ProtonInterface              string    `json:"proton_interface,omitempty"`
	ProtonAddress                string    `json:"proton_address,omitempty"`
	QBittorrentRunning           bool      `json:"qbittorrent_running"`
	QBittorrentInterface         string    `json:"qbittorrent_interface,omitempty"`
	QBittorrentAddress           string    `json:"qbittorrent_address,omitempty"`
	QBittorrentPort              uint16    `json:"qbittorrent_port,omitempty"`
	LocalPeerDiscoveryDisabled   bool      `json:"local_peer_discovery_disabled"`
	RouterPortForwardingDisabled bool      `json:"router_port_forwarding_disabled"`
}

func ReadRuntimeState() (RuntimeState, error) {
	path, err := runtimeStatePath()
	if err != nil {
		return RuntimeState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeState{}, err
	}
	var state RuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return RuntimeState{}, fmt.Errorf("decode runtime state: %w", err)
	}
	return state, nil
}

func writeRuntimeState(state RuntimeState) error {
	path, err := runtimeStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replaceStateFile(tempName, path)
}

func runtimeStatePath() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "qbt-proton-guard", "state.json"), nil
}
