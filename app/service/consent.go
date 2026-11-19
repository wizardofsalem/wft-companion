package service

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wizardofsalem/wft-companion/app/config"
	"github.com/wizardofsalem/wft-companion/app/install"
	"github.com/wizardofsalem/wft-companion/app/savedvars"
	"github.com/wizardofsalem/wft-companion/app/wowpaths"
)

func eligible(records []savedvars.Record, cfg config.Config, now time.Time) []savedvars.Record {
	c := cfg.Consent
	if c == nil {
		return nil
	}
	out := records[:0:0]
	for _, r := range records {
		if eligibleMeta(r.Type, r.At, cfg, now) {
			out = append(out, r)
		}
	}
	return out
}

func eligibleMeta(typ string, at int64, cfg config.Config, now time.Time) bool {
	c := cfg.Consent
	switch {
	case c == nil, cfg.Disabled[typ], HeldBack[typ], Retired[typ]:
		return false
	case c.Since[typ] > 0 && at < c.Since[typ]:
		return false
	}
	return true
}

func newConsent(now time.Time, includeExisting bool) *config.Consent {
	c := &config.Consent{Version: config.ConsentVersion, At: now.Unix(), IncludeExisting: includeExisting,
		Known: append([]string(nil), categoryTypes()...), Since: map[string]int64{}}
	if !includeExisting {
		for _, t := range categoryTypes() {
			c.Since[t] = now.Unix()
		}
	}
	return c
}

func (s *Service) Acknowledge(types []string, off []string) error {
	s.mu.Lock()
	if s.cfg.Consent == nil {
		s.mu.Unlock()
		return fmt.Errorf("finish the first-run screens first")
	}
	cfg := s.cfg
	c := *cfg.Consent
	c.Known = append([]string(nil), c.Known...)
	for _, t := range types {
		if _, ok := Labels[t]; ok && !c.KnowsCategory(t) {
			c.Known = append(c.Known, t)
		}
	}
	cfg.Consent = &c
	cfg.Disabled = copyMap(cfg.Disabled)
	for _, t := range off {
		if _, ok := Labels[t]; ok {
			cfg.Disabled[t] = true
		}
	}
	if err := config.Save(s.Dir, cfg); err != nil {
		s.mu.Unlock()
		return err
	}
	s.cfg = cfg
	s.invalidate()
	s.mu.Unlock()
	slog.Info("consent: new categories seen", "types", strings.Join(types, ","), "off", strings.Join(off, ","))
	s.writeAddonConsent()
	s.Kick()
	return nil
}

func copyMap(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		if v {
			out[k] = true
		}
	}
	return out
}

const ConsentFile = "Consent.lua"

func addonConsent(cfg config.Config) string {
	var b strings.Builder
	b.WriteString("-- Written by WFT Companion: the categories you chose to send.\n")
	b.WriteString("-- A category set to false is not recorded. Change it on the companion's\n")
	b.WriteString("-- page, not here (the companion rewrites this file).\n")
	b.WriteString("local _, ns = ...\n")
	if cfg.Consent == nil {
		b.WriteString("ns.consent = nil\n")
		return b.String()
	}
	types := append([]string(nil), categoryTypes()...)
	sort.Strings(types)
	b.WriteString("ns.consent = {\n")
	for _, t := range types {
		fmt.Fprintf(&b, "    %s = %t,\n", t, !cfg.Disabled[t])
	}
	b.WriteString("}\n")
	return b.String()
}

func (s *Service) writeAddonConsent() {
	s.mu.Lock()
	cfg := s.cfg
	dir := s.wowDirLocked()
	s.mu.Unlock()
	if dir == "" {
		return
	}
	addonDir := filepath.Join(install.AddOnsDir(dir), wowpaths.AddonName)
	if _, err := os.Stat(addonDir); err != nil {
		return
	}
	p := filepath.Join(addonDir, ConsentFile)
	want := addonConsent(cfg)
	if b, err := os.ReadFile(p); err == nil && string(b) == want {
		return
	}
	if err := config.WriteFileAtomic(p, []byte(want)); err != nil {
		slog.Warn("consent: couldn't update the addon's Consent.lua", "err", err)
		return
	}
	slog.Info("consent: addon's Consent.lua updated", "consented", cfg.Consent != nil)
}

func (s *Service) Preview(n int) []map[string]any {
	s.mu.Lock()
	cfg, up, ob := s.cfg, s.up, s.ob
	s.mu.Unlock()
	if up == nil || ob == nil || cfg.Consent == nil {
		return []map[string]any{}
	}
	var ids []string
	now := s.Now()
	for _, m := range ob.Index() {
		if eligibleMeta(m.Type, m.At, cfg, now) {
			ids = append(ids, m.ID)
		}
		if len(ids) >= n*4 {
			break
		}
	}
	pending, err := up.Pending(ob.Load(ids))
	if err != nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, min(n, len(pending)))
	for _, r := range pending[:min(n, len(pending))] {
		out = append(out, r.Data)
	}
	return out
}
