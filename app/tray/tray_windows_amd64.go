//go:build windows && amd64

package tray

import "unsafe"

var _ [976 - unsafe.Sizeof(notifyIconData{})]byte
var _ [unsafe.Sizeof(notifyIconData{}) - 976]byte
var _ [48 - unsafe.Sizeof(msg{})]byte
