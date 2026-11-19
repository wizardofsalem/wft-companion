//go:build !windows

package service

import "strings"

func isDiskFull(err string) bool {
	return strings.Contains(err, "no space left") || strings.Contains(err, "disk quota exceeded")
}
