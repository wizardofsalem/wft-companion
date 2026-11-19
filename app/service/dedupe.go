package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wizardofsalem/wft-companion/app/config"
	"github.com/wizardofsalem/wft-companion/app/savedvars"
)

var BatchEvery = 15 * time.Minute

var stateTypes = map[string]bool{
	"rankNames": true, "professions": true, "talents": true, "played": true, "reputations": true,
	"honor": true, "guildRoster": true, "guildLog": true, "guildInfo": true, "questsDone": true, "calendar": true, "guildChat": true, "zones": true, "dayCounts": true, "lockouts": true,
	"featsProbe": true, "session": true, "groupSession": true, "lootHistory": true, "kills": true, "gear": true,
	"charStats": true, "currencies": true, "inventory": true, "spellbook": true, "spellUse": true, "collections": true, "guildNews": true,
}

func stateKey(r savedvars.Record) string {
	guid := ""
	if c, ok := r.Data["character"].(map[string]any); ok {
		guid, _ = c["guid"].(string)
		if guid == "" {
			n, _ := c["name"].(string)
			sn, _ := c["surname"].(string)
			rl, _ := c["realm"].(string)
			guid = "name:" + n + " " + sn + "-" + rl
		}
	}
	extra := ""
	switch r.Type {
	case "guildRoster", "guildLog", "guildInfo", "guildNews", "calendar", "rankNames":
		g, _ := r.Data["guild"].(string)
		extra = strings.ToLower(g)
	case "guildChat", "zones", "dayCounts", "spellUse", "charStats":
		extra, _ = r.Data["day"].(string)
	case "featsProbe":
		extra = fmt.Sprint(r.Data["session"])
	case "session":
		extra = fmt.Sprint(r.Data["loginAt"])
	case "kills":
		extra = fmt.Sprint(r.Data["sessionLoginAt"])
	case "groupSession":
		extra = fmt.Sprint(r.Data["start"])
	case "lootHistory":
		extra, _ = r.Data["dropKey"].(string)
	}
	return r.Type + "|" + guid + "|" + extra
}

func contentHash(r savedvars.Record) string {
	f := make(map[string]any, len(r.Data))
	for k, v := range r.Data {
		switch k {
		case "id", "type", "at", "character", "addonVersion", "snapshot":
			continue
		}
		f[k] = v
	}
	b, _ := json.Marshal(f)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

type sentState struct {
	Hash string `json:"hash"`
	At   int64  `json:"at"`
}

const sentStateFile = "sent-state.json"

func (s *Service) loadSent() {
	s.sent = map[string]sentState{}
	b, err := os.ReadFile(filepath.Join(s.Dir, sentStateFile))
	if err != nil {
		return
	}
	if json.Unmarshal(b, &s.sent) != nil {
		slog.Warn("dedupe: sent-state.json damaged; starting afresh (the tracker dedupes)")
		s.sent = map[string]sentState{}
	}
}

func (s *Service) saveSent() {
	b, _ := json.Marshal(s.sent)
	if err := config.WriteFileAtomic(filepath.Join(s.Dir, sentStateFile), b); err != nil {
		slog.Warn("dedupe: saving sent-state.json failed", "err", err)
	}
}

func (s *Service) unchanged(r savedvars.Record) bool {
	if !stateTypes[r.Type] {
		return false
	}
	last, ok := s.sent[stateKey(r)]
	return ok && (last.Hash == contentHash(r) || r.At < last.At)
}

func (s *Service) noteSent(records []savedvars.Record) {
	changed := false
	for _, r := range records {
		if !stateTypes[r.Type] {
			continue
		}
		k := stateKey(r)
		if last, ok := s.sent[k]; ok && r.At < last.At {
			continue
		}
		s.sent[k] = sentState{Hash: contentHash(r), At: r.At}
		changed = true
	}
	if changed {
		s.saveSent()
	}
}
