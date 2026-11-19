//go:build windows

package service

import "strings"

func isDiskFull(err string) bool {
	return strings.Contains(strings.ToLower(err), "not enough space on the disk") ||
		strings.Contains(strings.ToLower(err), "disk is full")
}
