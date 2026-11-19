package install

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const FinishFlag = "--uninstall-finish"

const dirSep = "|"

var FinishWait = time.Minute

func FinishArgs(dirs []string) []string {
	return []string{FinishFlag, strings.Join(dirs, dirSep)}
}

func safeToRemove(dir string) bool {
	clean := filepath.Clean(dir)
	return filepath.IsAbs(clean) && strings.EqualFold(filepath.Base(clean), DirName)
}

func Finish(arg string) error {
	var dirs []string
	for _, d := range strings.Split(arg, dirSep) {
		if d == "" {
			continue
		}
		if !safeToRemove(d) {
			return fmt.Errorf("refusing to remove %q: not a %s folder", d, DirName)
		}
		dirs = append(dirs, d)
	}
	deadline := time.Now().Add(FinishWait)
	for {
		var left []string
		for _, d := range dirs {
			if err := os.RemoveAll(d); err != nil {
				left = append(left, d)
			}
		}
		if len(left) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("couldn't remove " + strings.Join(left, ", "))
		}
		dirs = left
		time.Sleep(500 * time.Millisecond)
	}
}

func copyForFinish(exe string) (string, error) {
	target := filepath.Join(os.TempDir(), fmt.Sprintf("WFTCompanion-uninstall-%s%s", randomSuffix(), filepath.Ext(exe)))
	if err := copyFile(exe, target); err != nil {
		os.Remove(target)
		return "", err
	}
	slog.Info("uninstall: finisher copied", "to", target)
	return target, nil
}
