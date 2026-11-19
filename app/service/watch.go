package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wizardofsalem/wft-companion/app/config"
	"github.com/wizardofsalem/wft-companion/app/diag"
	"github.com/wizardofsalem/wft-companion/app/wowpaths"
)

func (s *Service) adoptDetectedFolder() {
	if len(s.detected) == 0 {
		return
	}
	current := s.cfg.WoWDir
	if current != "" && ownFolder(current) {
		return
	}
	s.cfg.WoWDir = s.detected[0]
	if err := config.Save(s.Dir, s.cfg); err != nil {
		slog.Warn("settings: couldn't save the detected folder", "err", err)
		return
	}
	slog.Info("settings: detected folder saved", "wowDir", s.cfg.WoWDir, "was", current)
}

func ownFolder(dir string) bool {
	base := strings.TrimRight(dir, `\/`)
	if i := strings.LastIndexAny(base, `\/`); i >= 0 {
		base = base[i+1:]
	}
	for _, f := range wowpaths.GameFolders() {
		if strings.EqualFold(base, f) {
			return true
		}
	}
	return false
}

var StuckAfter = 90 * time.Second

func (s *Service) Watch(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	var since time.Time
	dumped := false
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			if s.mu.TryLock() {
				s.mu.Unlock()
				since, dumped = time.Time{}, false
				continue
			}
			if since.IsZero() {
				since = now
			}
			if !dumped && now.Sub(since) >= StuckAfter {
				dumped = true
				s.dumpStacks(now)
			}
		}
	}
}

func (s *Service) dumpStacks(now time.Time) {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	home, _ := os.UserHomeDir()
	text := diag.RedactPaths(diag.Redact(string(buf[:n])), []string{home})
	p := filepath.Join(s.Dir, "logs", "stuck-"+now.Format("20060102-150405")+".txt")
	if err := os.WriteFile(p, []byte(fmt.Sprintf("service lock held for %s\n\n%s", StuckAfter, text)), 0o600); err != nil {
		slog.Error("watch: the service seems stuck; couldn't write the stacks", "err", err)
		return
	}
	slog.Error("watch: the service seems stuck; stacks written", "path", p)
}
