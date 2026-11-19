//go:build !windows

package hidden

import "os/exec"

func apply(*exec.Cmd, bool) {}

func detach(*exec.Cmd) {}
