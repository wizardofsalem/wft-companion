//go:build !windows

package install

import (
	"errors"
	"log/slog"
	"os"
)

func ScheduleRemoval(dirs []string) error {
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if !safeToRemove(d) {
			return errors.New("refusing to remove " + d)
		}
		slog.Info("uninstall: removing", "dir", d)
		if err := os.RemoveAll(d); err != nil {
			return err
		}
	}
	return nil
}
