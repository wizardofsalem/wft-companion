//go:build windows

package install

import (
	"syscall"
	"unsafe"

	"github.com/wizardofsalem/wft-companion/app/syspath"
)

var pMessageBoxW = syscall.NewLazyDLL(syspath.System32("user32.dll")).NewProc("MessageBoxW")

func AskUninstall() (removeAddon, proceed bool) {
	const (
		mbYesNoCancel  = 0x3
		mbIconQuestion = 0x20
		idYes, idNo    = 6, 7
	)
	text, _ := syscall.UTF16PtrFromString("Remove WFT Companion from this computer?\n\n" +
		"Also remove the WoW Forever Tracker addon from your WoW folder?\n\n" +
		"Yes: remove the app and the addon\nNo: remove the app, keep the addon\nCancel: remove nothing")
	title, _ := syscall.UTF16PtrFromString("Uninstall WFT Companion")
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbYesNoCancel|mbIconQuestion)
	switch r {
	case idYes:
		return true, true
	case idNo:
		return false, true
	}
	return false, false
}
