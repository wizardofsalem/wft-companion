//go:build windows

package tray

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassExW       = user32.NewProc("RegisterClassExW")
	pCreateWindowExW        = user32.NewProc("CreateWindowExW")
	pDefWindowProcW         = user32.NewProc("DefWindowProcW")
	pDestroyWindow          = user32.NewProc("DestroyWindow")
	pGetMessageW            = user32.NewProc("GetMessageW")
	pTranslateMessage       = user32.NewProc("TranslateMessage")
	pDispatchMessageW       = user32.NewProc("DispatchMessageW")
	pPostMessageW           = user32.NewProc("PostMessageW")
	pPostQuitMessage        = user32.NewProc("PostQuitMessage")
	pCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	pAppendMenuW            = user32.NewProc("AppendMenuW")
	pTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	pDestroyMenu            = user32.NewProc("DestroyMenu")
	pGetCursorPos           = user32.NewProc("GetCursorPos")
	pSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	pLoadIconW              = user32.NewProc("LoadIconW")
	pCreateIconFromResource = user32.NewProc("CreateIconFromResourceEx")
	pDestroyIcon            = user32.NewProc("DestroyIcon")
	pGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	pRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	pShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	wndProcCallback         = syscall.NewCallback(wndProc)
	current                 *tray
)

const (
	wmNull        = 0x0000
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmContextMenu = 0x007B
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmTray        = 0x8000 + 1

	nimAdd     = 0
	nimModify  = 1
	nimDelete  = 2
	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10
	niifInfo   = 0x01

	smCxSmIcon = 49

	mfString       = 0x0000
	mfSeparator    = 0x0800
	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	idiApplication        = 32512
	alertCmd              = 1000
	errClassAlreadyExists = 1410
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUID            [16]byte
	BalloonIcon     uintptr
}

type point struct{ X, Y int32 }

type msg struct {
	Wnd     uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type tray struct {
	wnd            uintptr
	icon           uintptr
	alertIcon      uintptr
	menu           Menu
	taskbarCreated uintptr
	alert          atomic.Bool
	tip            atomic.Value
}

func Run(ctx context.Context, m Menu) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if current != nil {
		return fmt.Errorf("tray: already running")
	}
	t := &tray{menu: m}
	if err := t.create(); err != nil {
		return err
	}
	current = t
	defer func() { current = nil }()
	stop := context.AfterFunc(ctx, func() { pPostMessageW.Call(t.wnd, wmClose, 0, 0) })
	defer stop()
	t.tip.Store(m.Tooltip)
	if !t.add() {
		slog.Warn("tray: adding the icon failed")
	}
	if m.State != nil {
		go t.watchState(ctx)
	}
	var m2 msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m2)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m2)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m2)))
	}
	if t.icon != 0 {
		pDestroyIcon.Call(t.icon)
	}
	if t.alertIcon != 0 {
		pDestroyIcon.Call(t.alertIcon)
	}
	return nil
}

func (t *tray) watchState(ctx context.Context) {
	var w watch
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		st := t.menu.State()
		if update, notice := w.next(st); update {
			t.alert.Store(st.Alert)
			t.tip.Store(st.Tooltip)
			t.modify()
			if notice != "" {
				t.notify(notice)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (t *tray) currentIcon() uintptr {
	if t.alert.Load() && t.alertIcon != 0 {
		return t.alertIcon
	}
	return t.icon
}

func (t *tray) modify() {
	n := t.data()
	n.Flags = nifIcon | nifTip
	n.Icon = t.currentIcon()
	copyUTF16(n.Tip[:], t.tip.Load().(string))
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&n)))
}

