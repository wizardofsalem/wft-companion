package hidden

import "os/exec"

func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	apply(cmd, true)
	return cmd
}

func Program(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	apply(cmd, false)
	return cmd
}

func Detached(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	detach(cmd)
	return cmd
}
