package updater

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/wizardofsalem/wft-companion/app/updatestate"
)

const ExitStaged = 3

const BootLimit = 3

type Pending struct {
	Version string `json:"version"`
	From    string `json:"from"`
}

func PrevPath(exe string) string {
	ext := filepath.Ext(exe)
	return strings.TrimSuffix(exe, ext) + ".prev" + ext
}

func Swap(dir, exe, running string) (string, error) {
	var st Staged
	if !updatestate.Get(dir, fStaged, &st) || st.Path == "" {
		return "", errors.New("nothing staged")
	}
	clear := func() { updatestate.Set(dir, map[string]any{fStaged: nil}) }
	if _, err := os.Stat(st.Path); err != nil {
		clear()
		return "", fmt.Errorf("staged program missing: %w", err)
	}
	prev := PrevPath(exe)
	os.Remove(prev)
	if err := os.Rename(exe, prev); err != nil {
		clear()
		return "", fmt.Errorf("keeping the current program: %w", err)
	}
	if err := os.Rename(st.Path, exe); err != nil {
		os.Rename(prev, exe)
		clear()
		return "", fmt.Errorf("putting the update in place: %w", err)
	}
	if err := updatestate.Set(dir, map[string]any{fStaged: nil, "starts": 0,
		fPending: Pending{Version: st.Version, From: running}}); err != nil {
		return st.Version, err
	}
	slog.Info("update: swapped; health gate starts", "version", st.Version, "from", running)
	return st.Version, nil
}

func Promote(dir string) error {
	var p Pending
	if !updatestate.Get(dir, fPending, &p) {
		return nil
	}
	slog.Info("update: healthy", "version", p.Version, "from", p.From)
	return updatestate.Set(dir, map[string]any{fPending: nil, fCurrent: p.Version, fPrev: p.From, fFailed: nil})
}

func InGate(dir string) bool {
	var p Pending
	return updatestate.Get(dir, fPending, &p)
}

func Rollback(dir, exe, reason string) error {
	prev := PrevPath(exe)
	if _, err := os.Stat(prev); err != nil {
		return fmt.Errorf("no previous program to go back to: %w", err)
	}
	var p Pending
	failed := ""
	if updatestate.Get(dir, fPending, &p) {
		failed = p.Version
	} else {
		updatestate.Get(dir, fCurrent, &failed)
	}
	from := p.From
	if from == "" {
		updatestate.Get(dir, fPrev, &from)
	}
	broken := exe + ".failed"
	os.Remove(broken)
	if err := os.Rename(exe, broken); err != nil {
		return err
	}
	if err := os.Rename(prev, exe); err != nil {
		os.Rename(broken, exe)
		return err
	}
	os.Remove(broken)
	var bad []string
	updatestate.Get(dir, fBad, &bad)
	if failed != "" {
		bad = append(bad, failed)
	}
	msg := fmt.Sprintf("Update to %s failed (%s); still on %s.", failed, reason, from)
	slog.Error("update: rolled back", "failed", failed, "to", from, "reason", reason)
	return updatestate.Set(dir, map[string]any{fPending: nil, fBad: bad, fFailed: msg, "starts": 0,
		fCurrent: from, fPrev: nil})
}

func BootGuard(dir, exe string) bool {
	if updatestate.Starts(dir) < BootLimit {
		return false
	}
	if _, err := os.Stat(PrevPath(exe)); err != nil {
		return false
	}
	if err := Rollback(dir, exe, fmt.Sprintf("%d starts without a healthy check", BootLimit)); err != nil {
		slog.Error("update: boot guard couldn't roll back", "err", err)
		return false
	}
	return true
}

func CleanFailed(exe string) {
	os.Remove(exe + ".failed")
}
