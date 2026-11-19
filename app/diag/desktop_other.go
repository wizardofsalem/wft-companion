//go:build !windows

package diag

import "errors"

func knownDesktop() (string, error) { return "", errors.New("not Windows") }

func regQuery(key, value string) (string, error) { return "", errors.New("not Windows") }
