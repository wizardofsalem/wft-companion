package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/wizardofsalem/wft-companion/addon"
	"github.com/wizardofsalem/wft-companion/app/config"
	"github.com/wizardofsalem/wft-companion/app/diag"
	"github.com/wizardofsalem/wft-companion/app/install"
	"github.com/wizardofsalem/wft-companion/app/savedvars"
	"github.com/wizardofsalem/wft-companion/app/wowpaths"
)

func (s *Service) wowDirLocked() string {
	if s.cfg.WoWDir != "" {
		return s.cfg.WoWDir
	}
	if len(s.detected) > 0 {
		return s.detected[0]
	}
	return ""
}

func (s *Service) Start() {
	s.mu.Lock()
	first := s.firstRun
	s.firstRun = false
	cfg := s.cfg
	s.mu.Unlock()
	slog.Info("start", "firstRun", first, "ready", cfg.Ready(), "wowDir", cfg.WoWDir, "autostart", s.Autostart.Enabled())
	if first && s.Autostart.Supported() {
		if err := s.Autostart.Enable(); err != nil {
			slog.Error("first run: autostart failed", "err", err)
		} else {
			cfg.Autostart = true
			slog.Info("first run: starting with the computer from now on")
		}
		if err := config.Save(s.Dir, cfg); err != nil {
			slog.Error("first run: saving settings failed", "err", err)
		}
		s.mu.Lock()
		s.cfg.Autostart = cfg.Autostart
		s.mu.Unlock()
	}
	if _, err := s.EnsureAddon(false); err != nil {
		slog.Warn("start: addon install failed", "err", err)
	}
	s.writeAddonConsent()
}

func (s *Service) EnsureAddon(force bool) (string, error) {
	s.mu.Lock()
	dir := s.wowDirLocked()
	s.mu.Unlock()
	if dir == "" {
		slog.Info("addon: no WoW folder yet; not installing")
		return "", errors.New("Choose your game folder first.")
	}
	addons := install.AddOnsDir(dir)
	if removed, err := install.RemoveLegacyAddon(addons); err != nil {
		slog.Warn("addon: couldn't remove the pre-1.0 folder", "err", err)
	} else if removed {
		slog.Info("addon: the pre-1.0 folder removed")
	}
	installed := install.InstalledAddonVersion(addons)
	source := install.InstalledAddonSource(addons)
	bundled := addon.Version()
	newerThere := installed != "" && install.Newer(installed, bundled)
	sameCurse := source == "curseforge" && installed != "" && !install.Newer(bundled, installed)
	if newerThere || sameCurse || (!force && installed != "" && !install.Newer(bundled, installed)) {
		slog.Info("addon: left as it is", "installed", installed, "source", source, "bundled", bundled)
		s.writeAppStatus()
		if source == "curseforge" {
			return "CurseForge keeps your addon up to date.", nil
		}
		return "The addon is up to date.", nil
	}
	slog.Info("addon: installing", "dir", addons, "installed", installed, "bundled", bundled, "forced", force)
	if err := install.InstallAddon(install.WithInterface(s.AddonFiles, wowpaths.TocInterface()), addons); err != nil {
		slog.Warn("addon: install failed", "dir", addons, "err", err)
		return "", errors.New("The addon couldn't be installed. Close the game and try again.")
	}
	s.writeAddonConsent()
	s.writeAppStatus()
	return "The addon is installed. Restart the game to load it.", nil
}

