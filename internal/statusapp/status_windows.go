//go:build windows

package statusapp

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"github.com/selimsandal/qbt-proton-guard/internal/guard"
	"github.com/selimsandal/qbt-proton-guard/internal/notify"
	"golang.org/x/sys/windows"
)

const (
	wmApp            = 0x8000
	wmTray           = wmApp + 1
	wmCommand        = 0x0111
	wmDestroy        = 0x0002
	wmClose          = 0x0010
	wmTimer          = 0x0113
	wmRButtonUp      = 0x0205
	wmLButtonUp      = 0x0202
	wmContextMenu    = 0x007B
	nimAdd           = 0
	nimModify        = 1
	nimDelete        = 2
	nimSetVersion    = 4
	nifMessage       = 1
	nifIcon          = 2
	nifTip           = 4
	nifInfo          = 0x10
	niifUser         = 4
	niifLargeIcon    = 0x20
	notifyVersion4   = 4
	mfString         = 0
	mfGray           = 1
	mfChecked        = 8
	mfSeparator      = 0x800
	tpmRightButton   = 2
	cmdQuit          = 100
	cmdColoredIcon   = 101
	cmdNotifications = 102
	cmdLogin         = 103
	cmdDetails       = 104
	cmdCopy          = 105
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procShellNotifyIconW         = windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW")
	procCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon              = user32.NewProc("DestroyIcon")
	procSetTimer                 = user32.NewProc("SetTimer")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenuW              = user32.NewProc("AppendMenuW")
	procTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procSetForegroundWin         = user32.NewProc("SetForegroundWindow")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procCreateMutexW             = kernel32.NewProc("CreateMutexW")
	procMessageBoxW              = user32.NewProc("MessageBoxW")
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
	window        windows.Handle
	icon          windows.Handle
	colored       bool
	notifications bool
	state         guard.RuntimeState
	stateErr      error
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
	colored := preferenceEnabled("colored-icon")
	icon, err := loadAppIcon(colored)
	if err != nil {
		procDestroyWindow.Call(window)
		return err
	}
	app := &trayApp{window: windows.Handle(window), icon: icon, colored: colored, notifications: preferenceEnabled("notifications")}
	defer func() { procDestroyIcon.Call(uintptr(app.icon)) }()
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
			} else if uint16(wParam) == cmdDetails {
				activeApp.showText("qbt-proton-guard Details", FullStatus(activeApp.state, activeApp.stateErr, time.Now()))
			} else if uint16(wParam) == cmdCopy {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; [Console]::InputEncoding=[Text.Encoding]::UTF8; Set-Clipboard -Value ([Console]::In.ReadToEnd())")
				command.Stdin = strings.NewReader(FullStatus(activeApp.state, activeApp.stateErr, time.Now()))
				if output, err := command.CombinedOutput(); err != nil {
					activeApp.showText("Could not copy status", fmt.Sprintf("%v: %s", err, output))
				}
				cancel()
			} else if uint16(wParam) == cmdLogin {
				enabled, err := guard.StatusAtLogin()
				if err == nil {
					err = guard.SetStatusAtLogin(!enabled)
				}
				if err != nil {
					activeApp.showText("Could not change login setting", err.Error())
				}
			} else if uint16(wParam) == cmdColoredIcon {
				activeApp.toggleColoredIcon()
			} else if uint16(wParam) == cmdNotifications {
				if !activeApp.notifications {
					activeApp.refresh() // Discard queued muted messages before enabling.
				}
				if err := savePreference("notifications", !activeApp.notifications); err != nil {
					log.Printf("save notifications setting: %v", err)
				} else {
					activeApp.notifications = !activeApp.notifications
				}
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
		if app.notifications {
			app.updateTray(nimModify, notification.Message)
		}
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
	return strings.Join(app.menuLines()[:3], "\n")
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
	appendMenu(menu, mfString, cmdDetails, "Details…")
	appendMenu(menu, mfString, cmdCopy, "Copy full status")
	appendMenu(menu, mfSeparator, 0, "")
	flags := uintptr(mfString)
	if app.colored {
		flags |= mfChecked
	}
	appendMenu(menu, flags, cmdColoredIcon, "Colored icon")
	flags = mfString
	if app.notifications {
		flags |= mfChecked
	}
	appendMenu(menu, flags, cmdNotifications, "Notifications")
	flags = mfString
	if enabled, err := guard.StatusAtLogin(); err != nil {
		flags |= mfGray
	} else if enabled {
		flags |= mfChecked
	}
	appendMenu(menu, flags, cmdLogin, "Show icon at login")
	appendMenu(menu, mfString, cmdQuit, "Quit Status Icon")
	var cursor point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	procSetForegroundWin.Call(uintptr(app.window))
	procTrackPopupMenu.Call(menu, tpmRightButton, uintptr(cursor.X), uintptr(cursor.Y), 0, uintptr(app.window), 0)
}

func (app *trayApp) menuLines() []string {
	return statusLines(app.state, app.stateErr, time.Now())
}

func (app *trayApp) showText(title, text string) {
	titleWide, _ := windows.UTF16PtrFromString(title)
	textWide, _ := windows.UTF16PtrFromString(text)
	procMessageBoxW.Call(uintptr(app.window), uintptr(unsafe.Pointer(textWide)), uintptr(unsafe.Pointer(titleWide)), 0)
}

func (app *trayApp) toggleColoredIcon() {
	icon, err := loadAppIcon(!app.colored)
	if err != nil {
		log.Printf("change colored icon: %v", err)
		return
	}
	if err := savePreference("colored-icon", !app.colored); err != nil {
		procDestroyIcon.Call(uintptr(icon))
		log.Printf("save colored icon setting: %v", err)
		return
	}
	previous := app.icon
	app.colored, app.icon = !app.colored, icon
	app.updateTray(nimModify, "")
	procDestroyIcon.Call(uintptr(previous))
}

func loadAppIcon(colored bool) (windows.Handle, error) {
	image, err := statusIcon(colored)
	if err != nil {
		return 0, err
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image); err != nil {
		return 0, err
	}
	data := buffer.Bytes()
	icon, _, err := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1, 0x00030000, 32, 32, 0)
	runtime.KeepAlive(data)
	if icon == 0 {
		return 0, fmt.Errorf("create tray icon: %w", err)
	}
	return windows.Handle(icon), nil
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
