package statusapp

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/selimsandal/qbt-proton-guard/internal/guard"
)

func TestStatusPresentation(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		name      string
		state     guard.RuntimeState
		err       error
		want      []string
		attention bool
	}{
		{
			name:  "both closed is idle",
			state: guard.RuntimeState{UpdatedAt: now, Healthy: true, Message: "fail-closed on disabled interface"},
			want:  []string{"qBittorrent: Not running", "Proton VPN: Disconnected", "Guard: Running", "Forwarded port: Not active"},
		},
		{
			name:  "qBittorrent closed with VPN connected",
			state: guard.RuntimeState{UpdatedAt: now, ProtonConnected: true, ForwardedPort: 12345, Healthy: true},
			want:  []string{"qBittorrent: Not running", "Proton VPN: Connected", "Guard: Running", "Forwarded port: 12345"},
		},
		{
			name:  "both running",
			state: guard.RuntimeState{UpdatedAt: now, QBittorrentRunning: true, ProtonConnected: true, ForwardedPort: 12345, Healthy: true},
			want:  []string{"qBittorrent: Running", "Proton VPN: Connected", "Guard: Running", "Forwarded port: 12345"},
		},
		{
			name:  "VPN disconnected but qBittorrent safely blocked",
			state: guard.RuntimeState{UpdatedAt: now, QBittorrentRunning: true, Healthy: true},
			want:  []string{"qBittorrent: Running", "Proton VPN: Disconnected", "Guard: Running", "Forwarded port: Not active"},
		},
		{
			name:  "port failure while idle",
			state: guard.RuntimeState{UpdatedAt: now, ProtonConnected: true, ForwardedPort: 12345, PortForwardingError: "timeout"},
			want:  []string{"qBittorrent: Not running", "Proton VPN: Connected", "Guard: Running", "Forwarded port: Unavailable"},
		},
		{
			name:      "port failure while running",
			state:     guard.RuntimeState{UpdatedAt: now, QBittorrentRunning: true, ProtonConnected: true, PortForwardingError: "timeout"},
			want:      []string{"qBittorrent: Running", "Proton VPN: Connected", "Guard: Running", "Forwarded port: Unavailable"},
			attention: true,
		},
		{
			name:      "enforcement error while idle",
			state:     guard.RuntimeState{UpdatedAt: now, Error: "cannot write config"},
			want:      []string{"qBittorrent: Not running", "Proton VPN: Disconnected", "Guard: Error — check log", "Forwarded port: Not active"},
			attention: true,
		},
		{
			name:      "stale observations are not current",
			state:     guard.RuntimeState{UpdatedAt: now.Add(-10 * time.Second), QBittorrentRunning: true, ProtonConnected: true, Healthy: true, ForwardedPort: 12345},
			want:      []string{"qBittorrent: Unknown", "Proton VPN: Unknown", "Guard: Status unavailable", "Forwarded port: Unknown"},
			attention: true,
		},
		{
			name:      "read failure clears previous observations",
			state:     guard.RuntimeState{UpdatedAt: now, QBittorrentRunning: true, ProtonConnected: true, Healthy: true, ForwardedPort: 12345},
			err:       errors.New("read failed"),
			want:      []string{"qBittorrent: Unknown", "Proton VPN: Unknown", "Guard: Status unavailable", "Forwarded port: Unknown"},
			attention: true,
		},
		{
			name:      "failed inspections are unknown, not stopped",
			state:     guard.RuntimeState{UpdatedAt: now, ProtonDetectionError: "scutil failed", QBittorrentDetectionError: "ps failed"},
			want:      []string{"qBittorrent: Unknown", "Proton VPN: Unknown", "Guard: Running", "Forwarded port: Unknown"},
			attention: true,
		},
		{
			name:      "unreadable safety settings need attention",
			state:     guard.RuntimeState{UpdatedAt: now, SafetyReadError: "permission denied"},
			want:      []string{"qBittorrent: Not running", "Proton VPN: Disconnected", "Guard: Error — check log", "Forwarded port: Not active"},
			attention: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := statusLines(test.state, test.err, now); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("statusLines = %q, want %q", got, test.want)
			}
			if got := needsAttention(test.state, test.err, now); got != test.attention {
				t.Fatalf("needsAttention = %v, want %v", got, test.attention)
			}
		})
	}
}

func TestFullStatusIncludesDetailsAndMarksStaleObservations(t *testing.T) {
	now := time.Now()
	state := guard.RuntimeState{UpdatedAt: now.Add(-time.Minute), PID: 42, ProtonInterface: "utun5", ProtonAddress: "10.2.0.2", QBittorrentPort: 12345,
		PortForwardingError: "forwarding unavailable", ProtonDetectionError: "inspection failed", SafetyReadError: "unreadable settings", Message: "detailed diagnostic"}
	report := FullStatus(state, nil, now)
	for _, want := range []string{"qBittorrent: Unknown", "Last observation:", "outdated", "Guard PID: 42", "utun5", "10.2.0.2", "forwarding unavailable", "inspection failed", "unreadable settings", "detailed diagnostic", "qBittorrent safety settings: Unknown"} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q: %s", want, report)
		}
	}
	if strings.Contains(report, "Local peer discovery disabled: false") {
		t.Fatal("unreadable config presented as confirmed")
	}
	report = FullStatus(state, errors.New("invalid JSON"), now)
	if !strings.Contains(report, "invalid JSON") || strings.Contains(report, "utun5") {
		t.Fatalf("invalid state report: %s", report)
	}
}
