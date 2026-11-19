//go:build windows

package diag

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/wizardofsalem/wft-companion/app/hidden"
	"github.com/wizardofsalem/wft-companion/app/syspath"
)

var (
	shell32                  = syscall.NewLazyDLL("shell32.dll")
	ole32                    = syscall.NewLazyDLL("ole32.dll")
	procSHGetKnownFolderPath = shell32.NewProc("SHGetKnownFolderPath")
	procCoTaskMemFree        = ole32.NewProc("CoTaskMemFree")
)

var folderDesktop = syscall.GUID{Data1: 0xB4BFCC3A, Data2: 0xDB2C, Data3: 0x424C,
	Data4: [8]byte{0xB0, 0x29, 0x7F, 0xE9, 0x9A, 0x87, 0xC6, 0x41}}

func knownDesktop() (string, error) {
	if err := procSHGetKnownFolderPath.Find(); err != nil {
		return "", err
	}
	var p *uint16
	r, _, _ := procSHGetKnownFolderPath.Call(uintptr(unsafe.Pointer(&folderDesktop)), 0, 0, uintptr(unsafe.Pointer(&p)))
	if r != 0 || p == nil {
		return "", fmt.Errorf("SHGetKnownFolderPath: 0x%x", r)
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; ptr = unsafe.Add(ptr, 2) {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n)), nil
}

func regQuery(key, value string) (string, error) {
	cmd := hidden.Command(syspath.System32("reg.exe"), "query", key, "/v", value)
	out, err := cmd.Output()
	return string(out), err
}
