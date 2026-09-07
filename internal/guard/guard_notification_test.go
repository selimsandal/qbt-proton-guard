package guard

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNotificationsAreQuietForRoutineStates(t *testing.T) {
	var machine notificationState
	now := time.Unix(100, 0)
	states := []RuntimeState{
		{Healthy: true, ProtonConnected: true},
		{Healthy: true, ProtonConnected: true, QBittorrentRunning: true},
		{Healthy: true, ProtonConnected: true}, // ordinary app closure
		{Healthy: true},                        // confirmed VPN disconnect while idle
	}
	for i, state := range states {
		if message, send := machine.update(now.Add(time.Duration(i)*time.Second), state); send {
			t.Fatalf("state %d unexpectedly notified: %q", i, message)
		}
	}
}

func TestNotificationsDebounceFailureAndNotifyRecovery(t *testing.T) {
	var machine notificationState
	now := time.Unix(100, 0)
	failure := RuntimeState{ProtonDetectionError: "inspection failed"}
	assertNoNotification(t, &machine, now, failure)
	assertNoNotification(t, &machine, now.Add(notificationFailureDelay-time.Millisecond), failure)
	if message, send := machine.update(now.Add(notificationFailureDelay), failure); !send || message != "Protection is impaired. Open qbt-proton-guard for details." {
		t.Fatalf("sustained failure notification = (%q, %v)", message, send)
	}
	assertNoNotification(t, &machine, now.Add(4*time.Second), failure)
	if message, send := machine.update(now.Add(5*time.Second), RuntimeState{Healthy: true}); !send || message != "Protection has recovered and is operating normally." {
		t.Fatalf("recovery notification = (%q, %v)", message, send)
	}
	assertNoNotification(t, &machine, now.Add(6*time.Second), RuntimeState{Healthy: true})
}

func TestNotificationsIgnoreShortTransient(t *testing.T) {
	var machine notificationState
	now := time.Unix(100, 0)
	assertNoNotification(t, &machine, now, RuntimeState{QBittorrentDetectionError: "temporary"})
	assertNoNotification(t, &machine, now.Add(2*time.Second), RuntimeState{Healthy: true})
}

func TestInterventionIsImmediateAndNotRepeated(t *testing.T) {
	var machine notificationState
	now := time.Unix(100, 0)
	action := "qBittorrent was stopped because a safe Proton VPN connection could not be confirmed."
	if message, send := machine.update(now, RuntimeState{LastAction: action}); !send || message != action {
		t.Fatalf("intervention notification = (%q, %v)", message, send)
	}
	assertNoNotification(t, &machine, now.Add(time.Second), RuntimeState{})
}

func TestRuntimeStateDetectionDiagnosticsJSON(t *testing.T) {
	state := RuntimeState{
		ProtonDetectionError:      "proton unknown",
		QBittorrentDetectionError: "process unknown",
		SafetyReadError:           "config unknown",
		LastAction:                "stopped unsafe client",
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"proton_detection_error", "qbittorrent_detection_error", "safety_read_error", "last_action"} {
		if !json.Valid(data) || !containsJSONField(data, field) {
			t.Fatalf("runtime JSON lacks %q: %s", field, data)
		}
	}
}

func assertNoNotification(t *testing.T, machine *notificationState, now time.Time, state RuntimeState) {
	t.Helper()
	if message, send := machine.update(now, state); send {
		t.Fatalf("unexpected notification: %q", message)
	}
}

func containsJSONField(data []byte, field string) bool {
	var fields map[string]any
	if json.Unmarshal(data, &fields) != nil {
		return false
	}
	_, ok := fields[field]
	return ok
}
