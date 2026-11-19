//go:build windows

package secret

import (
	"syscall"
	"unsafe"

	"github.com/wizardofsalem/wft-companion/app/syspath"
)

var (
	crypt32  = syscall.NewLazyDLL(syspath.System32("crypt32.dll"))
	kernel32 = syscall.NewLazyDLL(syspath.System32("kernel32.dll"))

	pCryptProtectData   = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	pLocalFree          = kernel32.NewProc("LocalFree")
)

const cryptprotectUIForbidden = 0x1

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func (b *dataBlob) bytes() []byte {
	out := make([]byte, b.cbData)
	copy(out, unsafe.Slice(b.pbData, b.cbData))
	return out
}

var entropy = []byte("WFT Companion upload code v1")

func protect(plain []byte) ([]byte, error) {
	var out dataBlob
	r, _, err := pCryptProtectData.Call(uintptr(unsafe.Pointer(newBlob(plain))), 0,
		uintptr(unsafe.Pointer(newBlob(entropy))), 0, 0, cryptprotectUIForbidden, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.bytes(), nil
}

func unprotect(enc []byte) ([]byte, error) {
	if len(enc) == 0 {
		return nil, errUnreadable
	}
	var out dataBlob
	r, _, _ := pCryptUnprotectData.Call(uintptr(unsafe.Pointer(newBlob(enc))), 0,
		uintptr(unsafe.Pointer(newBlob(entropy))), 0, 0, cryptprotectUIForbidden, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, errUnreadable
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.bytes(), nil
}
