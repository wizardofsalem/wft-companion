//go:build !windows

package install

func Register(Entry) error { return nil }

func Unregister() error { return nil }

func Registered() bool { return false }