func (s *Service) SaveLogs() (string, error) {
	st := s.Status()
	statusJSON, _ := json.MarshalIndent(st, "", "  ")

	s.mu.Lock()
	cfg := s.cfg
	paths := make([]string, 0, len(s.files))
	for p := range s.files {
		paths = append(paths, p)
	}
	s.mu.Unlock()
	if cfg.WoWDir != "" {
		if found, err := wowpaths.SavedVariablesFiles(cfg.WoWDir); err == nil {
			paths = append(paths, found...)
		}
	}
	sort.Strings(paths)

	in := diag.Input{
		Version: s.Version, OS: runtime.GOOS, Arch: runtime.GOARCH, Exe: s.Exe, ConfigDir: s.Dir,
		Status: string(statusJSON), AppLogPaths: s.LogPaths, Now: s.Now(),
	}
	if home, err := os.UserHomeDir(); err == nil {
		in.Homes = append(in.Homes, home)
	}
	if p := os.Getenv("USERPROFILE"); p != "" {
		in.Homes = append(in.Homes, p)
	}
	in.Homes = append(in.Homes, s.Homes...)
	code := "(none)"
	if c, err := config.ParseCode(cfg.UploadCode); err == nil {
		code = c.Region + "-wft1-[redacted]"
	} else if cfg.UploadCode != "" {
		code = "(saved but not valid)"
	}
	var off []string
	for k := range cfg.Disabled {
		off = append(off, k)
	}
	sort.Strings(off)
	in.Settings = []string{
		"upload code: " + code,
		"api: " + cfg.Base(),
		"wow folder: " + cfg.WoWDir,
		fmt.Sprintf("switched off in the app: %v", off),
		fmt.Sprintf("start with the computer: saved %v, actual %v", cfg.Autostart, s.Autostart.Enabled()),
		"install folder: " + s.InstallDir,
	}
	for _, c := range wowpaths.Candidates(s.Env) {
		in.Candidates = append(in.Candidates, diag.Candidate{Path: c, Found: wowpaths.IsClassicDir(s.Env, c)})
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		f := diag.AddonFile{Path: p}
		if info, err := os.Stat(p); err == nil {
			f.Size, f.Modified = info.Size(), info.ModTime()
		}
		if a, err := savedvars.ReadFile(p); err != nil {
			f.Err = err.Error()
		} else {
			f.Addon = a
		}
		in.Files = append(in.Files, f)
	}

	content := diag.Build(in)
	dir, how, err := diag.Desktop(s.Desktop)
	if err != nil {
		slog.Warn("save logs: no Desktop; using the app folder", "err", err)
		dir = s.Dir
	} else {
		slog.Info("save logs: Desktop found", "dir", dir, "how", how)
	}
	p, err := diag.Save(dir, s.Now(), content)
	if err != nil {
		slog.Error("save logs: failed", "dir", dir, "err", err)
		return "", err
	}
	slog.Info("save logs: written", "path", p, "bytes", len(content))
	return p, nil
}

func (s *Service) Uninstall(removeAddon bool) error {
	slog.Info("uninstall", "removeAddon", removeAddon)
	if err := s.Autostart.Disable(); err != nil {
		slog.Warn("uninstall: autostart off failed", "err", err)
		return errors.New("The app couldn't be removed. Try again.")
	}
	if removeAddon {
		s.mu.Lock()
		dir := s.wowDirLocked()
		s.mu.Unlock()
		if dir != "" {
			if _, err := install.RemoveLegacyAddon(install.AddOnsDir(dir)); err != nil {
				slog.Warn("uninstall: old addon removal failed", "err", err)
				return errors.New("The addon couldn't be removed. Close the game and try again.")
			}
			if err := install.RemoveAddon(install.AddOnsDir(dir)); err != nil {
				slog.Warn("uninstall: addon removal failed", "err", err)
				return errors.New("The addon couldn't be removed. Close the game and try again.")
			}
		}
	}
	if err := s.Unregister(); err != nil {
		slog.Warn("uninstall: unregister failed", "err", err)
		return errors.New("The app couldn't be removed. Try again.")
	}
	dirs := []string{s.Dir}
	if s.InstallDir != "" && s.Exe != "" && install.IsInstalled(s.Exe, s.InstallDir) &&
		filepath.Clean(s.InstallDir) != filepath.Clean(s.Dir) {
		dirs = append(dirs, s.InstallDir)
	}
	return s.Remove(dirs)
}
