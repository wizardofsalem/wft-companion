//go:build windows

package autostart

import (
	"os"
	"strings"

	"github.com/wizardofsalem/wft-companion/app/hidden"
	"github.com/wizardofsalem/wft-companion/app/syspath"
)

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

func reg(args ...string) (string, error) {
	cmd := hidden.Command(syspath.System32("reg.exe"), args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func Enable() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	_, err = reg("add", runKey, "/v", Name, "/t", "REG_SZ", "/d", windowsCommand(exe), "/f")
	return err
}

func Disable() error {
	if !Enabled() {
		return nil
	}
	_, err := reg("delete", runKey, "/v", Name, "/f")
	return err
}

func Enabled() bool {
	out, err := reg("query", runKey, "/v", Name)
	return err == nil && strings.Contains(out, Name)
}

func Supported() bool { return true }
