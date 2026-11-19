//go:build windows

package updater

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/wizardofsalem/wft-companion/app/syspath"
)

var (
	wintrust = syscall.NewLazyDLL(syspath.System32("wintrust.dll"))
	crypt32  = syscall.NewLazyDLL(syspath.System32("crypt32.dll"))

	pWinVerifyTrust                 = wintrust.NewProc("WinVerifyTrust")
	pWTHelperProvDataFromStateData  = wintrust.NewProc("WTHelperProvDataFromStateData")
	pWTHelperGetProvSignerFromChain = wintrust.NewProc("WTHelperGetProvSignerFromChain")
	pCertGetNameStringW             = crypt32.NewProc("CertGetNameStringW")
)

var rootName = ""

var actionVerifyV2 = syscall.GUID{Data1: 0x00AAC56B, Data2: 0xCD44, Data3: 0x11D0,
	Data4: [8]byte{0x8C, 0xC2, 0x00, 0xC0, 0x4F, 0xC2, 0x95, 0xEE}}

type wintrustFileInfo struct {
	cbStruct       uint32
	pcwszFilePath  *uint16
	hFile          uintptr
	pgKnownSubject uintptr
}

type wintrustData struct {
	cbStruct            uint32
	pPolicyCallbackData uintptr
	pSIPClientData      uintptr
	dwUIChoice          uint32
	fdwRevocationChecks uint32
	dwUnionChoice       uint32
	pFile               *wintrustFileInfo
	dwStateAction       uint32
	hWVTStateData       uintptr
	pwszURLReference    *uint16
	dwProvFlags         uint32
	dwUIContext         uint32
	pSignatureSettings  uintptr
}

const (
	wtdUINone          = 2
	wtdRevokeNone      = 0
	wtdChoiceFile      = 1
	wtdStateVerify     = 1
	wtdStateClose      = 2
	certNameSimpleDisp = 4
)

type providerSigner struct {
	cbStruct       uint32
	verifyAsOfLow  uint32
	verifyAsOfHigh uint32
	csCertChain    uint32
	pasCertChain   unsafe.Pointer
}

type providerCert struct {
	cbStruct uint32
	pCert    uintptr
}

func verifyAuthenticode(path, want string) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	file := wintrustFileInfo{cbStruct: uint32(unsafe.Sizeof(wintrustFileInfo{})), pcwszFilePath: p}
	data := wintrustData{cbStruct: uint32(unsafe.Sizeof(wintrustData{})), dwUIChoice: wtdUINone,
		fdwRevocationChecks: wtdRevokeNone, dwUnionChoice: wtdChoiceFile, pFile: &file, dwStateAction: wtdStateVerify}
	r, _, _ := pWinVerifyTrust.Call(0, uintptr(unsafe.Pointer(&actionVerifyV2)), uintptr(unsafe.Pointer(&data)))
	defer func() {
		data.dwStateAction = wtdStateClose
		pWinVerifyTrust.Call(0, uintptr(unsafe.Pointer(&actionVerifyV2)), uintptr(unsafe.Pointer(&data)))
	}()
	if r != 0 {
		return fmt.Errorf("the download's code signature is not valid (0x%08x)", uint32(r))
	}
	prov, _, _ := pWTHelperProvDataFromStateData.Call(data.hWVTStateData)
	if prov == 0 {
		return fmt.Errorf("no signer information")
	}
	signer, _, _ := pWTHelperGetProvSignerFromChain.Call(prov, 0, 0, 0)
	if signer == 0 {
		return fmt.Errorf("no signer in the signature")
	}
	sgnr := *(**providerSigner)(unsafe.Pointer(&signer))
	n := sgnr.csCertChain
	if n == 0 || sgnr.pasCertChain == nil {
		return fmt.Errorf("no certificate chain")
	}
	stride := uintptr((*providerCert)(sgnr.pasCertChain).cbStruct)
	certName := func(i uint32) string {
		cert := (*providerCert)(unsafe.Add(sgnr.pasCertChain, uintptr(i)*stride)).pCert
		if cert == 0 {
			return ""
		}
		buf := make([]uint16, 512)
		pCertGetNameStringW.Call(cert, certNameSimpleDisp, 0, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		return syscall.UTF16ToString(buf)
	}
	if got := certName(0); got != want {
		return fmt.Errorf("signed by %q, not %q", got, want)
	}
	if rootName != "" {
		if got := certName(n - 1); got != rootName {
			return fmt.Errorf("signature chains to %q, not %q", got, rootName)
		}
	}
	return nil
}
