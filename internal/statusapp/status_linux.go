//go:build linux

package statusapp

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"time"

	"deedles.dev/tray"
	"github.com/godbus/dbus/v5"
	"github.com/selimsandal/qbt-proton-guard/assets"
	"github.com/selimsandal/qbt-proton-guard/internal/guard"
	"github.com/selimsandal/qbt-proton-guard/internal/notify"
	"golang.org/x/sys/unix"
)

func Run(ctx context.Context) error {
	lock, err := acquireLinuxLock()
	if err != nil {
		if err == unix.EWOULDBLOCK {
			return nil
		}
		return err
	}
	defer lock.Close()
	icon, _, err := image.Decode(bytes.NewReader(assets.AppIconPNG))
	if err != nil {
		return err
	}
	var item *tray.Item
	for ctx.Err() == nil {
		item, err = tray.New(
			tray.ItemID("qbt-proton-guard"),
			tray.ItemTitle("qbt-proton-guard"),
			tray.ItemCategory(tray.ApplicationStatus),
			tray.ItemIconName("qbt-proton-guard"),
			tray.ItemIconPixmap(icon),
			tray.ItemIsMenu(true),
			tray.ItemStatus(tray.Active),
		)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
		}
	}
	defer item.Close()

	statusRow, _ := item.Menu().AddChild(tray.MenuItemLabel("Loading guard status…"))
	messageRow, _ := item.Menu().AddChild(tray.MenuItemLabel(""))
	serviceRow, _ := item.Menu().AddChild(tray.MenuItemLabel("Service: Checking"))
	vpnRow, _ := item.Menu().AddChild(tray.MenuItemLabel("Proton VPN: Checking"))
	portRow, _ := item.Menu().AddChild(tray.MenuItemLabel("Forwarded port: Checking"))
	qbitRow, _ := item.Menu().AddChild(tray.MenuItemLabel("qBittorrent: Checking"))
	_, _ = item.Menu().AddChild(tray.MenuItemType(tray.Separator))
	_, _ = item.Menu().AddChild(
		tray.MenuItemLabel("Quit Status Icon"),
		tray.MenuItemHandler(tray.ClickedHandler(func(any, uint32) error {
			item.Close()
			return nil
		})),
	)

	connection, _ := dbus.ConnectSessionBus()
	if connection != nil {
		defer connection.Close()
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		state, err := guard.ReadRuntimeState()
		if err != nil {
			_ = statusRow.SetProps(tray.MenuItemLabel("Needs attention"))
			_ = messageRow.SetProps(tray.MenuItemLabel("Guard status unavailable"))
			_ = item.SetProps(tray.ItemStatus(tray.NeedsAttention))
		} else {
			fresh := time.Since(state.UpdatedAt) < 10*time.Second
			healthy := state.Healthy && fresh
			title := "Needs attention"
			if healthy {
				title = "Protected"
			}
			_ = statusRow.SetProps(tray.MenuItemLabel(title))
			_ = messageRow.SetProps(tray.MenuItemLabel(state.Message))
			_ = serviceRow.SetProps(tray.MenuItemLabel(fmt.Sprintf("Service: %s", map[bool]string{true: "Running", false: "Heartbeat stale"}[fresh])))
			_ = vpnRow.SetProps(tray.MenuItemLabel(fmt.Sprintf("Proton VPN: %s • %s", valueOr(state.ProtonInterface, "Unavailable"), state.ProtonAddress)))
			port := "Unavailable"
			if state.ForwardedPort != 0 && state.PortForwardingError == "" {
				port = fmt.Sprint(state.ForwardedPort)
			}
			_ = portRow.SetProps(tray.MenuItemLabel("Forwarded port: " + port))
			qbit := "Stopped"
			if state.QBittorrentRunning {
				qbit = fmt.Sprintf("Running • %s:%d", state.QBittorrentAddress, state.QBittorrentPort)
			}
			_ = qbitRow.SetProps(tray.MenuItemLabel("qBittorrent: " + qbit))
			trayStatus := tray.NeedsAttention
			if healthy {
				trayStatus = tray.Active
			}
			_ = item.SetProps(tray.ItemStatus(trayStatus), tray.ItemToolTip("qbt-proton-guard", []image.Image{icon}, "qbt-proton-guard", state.Message))
		}
		if connection != nil {
			deliverLinuxNotifications(ctx, connection)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func acquireLinuxLock() (*os.File, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(cache, "qbt-proton-guard")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(directory, "status.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func deliverLinuxNotifications(ctx context.Context, connection *dbus.Conn) {
	pending, _ := notify.ListPending()
	for _, notification := range pending {
		var id uint32
		hints := map[string]dbus.Variant{
			"desktop-entry": dbus.MakeVariant("qbt-proton-guard"),
			"category":      dbus.MakeVariant("network"),
		}
		err := connection.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").CallWithContext(
			ctx, "org.freedesktop.Notifications.Notify", 0,
			"qbt-proton-guard", uint32(0), "qbt-proton-guard", "qbt-proton-guard",
			notification.Message, []string{}, hints, int32(-1),
		).Store(&id)
		if err == nil {
			_ = notify.Remove(notification)
		}
	}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
