//go:build !windows

package updater

import "errors"

func verifyAuthenticode(path, want string) error {
	return errors.New("code signatures can't be checked on this system")
}
