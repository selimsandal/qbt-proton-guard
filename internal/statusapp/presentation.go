package statusapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/selimsandal/qbt-proton-guard/internal/guard"
)

// statusLines never presents an old observation as a current component state.
func statusLines(state guard.RuntimeState, stateErr error, now time.Time) []string {
	service, qbit, vpn, port := "Status unavailable", "Unknown", "Unknown", "Unknown"
	if stateErr == nil && now.Sub(state.UpdatedAt) < 10*time.Second {
		service, qbit, vpn, port = "Running", "Not running", "Disconnected", "Not active"
		if state.Error != "" || state.SafetyReadError != "" {
			service = "Error — check log"
		}
		if state.QBittorrentRunning {
			qbit = "Running"
		}
		if state.ProtonConnected {
			vpn = "Connected"
			if state.PortForwardingError != "" {
				port = "Unavailable"
			} else if state.ForwardedPort != 0 {
				port = fmt.Sprint(state.ForwardedPort)
			}
		}
		if state.QBittorrentDetectionError != "" {
			qbit = "Unknown"
		}
		if state.ProtonDetectionError != "" {
			vpn, port = "Unknown", "Unknown"
		}
	}
	return []string{"qBittorrent: " + qbit, "Proton VPN: " + vpn, "Guard: " + service, "Forwarded port: " + port}
}

func needsAttention(state guard.RuntimeState, stateErr error, now time.Time) bool {
	return stateErr != nil || now.Sub(state.UpdatedAt) >= 10*time.Second || state.Error != "" ||
		state.ProtonDetectionError != "" || state.QBittorrentDetectionError != "" || state.SafetyReadError != "" ||
		(state.QBittorrentRunning && !state.Healthy)
}

// FullStatus is the diagnostic snapshot used by Details and Copy full status.
func FullStatus(state guard.RuntimeState, stateErr error, now time.Time) string {
	text := strings.Join(statusLines(state, stateErr, now), "\n")
	if stateErr != nil {
		return text + "\n\nStatus could not be read: " + stateErr.Error()
	}
	text += "\n\nLast observation: " + state.UpdatedAt.Format(time.RFC3339)
	if now.Sub(state.UpdatedAt) >= 10*time.Second {
		text += " (outdated; details below are not current)"
	}
	text += fmt.Sprintf("\nGuard PID: %d\nChecks passed: %t", state.PID, state.Healthy)
	text += fmt.Sprintf("\nProton interface: %s\nProton address: %s", detailValue(state.ProtonInterface), detailValue(state.ProtonAddress))
	if state.SafetyReadError != "" {
		text += "\nqBittorrent safety settings: Unknown"
	} else {
		text += fmt.Sprintf("\nqBittorrent interface: %s\nqBittorrent address: %s\nqBittorrent port: %d\nLocal peer discovery disabled: %t\nRouter port forwarding disabled: %t",
			detailValue(state.QBittorrentInterface), detailValue(state.QBittorrentAddress), state.QBittorrentPort, state.LocalPeerDiscoveryDisabled, state.RouterPortForwardingDisabled)
	}
	for _, diagnostic := range []struct{ label, value string }{
		{"Guard error", state.Error}, {"Port forwarding error", state.PortForwardingError},
		{"Proton inspection error", state.ProtonDetectionError}, {"qBittorrent inspection error", state.QBittorrentDetectionError},
		{"Safety settings error", state.SafetyReadError}, {"Guard action this check", state.LastAction}, {"Diagnostic", state.Message},
	} {
		text += "\n" + diagnostic.label + ": " + detailValue(diagnostic.value)
	}
	return text
}

func detailValue(value string) string {
	if value == "" {
		return "—"
	}
	return value
}
