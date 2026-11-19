//go:build windows

package install

import (
	"syscall"
	"unsafe"

	"github.com/wizardofsalem/wft-companion/app/syspath"
)

var (
	advapi32 = syscall.NewLazyDLL(syspath.System32("advapi32.dll"))

	pRegCreateKeyExW = advapi32.NewProc("RegCreateKeyExW")
	pRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	pRegDeleteTreeW  = advapi32.NewProc("RegDeleteTreeW")
	pRegCloseKey     = advapi32.NewProc("RegCloseKey")
	pRegOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
)

const (
	hkeyCurrentUser = 0x80000001
	keyAllAccess    = 0xF003F
	keyRead         = 0x20019
	regSZ           = 1
	regDWORD        = 4
	errFileNotFound = 2
)

func regErr(r uintptr) error {
	if r == 0 {
		return nil
	}
	return syscall.Errno(r)
}

func Register(e Entry) error {
	sub, err := syscall.UTF16PtrFromString(UninstallKey)
	if err != nil {
		return err
	}
	var key syscall.Handle
	r, _, _ := pRegCreateKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(sub)), 0, 0, 0, keyAllAccess, 0,
		uintptr(unsafe.Pointer(&key)), 0)
	if err := regErr(r); err != nil {
		return err
	}
	defer pRegCloseKey.Call(uintptr(key))
	str := func(name, value string) error {
		n, _ := syscall.UTF16PtrFromString(name)
		v, _ := syscall.UTF16FromString(value)
		r, _, _ := pRegSetValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(n)), 0, regSZ,
			uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
		return regErr(r)
	}
	dword := func(name string, value uint32) error {
		n, _ := syscall.UTF16PtrFromString(name)
		r, _, _ := pRegSetValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(n)), 0, regDWORD,
			uintptr(unsafe.Pointer(&value)), 4)
		return regErr(r)
	}
	for _, kv := range [][2]string{
		{"DisplayName", e.DisplayName}, {"DisplayVersion", e.DisplayVersion}, {"Publisher", e.Publisher},
		{"InstallLocation", e.InstallLocation}, {"DisplayIcon", e.DisplayIcon}, {"UninstallString", e.UninstallString},
	} {
		if err := str(kv[0], kv[1]); err != nil {
			return err
		}
	}
	for _, kv := range []struct {
		name  string
		value uint32
	}{{"NoModify", 1}, {"NoRepair", 1}, {"EstimatedSize", e.EstimatedSizeKB}} {
		if err := dword(kv.name, kv.value); err != nil {
			return err
		}
	}
	return nil
}

func Unregister() error {
	sub, err := syscall.UTF16PtrFromString(UninstallKey)
	if err != nil {
		return err
	}
	r, _, _ := pRegDeleteTreeW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(sub)))
	if r == errFileNotFound {
		return nil
	}
	return regErr(r)
}

func Registered() bool {
	sub, err := syscall.UTF16PtrFromString(UninstallKey)
	if err != nil {
		return false
	}
	var key syscall.Handle
	r, _, _ := pRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(sub)), 0, keyRead, uintptr(unsafe.Pointer(&key)))
	if r != 0 {
		return false
	}
	pRegCloseKey.Call(uintptr(key))
	return true
}
