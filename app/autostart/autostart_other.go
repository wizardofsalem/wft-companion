//go:build !windows && !darwin

package autostart

func Enable() error { return nil }

func Disable() error { return nil }

func Enabled() bool { return false }

func Supported() bool { return false }
