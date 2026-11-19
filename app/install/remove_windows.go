//go:build windows

package install

import (
	"errors"
	"log/slog"
	"os"

	"github.com/wizardofsalem/wft-companion/app/hidden"
)

func ScheduleRemoval(dirs []string) error {
	for _, d := range dirs {
		if !safeToRemove(d) {
			return errors.New("refusing to remove " + d)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	finisher, err := copyForFinish(exe)
	if err != nil {
		return err
	}
	cmd := hidden.Detached(finisher, FinishArgs(dirs)...)
	slog.Info("uninstall: removal scheduled", "dirs", len(dirs))
	return cmd.Start()
}
