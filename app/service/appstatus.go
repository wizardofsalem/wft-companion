package service

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/wizardofsalem/wft-companion/app/config"
	"github.com/wizardofsalem/wft-companion/app/install"
	"github.com/wizardofsalem/wft-companion/app/wowpaths"
)

const AppStatusFile = "AppStatus.lua"

func appStatusLua(version string, seen time.Time, upload string) string {
	return "-- Written by WFT Companion while it runs: its version, the hour it last ran\n" +
		"-- and whether it can upload. The addon reads it when the game loads.\n" +
		"local _, ns = ...\n" +
		fmt.Sprintf("ns.appStatus = { version = %s, seen = %d, upload = %s }\n",
			strconv.Quote(version), seen.Truncate(time.Hour).Unix(), strconv.Quote(upload))
}

func (s *Service) uploadState() string {
	s.mu.Lock()
	up := s.up
	_, err := config.ParseCode(s.cfg.UploadCode)
	s.mu.Unlock()
	if err != nil || (up != nil && up.Status().NeedsSignIn) {
		return "signin"
	}
	return "ok"
}

func (s *Service) writeAppStatus() {
	s.mu.Lock()
	dir := s.wowDirLocked()
	s.mu.Unlock()
	if dir == "" {
		return
	}
	addonDir := filepath.Join(install.AddOnsDir(dir), wowpaths.AddonName)
	if _, err := os.Stat(addonDir); err != nil {
		return
	}
	p := filepath.Join(addonDir, AppStatusFile)
	want := appStatusLua(s.Version, s.Now(), s.uploadState())
	if b, err := os.ReadFile(p); err == nil && string(b) == want {
		return
	}
	if err := config.WriteFileAtomic(p, []byte(want)); err != nil {
		slog.Warn("app status: couldn't update the addon's AppStatus.lua", "err", err)
		return
	}
	slog.Info("app status: AppStatus.lua updated")
}
