//go:build !windows

package install

func AskUninstall() (removeAddon, proceed bool) { return false, true }
