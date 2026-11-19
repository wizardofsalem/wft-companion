package install

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wizardofsalem/wft-companion/addon"
	"github.com/wizardofsalem/wft-companion/app/updater"
)

const ExeName = "WFTCompanion.exe"

const DirName = "WFT Companion"

func Dir(getenv func(string) string) (string, error) {
	base := getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("LOCALAPPDATA is not set")
	}
	return filepath.Join(base, DirName), nil
}

func IsInstalled(exe, dir string) bool {
	return strings.EqualFold(filepath.Clean(filepath.Dir(exe)), filepath.Clean(dir))
}

const versionFile = "version.txt"

func InstalledVersion(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, versionFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func ShouldInstall(running, installed string, force bool) (bool, string) {
	switch {
	case force:
		return true, "forced"
	case installed == "":
		return true, "nothing installed (or version unknown)"
	case Newer(installed, running):
		return false, "installed version " + installed + " is newer than " + running
	}
	return true, "installing " + running + " over " + installed
}

func CopySelf(exe, dir, name, version string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(dir, name)
	tmp := target + ".new"
	if err := copyFile(exe, tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if _, err := os.Stat(target); err == nil {
		os.Remove(target + ".old")
		if err := os.Rename(target, target+".old"); err != nil {
			os.Remove(tmp)
			return "", fmt.Errorf("moving the old program aside: %w", err)
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, versionFile), []byte(version+"\n"), 0o644); err != nil {
		slog.Warn("install: couldn't record the version", "err", err)
	}
	slog.Info("install: program copied", "from", exe, "to", target)
	return target, nil
}

func CleanupOld(dir string) {
	old, _ := filepath.Glob(filepath.Join(dir, "*.old"))
	for _, p := range old {
		if err := os.Remove(p); err == nil {
			slog.Info("install: removed old program", "path", p)
		}
	}
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func AddOnsDir(classicDir string) string {
	return filepath.Join(classicDir, "Interface", "AddOns")
}

func InstalledAddonVersion(addonsDir string) string {
	f, err := os.Open(filepath.Join(addonsDir, addon.Name, addon.Name+".toc"))
	if err != nil {
		return ""
	}
	defer f.Close()
	return addon.TocVersion(f)
}

func InstalledAddonSource(addonsDir string) string {
	b, err := os.ReadFile(filepath.Join(addonsDir, addon.Name, addon.Name+".toc"))
	if err != nil {
		return ""
	}
	return addon.TocSource(b)
}

func Newer(a, b string) bool {
	pa, pb := parts(a), parts(b)
	for i := 0; i < max(len(pa), len(pb)); i++ {
		x, y := at(pa, i), at(pb, i)
		if x != y {
			return x > y
		}
	}
	_, _, hasA := strings.Cut(strings.TrimSpace(a), "-")
	_, _, hasB := strings.Cut(strings.TrimSpace(b), "-")
	switch {
	case !hasA && hasB:
		return b != ""
	case hasA && hasB:
		return updater.Newer(a, b)
	}
	return false
}

func parts(v string) []int {
	v, _, _ = strings.Cut(strings.TrimPrefix(strings.TrimSpace(v), "v"), "-")
	if v == "" {
		return nil
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}

func at(p []int, i int) int {
	if i < len(p) {
		return p[i]
	}
	return 0
}

func InstallAddon(files fs.FS, addonsDir string) error {
	if err := os.MkdirAll(addonsDir, 0o755); err != nil {
		return err
	}
	suffix := randomSuffix()
	staging := filepath.Join(addonsDir, addon.Name+".new-"+suffix)
	err := fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(staging, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		os.RemoveAll(staging)
		return err
	}
	final := filepath.Join(addonsDir, addon.Name)
	if _, err := os.Stat(final); err == nil {
		old := filepath.Join(addonsDir, addon.Name+".old-"+suffix)
		if err := os.Rename(final, old); err != nil {
			os.RemoveAll(staging)
			return fmt.Errorf("moving the old addon aside: %w", err)
		}
		defer os.RemoveAll(old)
	}
	if err := os.Rename(staging, final); err != nil {
		os.RemoveAll(staging)
		return err
	}
	slog.Info("install: addon installed", "dir", final)
	return nil
}

const LegacyName = "WoWForeverTracker"

func RemoveLegacyAddon(addonsDir string) (bool, error) {
	p := filepath.Join(addonsDir, LegacyName)
	if !legacyOurs(p) {
		return false, nil
	}
	slog.Info("install: removing the pre-1.0 addon folder", "dir", p)
	return true, os.RemoveAll(p)
}

func LegacyAddonLeft(addonsDir string) bool {
	p := filepath.Join(addonsDir, LegacyName)
	if _, err := os.Stat(filepath.Join(p, LegacyName+".toc")); err != nil {
		return false
	}
	return !legacyOurs(p)
}

const (
	legacyConsentHeader = "-- Written by WFT Companion"
	legacyNotes         = "for the WFT Companion app"
	legacySavedVars     = "WoWForeverTrackerDB"
)

var legacyFiles = []string{"Util.lua", "Consent.lua", "Context.lua", "Group.lua", "Recorders/BossKill.lua"}

func legacyOurs(p string) bool {
	toc, err := os.ReadFile(filepath.Join(p, LegacyName+".toc"))
	if err != nil {
		return false
	}
	if hasSourceLine(toc) {
		return true
	}
	if b, err := os.ReadFile(filepath.Join(p, "Consent.lua")); err == nil && strings.HasPrefix(string(b), legacyConsentHeader) {
		return true
	}
	notes, saved := tocField(toc, "Notes"), tocField(toc, "SavedVariables")
	if !strings.Contains(notes, legacyNotes) || saved != legacySavedVars {
		return false
	}
	for _, f := range legacyFiles {
		if st, err := os.Stat(filepath.Join(p, filepath.FromSlash(f))); err != nil || st.IsDir() {
			return false
		}
	}
	return true
}

func tocField(toc []byte, key string) string {
	for _, l := range strings.Split(string(toc), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(strings.TrimPrefix(l, "\ufeff")), "## "+key+":"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func hasSourceLine(toc []byte) bool {
	for _, l := range strings.Split(string(toc), "\n") {
		if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(l, "\ufeff")), "## X-WFT-Source:") {
			return true
		}
	}
	return false
}

func RemoveAddon(addonsDir string) error {
	p := filepath.Join(addonsDir, addon.Name)
	if _, err := os.Stat(filepath.Join(p, addon.Name+".toc")); err != nil {
		return nil
	}
	slog.Info("install: removing addon", "dir", p)
	return os.RemoveAll(p)
}

func randomSuffix() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func WithInterface(files fs.FS, iface string) fs.FS {
	if iface == "" {
		return files
	}
	return tocFS{files, iface}
}

type tocFS struct {
	fs.FS
	iface string
}

func (t tocFS) ReadFile(name string) ([]byte, error) {
	b, err := fs.ReadFile(t.FS, name)
	if err != nil || !strings.HasSuffix(name, ".toc") {
		return b, err
	}
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "## Interface:") {
			end := ""
			if strings.HasSuffix(l, "\r") {
				end = "\r"
			}
			lines[i] = "## Interface: " + t.iface + end
		}
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func (t tocFS) ReadDir(name string) ([]fs.DirEntry, error) { return fs.ReadDir(t.FS, name) }
