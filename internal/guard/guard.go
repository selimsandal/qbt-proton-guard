package guard

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/selimsandal/qbt-proton-guard/internal/notify"
	"github.com/selimsandal/qbt-proton-guard/internal/portforward"
	"github.com/selimsandal/qbt-proton-guard/internal/proton"
	"github.com/selimsandal/qbt-proton-guard/internal/qbittorrent"
)

type Guard struct {
	ports      portforward.Manager
	lastAction string
}

const notificationFailureDelay = 3 * time.Second

type notificationState struct {
	failureSince    time.Time
	failureNotified bool
	pendingAction   string
}

func Run(ctx context.Context, interval time.Duration) error {
	guard := &Guard{}
	lastResult := ""
	var notifications notificationState
	for {
		result, err := guard.Enforce(ctx)
		enforceErr := err
		if err != nil {
			result = "error: " + err.Error()
		}
		port, portErr := guard.ports.Status()
		state := RuntimeState{
			UpdatedAt:     time.Now(),
			PID:           os.Getpid(),
			Message:       result,
			Healthy:       enforceErr == nil && portErr == nil,
			ForwardedPort: port,
		}
		state.LastAction = guard.lastAction
		if enforceErr != nil {
			state.Error = enforceErr.Error()
		}
		if portErr != nil {
			state.PortForwardingError = portErr.Error()
		}
		guard.populateRuntimeState(ctx, &state)
		if err := writeRuntimeState(state); err != nil {
			log.Printf("write runtime status: %v", err)
		}
		if result != lastResult {
			log.Print(result)
			lastResult = result
		}
		notification, send := notifications.update(time.Now(), state)
		if send {
			notifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := notify.Send(notifyCtx, notification); err != nil {
				log.Printf("send notification: %v", err)
			}
			cancel()
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (n *notificationState) update(now time.Time, state RuntimeState) (string, bool) {
	if state.LastAction != "" {
		n.pendingAction = state.LastAction
	}
	failing := state.Error != "" || state.PortForwardingError != "" || state.ProtonDetectionError != "" || state.QBittorrentDetectionError != "" || state.SafetyReadError != ""
	if failing {
		if n.failureSince.IsZero() {
			n.failureSince = now
		}
	} else {
		n.failureSince = time.Time{}
		if n.failureNotified {
			n.failureNotified = false
			return "Protection has recovered and is operating normally.", true
		}
	}
	if n.pendingAction != "" {
		message := n.pendingAction
		n.pendingAction = ""
		return message, true
	}
	if failing && !n.failureNotified && now.Sub(n.failureSince) >= notificationFailureDelay {
		n.failureNotified = true
		return "Protection is impaired. Open qbt-proton-guard for details.", true
	}
	return "", false
}

func (guard *Guard) populateRuntimeState(ctx context.Context, state *RuntimeState) {
	if tunnel, err := proton.Detect(ctx); err == nil {
		state.ProtonConnected = true
		state.ProtonInterface = tunnel.Interface
		state.ProtonAddress = tunnel.Address
	} else if !errors.Is(err, proton.ErrDisconnected) {
		state.ProtonDetectionError = err.Error()
		state.Healthy = false
	}
	if path, err := qbittorrent.FindConfig(); err == nil {
		if safety, err := qbittorrent.ReadSafety(path); err == nil {
			state.QBittorrentInterface = safety.Interface
			state.QBittorrentAddress = safety.Address
			state.QBittorrentPort = safety.Port
			state.LocalPeerDiscoveryDisabled = safety.LocalPeerDiscoveryDisabled
			state.RouterPortForwardingDisabled = safety.RouterPortForwardingDisabled
		} else {
			state.SafetyReadError = err.Error()
			state.Healthy = false
		}
	} else {
		state.SafetyReadError = err.Error()
		state.Healthy = false
	}
	if running, err := qbittorrent.Running(ctx); err == nil {
		state.QBittorrentRunning = running
	} else {
		state.QBittorrentDetectionError = err.Error()
		state.Healthy = false
	}
}

func Enforce(ctx context.Context) (string, error) {
	return (&Guard{}).Enforce(ctx)
}

func (guard *Guard) Enforce(ctx context.Context) (string, error) {
	guard.lastAction = ""
	target := qbittorrent.Binding{Interface: qbittorrent.DisabledInterface, Name: qbittorrent.DisabledInterface}
	tunnel, tunnelErr := proton.Detect(ctx)
	if tunnelErr == nil {
		target = qbittorrent.Binding{Interface: tunnel.Interface, Name: tunnel.Name, Address: tunnel.Address}
	}

	path, err := qbittorrent.FindConfig()
	if err != nil {
		return "", err
	}
	current, err := qbittorrent.ReadSafety(path)
	if err != nil {
		return "", fmt.Errorf("read qBittorrent safety settings: %w", err)
	}
	desired := qbittorrent.Safety{
		Binding:                      target,
		Port:                         current.Port,
		LocalPeerDiscoveryDisabled:   true,
		RouterPortForwardingDisabled: true,
	}
	var portErr error
	if tunnelErr == nil {
		forwardCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		port, err := guard.ports.Forward(forwardCtx, tunnel)
		cancel()
		portErr = err
		if port != 0 {
			desired.Port = port
		}
	} else {
		guard.ports.Reset()
	}
	running, err := qbittorrent.Running(ctx)
	if err != nil {
		return "", fmt.Errorf("inspect qBittorrent process: %w", err)
	}
	if qbittorrent.SafetyMatches(current, desired) {
		return describe(target.Interface, desired.Port, running, tunnelErr, portErr), nil
	}

	wasRunning := running
	if running {
		if err := qbittorrent.Stop(ctx); err != nil {
			return "", fmt.Errorf("stop unsafely bound qBittorrent: %w", err)
		}
		guard.lastAction = "The guard stopped qBittorrent before updating its network settings."
	}
	if err := qbittorrent.WriteSafety(path, desired); err != nil {
		return "", err
	}
	verified, err := qbittorrent.ReadSafety(path)
	if err != nil || !qbittorrent.SafetyMatches(verified, desired) {
		return "", fmt.Errorf("qBittorrent safety settings verification failed")
	}

	if wasRunning && tunnelErr == nil {
		if err := qbittorrent.StartAndWait(ctx); err != nil {
			return "", fmt.Errorf("binding corrected to %s but qBittorrent restart failed: %w", target.Interface, err)
		}
		guard.lastAction = "The guard restarted qBittorrent to use the Proton VPN connection."
		return describe(target.Interface, desired.Port, true, nil, portErr), nil
	}
	if tunnelErr != nil {
		if wasRunning {
			guard.lastAction = "qBittorrent was stopped because a safe Proton VPN connection could not be confirmed."
		}
		return fmt.Sprintf("fail-closed: Proton unavailable; qBittorrent stopped and bound to %s", target.Interface), nil
	}
	return describe(target.Interface, desired.Port, false, nil, portErr), nil
}

func describe(target string, port uint16, running bool, tunnelErr, portErr error) string {
	if tunnelErr != nil {
		if running {
			return fmt.Sprintf("fail-closed on %s; qBittorrent has no usable BitTorrent interface", target)
		}
		return fmt.Sprintf("fail-closed on %s; qBittorrent is stopped", target)
	}
	if running {
		if portErr != nil {
			return fmt.Sprintf("protected: qBittorrent is bound to Proton interface %s; port forwarding unavailable: %v", target, portErr)
		}
		return fmt.Sprintf("protected: qBittorrent is bound to Proton interface %s and forwarded port %d", target, port)
	}
	if portErr != nil {
		return fmt.Sprintf("ready: qBittorrent will bind to Proton interface %s; port forwarding unavailable: %v", target, portErr)
	}
	return fmt.Sprintf("ready: qBittorrent will bind to Proton interface %s and forwarded port %d", target, port)
}
