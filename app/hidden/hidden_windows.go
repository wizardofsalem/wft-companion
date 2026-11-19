//go:build windows

package hidden

import (
	"os/exec"
	"syscall"
)

const (
	createNoWindow        = 0x08000000
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

func apply(cmd *exec.Cmd, hide bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: hide, CreationFlags: createNoWindow}
}

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: detachedProcess | createNewProcessGroup}
}
