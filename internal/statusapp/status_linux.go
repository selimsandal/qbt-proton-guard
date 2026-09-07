//go:build linux

package statusapp

import (
	"context"
	"image"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"deedles.dev/tray"
	"github.com/godbus/dbus/v5"
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
	colored := preferenceEnabled("colored-icon")
	notifications := preferenceEnabled("notifications")
	icon, err := statusIcon(colored)
	if err != nil {
		return err
	}
	var item *tray.Item
	for ctx.Err() == nil {
		item, err = tray.New(
			tray.ItemID("qbt-proton-guard"),
			tray.ItemTitle("qbt-proton-guard"),
			tray.ItemCategory(tray.ApplicationStatus),
			tray.ItemIconName(""),
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
	if item == nil {
		return nil
	}
	defer item.Close()

	qbitRow, _ := item.Menu().AddChild(tray.MenuItemLabel("qBittorrent: Checking"))
	vpnRow, _ := item.Menu().AddChild(tray.MenuItemLabel("Proton VPN: Checking"))
	serviceRow, _ := item.Menu().AddChild(tray.MenuItemLabel("Guard: Checking"))
	portRow, _ := item.Menu().AddChild(tray.MenuItemLabel("Forwarded port: Checking"))
	_, _ = item.Menu().AddChild(tray.MenuItemType(tray.Separator))
	for _, action := range []struct {
		label string
		run   func() error
	}{{"Details…", openLinuxDetails}, {"Copy full status", copyLinuxDetails}} {
		_, _ = item.Menu().AddChild(tray.MenuItemLabel(action.label), tray.MenuItemHandler(tray.ClickedHandler(func(any, uint32) error {
			if err := action.run(); err != nil {
				log.Printf("%s: %v", action.label, err)
				return err
			}
			return nil
		})))
	}
	_, _ = item.Menu().AddChild(tray.MenuItemType(tray.Separator))
	// Menu callbacks run separately from the refresh loop; serialize the toggle here.
	toggleIcon := make(chan struct{}, 1)
	iconSetting, _ := item.Menu().AddChild(
		tray.MenuItemLabel("Colored icon"),
		tray.MenuItemToggleType(tray.Checkmark),
		tray.MenuItemToggleState(map[bool]tray.MenuToggleState{true: tray.On, false: tray.Off}[colored]),
		tray.MenuItemHandler(tray.ClickedHandler(func(any, uint32) error {
			select {
			case toggleIcon <- struct{}{}:
			default:
			}
			return nil
		})),
	)
	toggleNotifications := make(chan struct{}, 1)
	notificationSetting, _ := item.Menu().AddChild(
		tray.MenuItemLabel("Notifications"),
		tray.MenuItemToggleType(tray.Checkmark),
		tray.MenuItemToggleState(map[bool]tray.MenuToggleState{true: tray.On, false: tray.Off}[notifications]),
		tray.MenuItemHandler(tray.ClickedHandler(func(any, uint32) error {
			select {
			case toggleNotifications <- struct{}{}:
			default:
			}
			return nil
		})),
	)
	login, loginErr := guard.StatusAtLogin()
	toggleLogin := make(chan struct{}, 1)
	loginSetting, _ := item.Menu().AddChild(
		tray.MenuItemLabel("Show icon at login"),
		tray.MenuItemEnabled(loginErr == nil),
		tray.MenuItemToggleType(tray.Checkmark),
		tray.MenuItemToggleState(map[bool]tray.MenuToggleState{true: tray.On, false: tray.Off}[login]),
		tray.MenuItemHandler(tray.ClickedHandler(func(any, uint32) error {
			select {
			case toggleLogin <- struct{}{}:
			default:
			}
			return nil
		})),
	)
	quit := make(chan struct{}, 1)
	_, _ = item.Menu().AddChild(
		tray.MenuItemLabel("Quit Status Icon"),
		tray.MenuItemHandler(tray.ClickedHandler(func(any, uint32) error {
			select {
			case quit <- struct{}{}:
			default:
			}
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
		now := time.Now()
		lines := statusLines(state, err, now)
		_ = qbitRow.SetProps(tray.MenuItemLabel(lines[0]))
		_ = vpnRow.SetProps(tray.MenuItemLabel(lines[1]))
		_ = serviceRow.SetProps(tray.MenuItemLabel(lines[2]))
		_ = portRow.SetProps(tray.MenuItemLabel(lines[3]))
		trayStatus := tray.Active
		if needsAttention(state, err, now) {
			trayStatus = tray.NeedsAttention
		}
		_ = item.SetProps(tray.ItemStatus(trayStatus), tray.ItemToolTip("qbt-proton-guard", []image.Image{icon}, "qbt-proton-guard", strings.Join(lines, "\n")))
		deliverLinuxNotifications(ctx, connection, notifications)
		select {
		case <-ctx.Done():
			return nil
		case <-quit:
			return nil
		case <-toggleLogin:
			if err := guard.SetStatusAtLogin(!login); err != nil {
				log.Printf("change icon login setting: %v", err)
				continue
			}
			login = !login
			_ = loginSetting.SetProps(tray.MenuItemToggleState(map[bool]tray.MenuToggleState{true: tray.On, false: tray.Off}[login]))
		case <-toggleIcon:
			next, err := statusIcon(!colored)
			if err == nil {
				err = savePreference("colored-icon", !colored)
			}
			if err != nil {
				log.Printf("change colored icon setting: %v", err)
				continue
			}
			colored, icon = !colored, next
			_ = item.SetProps(tray.ItemIconPixmap(icon))
			_ = iconSetting.SetProps(tray.MenuItemToggleState(map[bool]tray.MenuToggleState{true: tray.On, false: tray.Off}[colored]))
		case <-toggleNotifications:
			if !notifications {
				deliverLinuxNotifications(ctx, connection, false)
			}
			if err := savePreference("notifications", !notifications); err != nil {
				log.Printf("save notifications setting: %v", err)
				continue
			}
			notifications = !notifications
			_ = notificationSetting.SetProps(tray.MenuItemToggleState(map[bool]tray.MenuToggleState{true: tray.On, false: tray.Off}[notifications]))
		case <-ticker.C:
		}
	}
}

func openLinuxDetails() error {
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(cache, "qbt-proton-guard")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "status-details.txt")
	state, stateErr := guard.ReadRuntimeState()
	if err := os.WriteFile(path, []byte(FullStatus(state, stateErr, time.Now())), 0o600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "xdg-open", path).Run()
}

func copyLinuxDetails() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "xclip", "-selection", "clipboard")
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		command = exec.CommandContext(ctx, "wl-copy")
	}
	state, err := guard.ReadRuntimeState()
	command.Stdin = strings.NewReader(FullStatus(state, err, time.Now()))
	return command.Run()
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

func deliverLinuxNotifications(ctx context.Context, connection *dbus.Conn, enabled bool) {
	pending, _ := notify.ListPending()
	for _, notification := range pending {
		if !enabled {
			_ = notify.Remove(notification)
			continue
		}
		if connection == nil {
			return
		}
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
