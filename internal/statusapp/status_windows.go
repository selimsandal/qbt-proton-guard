//go:build windows

package statusapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"github.com/selimsandal/qbt-proton-guard/internal/guard"
	"github.com/selimsandal/qbt-proton-guard/internal/notify"
	"golang.org/x/sys/windows"
)

const (
	wmApp          = 0x8000
	wmTray         = wmApp + 1
	wmCommand      = 0x0111
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmTimer        = 0x0113
	wmRButtonUp    = 0x0205
	wmLButtonUp    = 0x0202
	wmContextMenu  = 0x007B
	nimAdd         = 0
	nimModify      = 1
	nimDelete      = 2
	nimSetVersion  = 4
	nifMessage     = 1
	nifIcon        = 2
	nifTip         = 4
	nifInfo        = 0x10
	niifUser       = 4
	niifLargeIcon  = 0x20
	notifyVersion4 = 4
	imageIcon      = 1
	lrLoadFromFile = 0x10
	mfString       = 0
	mfGray         = 1
	mfSeparator    = 0x800
	tpmRightButton = 2
	cmdQuit        = 100
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procShellNotifyIconW = windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW")
	procLoadImageW       = user32.NewProc("LoadImageW")
	procLoadIconW        = user32.NewProc("LoadIconW")
	procSetTimer         = user32.NewProc("SetTimer")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procSetForegroundWin = user32.NewProc("SetForegroundWindow")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procCreateMutexW     = kernel32.NewProc("CreateMutexW")
)

type wndClassEx struct {
	Size, Style            uint32
	WndProc                uintptr
	ClsExtra, WndExtra     int32
	Instance, Icon, Cursor windows.Handle
	Background             windows.Handle
	MenuName, ClassName    *uint16
	IconSmall              windows.Handle
}

type point struct{ X, Y int32 }

type message struct {
	Window  windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   point
	Private uint32
}

type notifyIconData struct {
	Size             uint32
	Window           windows.Handle
	ID, Flags        uint32
	CallbackMessage  uint32
	Icon             windows.Handle
	Tip              [128]uint16
	State, StateMask uint32
	Info             [256]uint16
	TimeoutOrVersion uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	ItemGUID         windows.GUID
	BalloonIcon      windows.Handle
}

type trayApp struct {
	window   windows.Handle
	icon     windows.Handle
	state    guard.RuntimeState
	stateErr error
}

var activeApp *trayApp

func Run(ctx context.Context) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	mutexName, _ := windows.UTF16PtrFromString("Local\\qbt-proton-guard-status")
	mutex, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	if mutex == 0 {
		return callErr
	}
	defer windows.CloseHandle(windows.Handle(mutex))
	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		return nil
	}

	className, _ := windows.UTF16PtrFromString("qbt-proton-guard-status")
	wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: windows.NewCallback(windowProc), ClassName: className}
	if result, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); result == 0 {
		return fmt.Errorf("register window class: %w", err)
	}
	window, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if window == 0 {
		return fmt.Errorf("create status window: %w", err)
	}
	app := &trayApp{window: windows.Handle(window), icon: loadAppIcon()}
	activeApp = app
	defer func() { activeApp = nil }()
	app.refresh()
	app.updateTray(nimAdd, "")
	defer app.updateTray(nimDelete, "")
	app.setVersion()
	procSetTimer.Call(window, 1, 1000, 0)

	go func() {
		<-ctx.Done()
		procPostMessageW.Call(window, wmClose, 0, 0)
	}()
	var msg message
	for {
		result, _, getErr := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) == -1 {
			return fmt.Errorf("read window message: %w", getErr)
		}
		if result == 0 {
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func windowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	if activeApp != nil {
		switch msg {
		case wmTimer:
			activeApp.refresh()
			return 0
		case wmTray:
			switch uint16(lParam) {
			case wmRButtonUp, wmLButtonUp, wmContextMenu:
				activeApp.showMenu()
			}
			return 0
		case wmCommand:
			if uint16(wParam) == cmdQuit {
				procDestroyWindow.Call(hwnd)
			}
			return 0
		case wmClose:
			procDestroyWindow.Call(hwnd)
			return 0
		case wmDestroy:
			procPostQuitMessage.Call(0)
			return 0
		}
	}
	result, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return result
}