func (t *tray) create() error {
	inst, _, _ := pGetModuleHandleW.Call(0)
	class, _ := syscall.UTF16PtrFromString("WFTCompanionTray")
	wc := wndClassEx{WndProc: wndProcCallback, Instance: inst, ClassName: class}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		if errno, ok := err.(syscall.Errno); !ok || errno != errClassAlreadyExists {
			return fmt.Errorf("tray: RegisterClassEx: %w", err)
		}
	}
	title, _ := syscall.UTF16PtrFromString("WFT Companion")
	wnd, _, err := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, inst, 0)
	if wnd == 0 {
		return fmt.Errorf("tray: CreateWindowEx: %w", err)
	}
	t.wnd = wnd
	name, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	t.taskbarCreated, _, _ = pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	size, _, _ := pGetSystemMetrics.Call(smCxSmIcon)
	if size == 0 {
		size = 16
	}
	icon := IconPNG(int(size))
	t.icon, _, _ = pCreateIconFromResource.Call(uintptr(unsafe.Pointer(&icon[0])), uintptr(len(icon)),
		1, 0x00030000, size, size, 0)
	if t.icon == 0 {
		t.icon, _, _ = pLoadIconW.Call(0, idiApplication)
	}
	if t.menu.State != nil {
		alert := AlertPNG(int(size))
		t.alertIcon, _, _ = pCreateIconFromResource.Call(uintptr(unsafe.Pointer(&alert[0])), uintptr(len(alert)),
			1, 0x00030000, size, size, 0)
	}
	return nil
}

func (t *tray) data() notifyIconData {
	var n notifyIconData
	n.Size = uint32(unsafe.Sizeof(n))
	n.Wnd = t.wnd
	n.ID = 1
	return n
}

func (t *tray) add() bool {
	n := t.data()
	n.Flags = nifMessage | nifIcon | nifTip
	n.CallbackMessage = wmTray
	n.Icon = t.currentIcon()
	tip, _ := t.tip.Load().(string)
	copyUTF16(n.Tip[:], tip)
	r, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&n)))
	return r != 0
}

func (t *tray) remove() {
	n := t.data()
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&n)))
}

func (t *tray) notify(text string) {
	n := t.data()
	n.Flags = nifInfo
	n.InfoFlags = niifInfo
	copyUTF16(n.InfoTitle[:], "WFT Companion")
	copyUTF16(n.Info[:], text)
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&n)))
}

func (t *tray) run(i int) {
	if i < 0 || i >= len(t.menu.Items) || t.menu.Items[i].Do == nil {
		return
	}
	do := t.menu.Items[i].Do
	go func() {
		if text := do(); text != "" {
			t.notify(text)
		}
	}()
}

func (t *tray) showMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	if t.alert.Load() && t.menu.AlertItem.Label != "" {
		label, _ := syscall.UTF16PtrFromString(t.menu.AlertItem.Label)
		pAppendMenuW.Call(menu, mfString, alertCmd, uintptr(unsafe.Pointer(label)))
		pAppendMenuW.Call(menu, mfSeparator, 0, 0)
	}
	for i, it := range t.menu.Items {
		if it.Label == "" {
			pAppendMenuW.Call(menu, mfSeparator, 0, 0)
			continue
		}
		label, _ := syscall.UTF16PtrFromString(it.Label)
		pAppendMenuW.Call(menu, mfString, uintptr(i+1), uintptr(unsafe.Pointer(label)))
	}
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(t.wnd)
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmReturnCmd|tpmRightButton|tpmNoNotify,
		uintptr(pt.X), uintptr(pt.Y), 0, t.wnd, 0)
	pPostMessageW.Call(t.wnd, wmNull, 0, 0)
	switch {
	case cmd == alertCmd:
		if do := t.menu.AlertItem.Do; do != nil {
			go func() {
				if text := do(); text != "" {
					t.notify(text)
				}
			}()
		}
	case cmd > 0:
		t.run(int(cmd) - 1)
	}
}

func wndProc(wnd, message, wParam, lParam uintptr) uintptr {
	t := current
	switch {
	case t != nil && message == wmTray:
		switch lParam & 0xffff {
		case wmLButtonUp:
			t.run(t.menu.Default)
		case wmRButtonUp, wmContextMenu:
			t.showMenu()
		}
		return 0
	case t != nil && t.taskbarCreated != 0 && message == t.taskbarCreated:
		t.add()
		return 0
	case message == wmClose:
		pDestroyWindow.Call(wnd)
		return 0
	case message == wmDestroy:
		if t != nil {
			t.remove()
		}
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(wnd, message, wParam, lParam)
	return r
}