func (app *trayApp) refresh() {
	app.state, app.stateErr = guard.ReadRuntimeState()
	app.updateTray(nimModify, "")
	pending, _ := notify.ListPending()
	for _, notification := range pending {
		app.updateTray(nimModify, notification.Message)
		_ = notify.Remove(notification)
	}
}

func (app *trayApp) updateTray(action uint32, notification string) {
	data := notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Window: app.window, ID: 1}
	data.Flags = nifMessage | nifIcon | nifTip
	data.CallbackMessage = wmTray
	data.Icon = app.icon
	copyUTF16(data.Tip[:], app.tooltip())
	if notification != "" {
		data.Flags |= nifInfo
		copyUTF16(data.InfoTitle[:], "qbt-proton-guard")
		copyUTF16(data.Info[:], notification)
		data.InfoFlags = niifUser | niifLargeIcon
		data.BalloonIcon = app.icon
	}
	procShellNotifyIconW.Call(uintptr(action), uintptr(unsafe.Pointer(&data)))
}

func (app *trayApp) setVersion() {
	data := notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Window: app.window, ID: 1, TimeoutOrVersion: notifyVersion4}
	procShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&data)))
}

func (app *trayApp) tooltip() string {
	if app.stateErr != nil || time.Since(app.state.UpdatedAt) >= 10*time.Second || !app.state.Healthy {
		return "qbt-proton-guard: Needs attention"
	}
	return "qbt-proton-guard: Protected"
}

func (app *trayApp) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	for _, label := range app.menuLines() {
		appendMenu(menu, mfString, 0, label)
	}
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, cmdQuit, "Quit Status Icon")
	var cursor point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	procSetForegroundWin.Call(uintptr(app.window))
	procTrackPopupMenu.Call(menu, tpmRightButton, uintptr(cursor.X), uintptr(cursor.Y), 0, uintptr(app.window), 0)
}

func (app *trayApp) menuLines() []string {
	if app.stateErr != nil {
		return []string{"Guard status unavailable", "The protection service may not be running."}
	}
	fresh := time.Since(app.state.UpdatedAt) < 10*time.Second
	title := "Needs attention"
	if fresh && app.state.Healthy {
		title = "Protected"
	}
	service := "Heartbeat stale"
	if fresh {
		service = "Running"
	}
	vpn := "Unavailable"
	if app.state.ProtonConnected {
		vpn = strings.TrimSpace(app.state.ProtonInterface + " • " + app.state.ProtonAddress)
	}
	port := "Unavailable"
	if app.state.ForwardedPort != 0 && app.state.PortForwardingError == "" {
		port = fmt.Sprint(app.state.ForwardedPort)
	}
	qbit := "Stopped"
	if app.state.QBittorrentRunning {
		qbit = fmt.Sprintf("Running • %s:%d", app.state.QBittorrentAddress, app.state.QBittorrentPort)
	}
	return []string{title, app.state.Message, "Service: " + service, "Proton VPN: " + vpn, "Forwarded port: " + port, "qBittorrent: " + qbit}
}

func loadAppIcon() windows.Handle {
	path := filepath.Join(os.Getenv("LOCALAPPDATA"), "qbt-proton-guard", "qbt-proton-guard.ico")
	pathUTF16, _ := windows.UTF16PtrFromString(path)
	icon, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(pathUTF16)), imageIcon, 0, 0, lrLoadFromFile)
	if icon == 0 {
		icon, _, _ = procLoadIconW.Call(0, 32512)
	}
	return windows.Handle(icon)
}

func appendMenu(menu uintptr, flags, id uintptr, label string) {
	wide, _ := windows.UTF16PtrFromString(label)
	procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(wide)))
}

func copyUTF16(destination []uint16, value string) {
	encoded, _ := windows.UTF16FromString(value)
	if len(encoded) > len(destination) {
		encoded = encoded[:len(destination)]
		encoded[len(encoded)-1] = 0
	}
	copy(destination, encoded)
}
