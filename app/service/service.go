package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/wizardofsalem/wft-companion/addon"
	"github.com/wizardofsalem/wft-companion/app/diag"
	"github.com/wizardofsalem/wft-companion/app/install"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wizardofsalem/wft-companion/app/autostart"
	"github.com/wizardofsalem/wft-companion/app/config"
	"github.com/wizardofsalem/wft-companion/app/outbox"
	"github.com/wizardofsalem/wft-companion/app/savedvars"
	"github.com/wizardofsalem/wft-companion/app/schema"
	"github.com/wizardofsalem/wft-companion/app/updater"
	"github.com/wizardofsalem/wft-companion/app/uploader"
	"github.com/wizardofsalem/wft-companion/app/wowpaths"
)

const Interval = 30 * time.Second

var Labels = map[string]string{
	"rankNames":    "Ranks",
	"professions":  "Professions",
	"loot":         "Loot",
	"talents":      "Talents",
	"level":        "Levels",
	"quest":        "Quests",
	"instance":     "Dungeons",
	"sold":         "Sales",
	"gold":         "Gold",
	"death":        "Deaths",
	"played":       "Playtime",
	"reputations":  "Reputations",
	"honor":        "Honour",
	"guildRoster":  "Roster",
	"guildLog":     "Log",
	"gearChange":   "Equipped Gear Changes",
	"itemFacts":    "Catalogue",
	"charStats":    "Stats",
	"currencies":   "Currencies",
	"inventory":    "Bags",
	"spellbook":    "Spells",
	"spellUse":     "Casts",
	"collections":  "Collections",
	"guildNews":    "News",
	"guildInfo":    "Messages",
	"zones":        "Zones",
	"moments":      "Moments",
	"social":       "Emotes",
	"dayCounts":    "Counts",
	"lockouts":     "Lockouts",
	"charter":      "Charters",
	"lootWon":      "Rolls",
	"bgMatch":      "Battlegrounds",
	"inspect":      "Inspections",
	"duel":         "Duels",
	"questsDone":   "Completions",
	"xp":           "Experience",
	"calendar":     "Calendar",
	"guildChat":    "Chat",
	"featsProbe":   "Probe",
	"sighting":     "Sightings",
	"session":      "Sessions",
	"gear":         "Gear",
	"kills":        "Kills",
	"groupSession": "Groups",
	"fight":        "Bosses",
	"lootHistory":  "Drops",
}

var TestOnly = map[string]bool{"featsProbe": true}

func IsTestBuild(version string) bool {
	_, pre, ok := strings.Cut(version, "-")
	return ok && (strings.HasPrefix(pre, "test") || strings.HasPrefix(pre, "probe") || strings.HasPrefix(pre, "dev"))
}

var Retired = map[string]bool{"bossKill": true, "groupDeath": true}

func categoryTypes() []string {
	out := make([]string, 0, len(savedvars.Types))
	for _, t := range savedvars.Types {
		if !Retired[t] {
			out = append(out, t)
		}
	}
	return out
}

type Autostart struct {
	Enable, Disable func() error
	Enabled         func() bool
	Supported       func() bool
}

func SystemAutostart() Autostart {
	return Autostart{Enable: autostart.Enable, Disable: autostart.Disable, Enabled: autostart.Enabled, Supported: autostart.Supported}
}

type fileCache struct {
	modTime  time.Time
	size     int64
	meta     *savedvars.Addon
	counts   map[string]int
	total    int
	err      string
	held     []HeldRecord
	region   string
	regionAt int64
	seenMod  time.Time
	seenSize int64
	seenAt   time.Time
	changing time.Time
}

var (
	StableFor   = 2 * time.Second
	MaxUnstable = time.Minute
)

var (
	ResumeJump  = 2 * time.Minute
	ResumeDelay = 10 * time.Second
)

func clockJump(monotonic, wall time.Duration) (time.Duration, bool) {
	d := wall - monotonic
	return d, d > ResumeJump || d < -ResumeJump
}

type Service struct {
	Dir         string
	Version     string
	Env         wowpaths.Env
	Client      *http.Client
	OpenURL     func(string)
	SignInSleep func(context.Context, time.Duration) error
	Autostart   Autostart
	Now         func() time.Time

	Exe        string
	InstallDir string
	LogPaths   []string
	Desktop    diag.DesktopEnv
	AddonFiles fs.FS
	Remove     func(dirs []string) error
	Unregister func() error
	Updater    *updater.Updater
	Homes      []string

	mu                sync.Mutex
	firstRun          bool
	cfg               config.Config
	up                *uploader.Uploader
	ob                *outbox.Outbox
	files             map[string]*fileCache
	signing           *signing
	runTotal, runSent atomic.Int64
	runActive         atomic.Bool
	runFinished       atomic.Int64
	codeCheckedAt     time.Time
	signMsg           string
	detected          []string
	lastScan          time.Time
	cycleAt           time.Time
	resumes           int
	queueErr          string
	direct            []savedvars.Record
	unfinished        bool
	applyErr          string
	cycleNano         atomic.Int64
	sent              map[string]sentState
	force             bool
	filesChanged      bool
	lastAttempt       time.Time
	started           time.Time
	scanErr           string
	result            uploader.Result
	kick              chan struct{}
}

func New(dir, version string) (*Service, error) {
	s := &Service{Dir: dir, Version: version, Env: wowpaths.System(), Autostart: SystemAutostart(),
		Desktop: diag.SystemDesktop()}
	return s, s.init()
}

func (s *Service) init() error {
	if s.Now == nil {
		s.Now = time.Now
	}
	s.started = time.Now()
	s.files = map[string]*fileCache{}
	s.kick = make(chan struct{}, 1)
	if s.AddonFiles == nil {
		s.AddonFiles = addon.Files()
	}
	if s.Remove == nil {
		s.Remove = install.ScheduleRemoval
	}
	if s.Unregister == nil {
		s.Unregister = install.Unregister
	}
	_, statErr := os.Stat(filepath.Join(s.Dir, "config.json"))
	s.firstRun = errors.Is(statErr, os.ErrNotExist)
	cfg, err := config.Load(s.Dir)
	if err != nil {
		return err
	}
	s.cfg = cfg
	s.loadSent()
	s.rebuildUploader()
	if ob, err := outbox.Open(filepath.Join(s.Dir, "outbox"), s.Now); err != nil {
		slog.Error("outbox: can't open; sending straight from the addon's files", "err", err)
	} else {
		s.ob = ob
		for t := range Retired {
			if n := ob.RemoveType(t); n > 0 {
				slog.Info("outbox: dropped a retired type", "type", t, "records", n)
			}
		}
	}
	s.detected = wowpaths.Detect(s.Env)
	s.adoptDetectedFolder()
	return nil
}

func (s *Service) Init() error { return s.init() }

func (s *Service) rebuildUploader() {
	code, err := config.ParseCode(s.cfg.UploadCode)
	if err != nil {
		s.up = nil
		return
	}
	if s.devBuild() && s.cfg.APIBase == "" {
		slog.Warn("development build: not uploading until apiBase is set in config.json")
		s.up = nil
		return
	}
	s.up = &uploader.Uploader{
		Client:     s.Client,
		Base:       s.cfg.Base(),
		Code:       code,
		AppVersion: s.Version,
		StatePath:  filepath.Join(s.Dir, "uploaded-"+code.Region+".json"),
		OnSent:     func(n int) { s.runSent.Add(int64(n)) },
		Now:        s.Now,
	}
}

func (s *Service) devBuild() bool {
	return strings.HasSuffix(s.Version, "-dev")
}

func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(Interval)
	defer t.Stop()
	prev := time.Now()
	for {
		s.Tick(ctx)
		s.writeAddonConsent()
		s.writeAppStatus()
		s.mu.Lock()
		s.cycleAt = s.Now()
		s.mu.Unlock()
		s.cycleNano.Store(s.cycleAt.UnixNano())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.kick:
		}
		now := time.Now()
		if d, jumped := clockJump(now.Sub(prev), now.Round(0).Sub(prev.Round(0))); jumped {
			s.resumed(d)
		}
		prev = now
	}
}

func (s *Service) resumed(jump time.Duration) {
	slog.Info("resumed (sleep or clock change); checking again shortly", "jump", jump.Round(time.Second).String())
	s.mu.Lock()
	s.resumes++
	up := s.up
	s.mu.Unlock()
	if up != nil {
		up.RetryNow()
	}
	time.AfterFunc(ResumeDelay, s.Kick)
}

func (s *Service) CycleAt() time.Time {
	if n := s.cycleNano.Load(); n != 0 {
		return time.Unix(0, n)
	}
	return time.Time{}
}

func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *Service) Tick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("check: recovered from a panic", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	s.mu.Lock()
	cfg, up := s.cfg, s.up
	s.mu.Unlock()
	if cfg.WoWDir != "" && cfg.Consent == nil {
		scanErr := s.scan(cfg.WoWDir, nil)
		s.mu.Lock()
		s.lastScan, s.scanErr = s.Now(), scanErr
		s.mu.Unlock()
		return
	}
	if !cfg.Ready() || up == nil {
		slog.Debug("check: not set up yet")
		return
	}
	scanErr := s.scan(cfg.WoWDir, func(batch []savedvars.Record) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.queueLocked(up, cfg, batch)
	})
	s.mu.Lock()
	due := s.force || s.filesChanged || s.unfinished || s.Now().Sub(s.lastAttempt) >= BatchEvery
	checkCode := s.force || s.codeCheckedAt.IsZero() || s.Now().Sub(s.codeCheckedAt) >= CodeCheckEvery
	if checkCode {
		s.codeCheckedAt = s.Now()
	}
	if due {
		s.force, s.filesChanged, s.lastAttempt = false, false, s.Now()
	}
	if !due {
		s.lastScan, s.scanErr = s.Now(), scanErr
	}
	s.mu.Unlock()
	if checkCode {
		up.CheckCode(ctx)
	}
	up.CheckBatches(ctx)
	if !due {
		slog.Debug("check: queued; next upload with the batch")
		return
	}
	res, err := s.send(ctx, up, cfg)
	s.mu.Lock()
	s.unfinished = err != nil
	s.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastScan = s.Now()
	s.scanErr = scanErr
	s.result = res
	switch {
	case errors.Is(err, uploader.ErrWaiting):
		slog.Debug("check: waiting before the next upload", "pending", res.Pending)
	case err != nil:
		slog.Warn("check: upload failed", "err", err, "pending", res.Pending)
	case res.Requests > 0:
		slog.Info("check: uploaded", "requests", res.Requests, "accepted", res.Accepted, "duplicates", res.Duplicates,
			"rejected", res.Rejected, "pending", res.Pending)
	default:
		slog.Debug("check: nothing new")
	}
}

var CodeCheckEvery = 15 * time.Minute

var PageSize = 1000

func (s *Service) queueLocked(up *uploader.Uploader, cfg config.Config, records []savedvars.Record) {
	var fresh []savedvars.Record
	skipped := 0
	for _, r := range eligible(records, cfg, s.Now()) {
		if (s.ob != nil && s.ob.Has(r.ID)) || up.Settled(r) {
			continue
		}
		if s.unchanged(r) {
			skipped++
			continue
		}
		fresh = append(fresh, r)
	}
	if skipped > 0 {
		slog.Debug("dedupe: unchanged state records not queued", "records", skipped)
	}
	if s.ob == nil {
		s.direct = append(s.direct, fresh...)
		return
	}
	n, err := s.ob.Add(fresh)
	s.queueErr = ""
	if err != nil {
		s.queueErr = err.Error()
		s.direct = append(s.direct, fresh...)
		s.invalidate()
		slog.Warn("outbox: couldn't queue new records (disk full?); sending them directly", "err", err, "records", len(fresh))
	} else if n > 0 {
		slog.Info("outbox: queued", "records", n)
	}
}

func (s *Service) send(ctx context.Context, up *uploader.Uploader, cfg config.Config) (uploader.Result, error) {
	var total uploader.Result
	add := func(r uploader.Result) {
		total.Requests += r.Requests
		total.Accepted += r.Accepted
		total.Duplicates += r.Duplicates
		total.Untracked += r.Untracked
		total.Retry += r.Retry
		total.Rejected += r.Rejected
		total.Answered += r.Answered
		total.OtherRegion += r.OtherRegion
	}
	defer func() { up.NoteRun(total) }()
	s.mu.Lock()
	direct := s.direct
	s.direct = nil
	s.mu.Unlock()
	var ids, all []string
	if s.ob != nil {
		idx := s.ob.Index()
		all = make([]string, len(idx))
		now := s.Now()
		for i, m := range idx {
			all[i] = m.ID
			if eligibleMeta(m.Type, m.At, cfg, now) {
				ids = append(ids, m.ID)
			}
		}
	}
	untracked := up.Status().UntrackedIDs
	n := len(direct)
	for _, id := range ids {
		if !untracked[id] {
			n++
		}
	}
	if n > 0 {
		s.runTotal.Store(int64(n))
		s.runSent.Store(0)
		s.runActive.Store(true)
		defer func() {
			s.runFinished.Store(s.Now().UnixMilli())
			s.runActive.Store(false)
		}()
	}
	if len(direct) > 0 {
		res, err := up.Sync(ctx, direct)
		add(res)
		s.settle(up, direct)
		if err != nil {
			return total, err
		}
	}
	if s.ob == nil {
		return total, nil
	}
	up.Prune(all)
	var err error
	for start := 0; start < len(ids); start += PageSize {
		page := s.ob.Load(ids[start:min(start+PageSize, len(ids))])
		var res uploader.Result
		res, err = up.Sync(ctx, page)
		add(res)
		s.settle(up, page)
		if err != nil {
			break
		}
	}
	total.Pending = s.ob.Stats().Records
	return total, err
}

func (s *Service) settle(up *uploader.Uploader, records []savedvars.Record) {
	var done []string
	var settled []savedvars.Record
	for _, r := range records {
		if up.Settled(r) {
			done = append(done, r.ID)
			settled = append(settled, r)
		}
	}
	if len(done) == 0 {
		return
	}
	s.mu.Lock()
	s.noteSent(settled)
	s.mu.Unlock()
	if s.ob != nil {
		s.ob.Remove(done)
	}
	slog.Debug("outbox: settled", "records", len(done))
}

type HeldRecord struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Field string `json:"field"`
	Rule  string `json:"rule"`
}

func checkRecords(records []savedvars.Record, now time.Time, path string) ([]savedvars.Record, []HeldRecord) {
	ok := records[:0:0]
	var held []HeldRecord
	for _, r := range records {
		p, pass := schema.Check(r.Data, now)
		if pass {
			ok = append(ok, r)
			continue
		}
		h := HeldRecord{ID: r.ID, Type: r.Type, Field: p.Field, Rule: p.Rule}
		if len(held) < 20 {
			slog.Warn("check: record held back", "id", h.ID, "type", h.Type, "field", h.Field, "rule", h.Rule)
		}
		held = append(held, h)
	}
	if len(held) > 0 {
		slog.Warn("check: records held back in this file", "path", path, "held", len(held), "passed", len(ok))
	}
	return ok, held
}

func (s *Service) stable(c *fileCache, info os.FileInfo, p string) bool {
	now := s.Now()
	same := c.seenMod.Equal(info.ModTime()) && c.seenSize == info.Size()
	if c.changing.IsZero() {
		c.changing = now
	}
	switch {
	case StableFor <= 0, now.Sub(info.ModTime()) >= StableFor:
		return true
	case same && now.Sub(c.seenAt) >= StableFor:
		return true
	case now.Sub(c.changing) >= MaxUnstable:
		slog.Warn("scan: file keeps changing; reading it anyway", "path", p)
		return true
	}
	if !same {
		c.seenMod, c.seenSize, c.seenAt = info.ModTime(), info.Size(), now
	}
	slog.Debug("scan: file changed; waiting until it stops changing", "path", p)
	time.AfterFunc(StableFor, s.Kick)
	return false
}

func (s *Service) scan(dir string, sink func([]savedvars.Record)) string {
	paths, err := wowpaths.SavedVariablesFiles(dir)
	if err != nil {
		slog.Warn("scan: can't list accounts", "dir", dir, "err", err)
		return "We can't open your game folder. Check it under Setup."
	}
	present := map[string]bool{}
	parsed := false
	for _, p := range paths {
		present[p] = true
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		s.mu.Lock()
		c := s.files[p]
		if c == nil {
			c = &fileCache{}
			s.files[p] = c
		}
		ready := !(c.modTime.Equal(info.ModTime()) && c.size == info.Size()) && s.stable(c, info, p)
		now := s.Now()
		s.mu.Unlock()
		if !ready {
			continue
		}
		slog.Info("scan: file changed", "path", p, "size", info.Size(), "modified", info.ModTime().Format(time.RFC3339))
		counts := map[string]int{}
		total := 0
		var held []HeldRecord
		var batch []savedvars.Record
		region, regionAt := "", int64(0)
		meta, err := savedvars.Stream(p, func(r savedvars.Record) error {
			if rg := recordRegion(r); rg != "" && (region == "" || r.At > regionAt) {
				region, regionAt = rg, r.At
			}
			if pr, ok := schema.Check(r.Data, now); !ok {
				h := HeldRecord{ID: r.ID, Type: r.Type, Field: pr.Field, Rule: pr.Rule}
				if len(held) < 20 {
					slog.Warn("check: record held back", "id", h.ID, "type", h.Type, "field", h.Field, "rule", h.Rule)
				}
				held = append(held, h)
				return nil
			}
			counts[r.Type]++
			total++
			if sink != nil {
				batch = append(batch, r)
				if len(batch) >= PageSize {
					sink(batch)
					batch = nil
				}
			}
			return nil
		})
		if err == nil && sink != nil && len(batch) > 0 {
			sink(batch)
		}
		parsed = true
		s.mu.Lock()
		if err != nil {
			c.err = "couldn't read this file yet (will try again): " + err.Error()
			s.mu.Unlock()
			slog.Warn("scan: parse failed (will retry)", "path", p, "err", err)
			continue
		}
		s.filesChanged = true
		c.meta, c.counts, c.total, c.held, c.err = meta, counts, total, held, ""
		c.region, c.regionAt = region, regionAt
		c.modTime, c.size = info.ModTime(), info.Size()
		c.changing = time.Time{}
		s.mu.Unlock()
		if len(held) > 0 {
			slog.Warn("check: records held back in this file", "path", p, "held", len(held), "passed", total)
		}
		slog.Info("scan: read", "path", p, "records", total, "byType", fmt.Sprint(counts), "held", len(held),
			"skipped", meta.Skipped, "addonVersion", meta.Diag.Version, "addonLogLines", len(meta.Diag.Log), "region", region)
	}
	s.mu.Lock()
	for p := range s.files {
		if !present[p] {
			slog.Info("scan: file gone", "path", p)
			delete(s.files, p)
		}
	}
	s.mu.Unlock()
	if parsed {
		debug.FreeOSMemory()
	}
	if len(paths) == 0 {
		return "Nothing to upload yet. Log in to the game, then log out."
	}
	return ""
}

func (s *Service) invalidate() {
	for _, c := range s.files {
		c.modTime = time.Time{}
	}
}

var HeldBack = map[string]bool{}

type Settings struct {
	WoWDir          string          `json:"wowDir"`
	UploadCode      string          `json:"uploadCode"`
	Disabled        map[string]bool `json:"disabled"`
	Autostart       bool            `json:"autostart"`
	AskFirst        *bool           `json:"askFirst,omitempty"`
	Agree           bool            `json:"agree"`
	IncludeExisting bool            `json:"includeExisting"`
}

func (s *Service) Configure(in Settings) error {
	dir := wowpaths.Normalize(s.Env, in.WoWDir)
	slog.Info("settings: requested", "wowDir", in.WoWDir, "normalised", dir, "newCode", strings.TrimSpace(in.UploadCode) != "")
	if dir == "" {
		return errors.New("Choose your game folder.")
	}
	if !wowpaths.IsClassicDir(s.Env, dir) {
		slog.Info("settings: folder refused", "dir", dir, "want", strings.Join(wowpaths.GameFolders(), " or "))
		return errors.New("That isn't your game folder. Choose another one.")
	}
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	keepFolder := func(err error) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.cfg.WoWDir != dir {
			c := s.cfg
			c.WoWDir = dir
			if serr := config.Save(s.Dir, c); serr == nil {
				s.cfg.WoWDir = dir
				slog.Info("settings: folder saved; the rest was refused", "wowDir", dir, "reason", err)
			}
		}
		return err
	}
	if strings.TrimSpace(in.UploadCode) != "" {
		code, err := config.ParseCode(in.UploadCode)
		if err != nil {
			slog.Info("settings: upload code refused", "reason", err)
			return keepFolder(err)
		}
		cfg.UploadCode = code.String()
	}
	cfg.WoWDir = dir
	if in.AskFirst != nil {
		cfg.UpdateAskFirst = *in.AskFirst
	}
	cfg.Disabled = map[string]bool{}
	for k, off := range in.Disabled {
		if _, known := Labels[k]; known && off {
			cfg.Disabled[k] = true
		}
	}
	now := s.Now()
	switch {
	case cfg.Consent == nil && !in.Agree:
		return keepFolder(errors.New("Read what is sent, then press Agree."))
	case cfg.Consent == nil:
		cfg.Consent = newConsent(now, in.IncludeExisting)
		slog.Info("consent: agreed", "includeExisting", in.IncludeExisting, "version", cfg.Consent.Version)
	default:
		c := *cfg.Consent
		c.Since = map[string]int64{}
		for k, v := range cfg.Consent.Since {
			c.Since[k] = v
		}
		for k := range s.cfg.Disabled {
			if s.cfg.Disabled[k] && !cfg.Disabled[k] {
				c.Since[k] = now.Unix()
				slog.Info("consent: category switched back on; earlier records skipped", "type", k)
			}
		}
		cfg.Consent = &c
	}
	cfg.Autostart = in.Autostart
	if err := config.Save(s.Dir, cfg); err != nil {
		slog.Error("settings: save failed", "err", err)
		return errors.New("Your settings couldn't be saved. Try again.")
	}
	var off []string
	for k := range cfg.Disabled {
		off = append(off, k)
	}
	sort.Strings(off)
	region := ""
	if code, err := config.ParseCode(cfg.UploadCode); err == nil {
		region = code.Region
	}
	slog.Info("settings: saved", "wowDir", cfg.WoWDir, "region", region, "codeChanged", cfg.UploadCode != s.cfg.UploadCode,
		"switchedOff", strings.Join(off, ","), "autostart", cfg.Autostart)
	s.mu.Lock()
	if s.ob != nil {
		for k := range cfg.Disabled {
			if !s.cfg.Disabled[k] {
				if n := s.ob.RemoveType(k); n > 0 {
					slog.Info("outbox: dropped a switched-off category", "type", k, "records", n)
				}
			}
		}
	}
	codeChanged := cfg.UploadCode != s.cfg.UploadCode
	s.cfg = cfg
	s.invalidate()
	if codeChanged || s.up == nil {
		s.rebuildUploader()
	}
	s.mu.Unlock()
	if ApplyInBackground {
		go s.applySettings(in.Autostart)
	} else {
		s.applySettings(in.Autostart)
	}
	return nil
}

func (s *Service) SyncNow() {
	s.mu.Lock()
	up := s.up
	s.force = true
	s.mu.Unlock()
	if up != nil {
		up.RetryNow()
	}
	s.Kick()
}

func (s *Service) Detect() []string {
	found := wowpaths.Detect(s.Env)
	s.mu.Lock()
	s.detected = found
	s.mu.Unlock()
	return found
}

type Category struct {
	Type         string `json:"type"`
	Label        string `json:"label"`
	TestOnly     bool   `json:"testOnly,omitempty"`
	Enabled      bool   `json:"enabled"`
	AddonEnabled *bool  `json:"addonEnabled"`
	InFiles      int    `json:"inFiles"`
	Pending      int    `json:"pending"`
	Held         int    `json:"held,omitempty"`
	Uploaded     int    `json:"uploaded"`
	HeldBack     bool   `json:"heldBack"`
}

type File struct {
	Account  string    `json:"account"`
	Path     string    `json:"path"`
	Records  int       `json:"records"`
	Modified time.Time `json:"modified"`
	Error    string    `json:"error,omitempty"`
}

type Rejected struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type Progress struct {
	Total      int        `json:"total"`
	Sent       int        `json:"sent"`
	Active     bool       `json:"active"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type Status struct {
	Problem            string                  `json:"problem,omitempty"`
	Progress           *Progress               `json:"progress,omitempty"`
	Version            string                  `json:"version"`
	TestBuild          bool                    `json:"testBuild"`
	Ready              bool                    `json:"ready"`
	GameName           string                  `json:"gameName"`
	Consented          bool                    `json:"consented"`
	IncludeExisting    bool                    `json:"includeExisting,omitempty"`
	Account            Account                 `json:"account"`
	Guilds             []GuildState            `json:"guilds,omitempty"`
	UploadQueue        []QueueLine             `json:"uploadQueue,omitempty"`
	Own                map[string]Own          `json:"own,omitempty"`
	GoldShared         *bool                   `json:"goldShared,omitempty"`
	Update             *updater.Status         `json:"update,omitempty"`
	WoWDir             string                  `json:"wowDir"`
	Detected           []string                `json:"detected"`
	AddonInstalled     bool                    `json:"addonInstalled"`
	AddonVersion       string                  `json:"addonVersion,omitempty"`
	AddonSource        string                  `json:"addonSource,omitempty"`
	LegacyAddon        bool                    `json:"legacyAddon,omitempty"`
	AddonBundled       string                  `json:"addonBundled"`
	AddonsDir          string                  `json:"addonsDir,omitempty"`
	CodeHint           string                  `json:"codeHint,omitempty"`
	Region             string                  `json:"region,omitempty"`
	Waiting            int                     `json:"waiting,omitempty"`
	Untracked          int                     `json:"untracked,omitempty"`
	GaveUp             int                     `json:"gaveUp,omitempty"`
	Autostart          bool                    `json:"autostart"`
	AutostartSupported bool                    `json:"autostartSupported"`
	Categories         []Category              `json:"categories"`
	Files              []File                  `json:"files"`
	LastScan           *time.Time              `json:"lastScan,omitempty"`
	LastUpload         *time.Time              `json:"lastUpload,omitempty"`
	NextAttempt        *time.Time              `json:"nextAttempt,omitempty"`
	Batch              *Batch                  `json:"batch,omitempty"`
	Linked             bool                    `json:"linked,omitempty"`
	LinkedAt           *time.Time              `json:"linkedAt,omitempty"`
	Message            string                  `json:"message,omitempty"`
	Error              string                  `json:"error,omitempty"`
	Rejected           []Rejected              `json:"rejected,omitempty"`
	Held               []HeldRecord            `json:"held,omitempty"`
	HeldTotal          int                     `json:"heldTotal,omitempty"`
	Queue              *outbox.Stats           `json:"queue,omitempty"`
	Probe              *savedvars.ProbeSummary `json:"probe,omitempty"`
	RegionFound        string                  `json:"regionFound,omitempty"`
	WrongRegion        bool                    `json:"wrongRegion,omitempty"`
	NotKept            string                  `json:"notKept,omitempty"`
}

type Batch struct {
	Queued      int        `json:"queued,omitempty"`
	Held        int        `json:"held,omitempty"`
	NextBatchAt *time.Time `json:"nextBatchAt,omitempty"`
	DoneAt      *time.Time `json:"doneAt,omitempty"`
	Done        int        `json:"done,omitempty"`
	Counted     int        `json:"counted"`
	Duplicates  int        `json:"duplicates"`
	Dropped     int        `json:"dropped"`
	Rejected    int        `json:"rejected"`
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (s *Service) Status() Status {
	s.mu.Lock()
	st := Status{
		Version:            s.Version,
		TestBuild:          IsTestBuild(s.Version),
		Ready:              s.cfg.Ready(),
		GameName:           wowpaths.GameName(),
		Consented:          s.cfg.Consent != nil,
		IncludeExisting:    s.cfg.Consent != nil && s.cfg.Consent.IncludeExisting,
		WoWDir:             s.cfg.WoWDir,
		Detected:           append([]string{}, s.detected...),
		Autostart:          s.cfg.Autostart,
		AutostartSupported: s.Autostart.Supported(),
		LastScan:           timePtr(s.lastScan),
		Message:            s.scanErr,
	}
	st.AddonBundled = addon.Version()
	wowDir := s.wowDirLocked()
	if code, err := config.ParseCode(s.cfg.UploadCode); err == nil {
		st.CodeHint = fmt.Sprintf("%s-wft1-…%s", code.Region, code.Key[len(code.Key)-4:])
		st.Region = strings.ToUpper(code.Region)
	}
	found, _ := s.filesRegionLocked()
	st.RegionFound = strings.ToUpper(found)

	inFiles := map[string]int{}
	heldBy := map[string]int{}
	addonEnabled := map[string]bool{}
	addonKnown := false
	paths := make([]string, 0, len(s.files))
	for p := range s.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		c := s.files[p]
		f := File{Path: p, Account: filepath.Base(filepath.Dir(filepath.Dir(p))), Modified: c.modTime, Error: c.err}
		if c.meta != nil {
			if p := c.meta.LatestProbe(); p != nil && (st.Probe == nil || p.StartedAt().After(st.Probe.StartedAt)) {
				sum := p.Summary()
				st.Probe = &sum
			}
			f.Records = c.total
			for t, n := range c.counts {
				inFiles[t] += n
			}
			for _, h := range c.held {
				heldBy[h.Type]++
				st.HeldTotal++
				if len(st.Held) < 20 {
					st.Held = append(st.Held, h)
				}
			}
			for k, v := range c.meta.Enabled {
				addonKnown = true
				addonEnabled[k] = addonEnabled[k] || v
			}
		}
		st.Files = append(st.Files, f)
	}

	cfg, up, ob, updater, applyErr := s.cfg, s.up, s.ob, s.Updater, s.applyErr
	dev := s.devBuild()
	s.mu.Unlock()

	if wowDir != "" {
		st.AddonsDir = install.AddOnsDir(wowDir)
		st.AddonInstalled = wowpaths.AddonInstalled(wowDir)
		st.AddonVersion = install.InstalledAddonVersion(st.AddonsDir)
		st.AddonSource = install.InstalledAddonSource(st.AddonsDir)
		st.LegacyAddon = install.LegacyAddonLeft(st.AddonsDir)
	}
	pend := map[string]int{}
	var upStatus uploader.Status
	if up != nil {
		upStatus = up.Status()
		if ob != nil {
			q := ob.Stats()
			st.Queue = &q
			now := s.Now()
			for _, m := range ob.Index() {
				if eligibleMeta(m.Type, m.At, cfg, now) && !upStatus.UntrackedIDs[m.ID] {
					pend[m.Type]++
				}
			}
		}
		st.LastUpload = timePtr(upStatus.LastUpload)
		if upStatus.NextAttempt.After(s.Now()) {
			st.NextAttempt = timePtr(upStatus.NextAttempt)
		}
		st.Problem = upStatus.Problem
		st.NotKept = upStatus.NotKept
		st.WrongRegion = wrongRegion(st.Region, st.RegionFound, st.NotKept)
		st.Waiting, st.Untracked, st.GaveUp = upStatus.Waiting, upStatus.Untracked, upStatus.GaveUp
		st.Linked = upStatus.Linked
		if upStatus.Linked && !upStatus.LinkedAt.IsZero() {
			st.LinkedAt = timePtr(upStatus.LinkedAt)
		}
		if b := upStatus.Batch; b != nil {
			st.Batch = &Batch{Queued: b.Queued, Held: b.Held, NextBatchAt: timePtr(b.NextBatchAt), DoneAt: timePtr(b.DoneAt), Done: b.Done,
				Counted: b.Counts.Counted, Duplicates: b.Counts.Duplicates, Dropped: b.Counts.Dropped, Rejected: b.Counts.Rejected}
		}
		ids := make([]string, 0, len(upStatus.Rejected))
		for id := range upStatus.Rejected {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids[:min(len(ids), 20)] {
			st.Rejected = append(st.Rejected, Rejected{ID: id, Reason: upStatus.Rejected[id].Reason})
		}
	}
	for _, typ := range categoryTypes() {
		c := Category{Type: typ, Label: Labels[typ], TestOnly: TestOnly[typ], Enabled: !cfg.Disabled[typ], InFiles: inFiles[typ],
			Pending: pend[typ], Held: heldBy[typ], Uploaded: upStatus.Totals[typ], HeldBack: HeldBack[typ]}
		if addonKnown {
			on := addonEnabled[typ]
			c.AddonEnabled = &on
		}
		st.Categories = append(st.Categories, c)
	}
	if t := s.runTotal.Load(); t > 0 {
		st.Progress = &Progress{Total: int(t), Sent: int(min(s.runSent.Load(), t)), Active: s.runActive.Load()}
		if f := s.runFinished.Load(); f > 0 && !st.Progress.Active {
			st.Progress.FinishedAt = timePtr(time.UnixMilli(f))
		}
	}
	s.mu.Lock()
	if up != nil {
		st.Account = s.accountLocked(&upStatus)
	} else {
		st.Account = s.accountLocked(nil)
	}
	s.mu.Unlock()
	switch {
	case st.Ready && up == nil && dev:
		st.Message = "Uploads are off in this test build."
	case st.Consented && st.Account.State == AccountNone:
		st.Message = "Sign in to start uploading."
	case !st.Ready:
		st.Message = "Finish setting up to start uploading."
	case !st.AddonInstalled && st.Message == "":
		st.Message = "The addon is missing. Go to Updates and press Reinstall the addon."
	}
	if applyErr != "" && st.Error == "" {
		var lines []string
		if strings.Contains(applyErr, "starting with the computer") {
			lines = append(lines, "Starting with your computer couldn't be changed.")
		}
		if strings.Contains(applyErr, "addon") {
			lines = append(lines, "The addon couldn't be installed. Close the game, then go to Updates and press Reinstall the addon.")
		}
		if len(lines) == 0 {
			lines = append(lines, "Something went wrong. Go to Help, press Save logs and send us the file.")
		}
		st.Error = strings.Join(lines, " ")
	}
	if updater != nil {
		u := updater.Status()
		st.Update = &u
	}
	return st
}

func wrongRegion(code, found, notKept string) bool {
	if code == "" {
		return false
	}
	return notKept == uploader.NotKeptRegion || (found != "" && found != code)
}

var ApplyInBackground = true

func (s *Service) applySettings(autostartOn bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("settings: applying failed", "panic", fmt.Sprint(r))
		}
	}()
	var problems []string
	if s.Autostart.Supported() && autostartOn != s.Autostart.Enabled() {
		toggle := s.Autostart.Disable
		if autostartOn {
			toggle = s.Autostart.Enable
		}
		if err := toggle(); err != nil {
			slog.Error("settings: autostart change failed; the other settings are saved", "enable", autostartOn, "err", err)
			problems = append(problems, "starting with the computer couldn't be changed: "+err.Error())
			s.mu.Lock()
			s.cfg.Autostart = s.Autostart.Enabled()
			config.Save(s.Dir, s.cfg)
			s.mu.Unlock()
		} else {
			slog.Info("settings: autostart changed", "enabled", autostartOn)
		}
	}
	if _, err := s.EnsureAddon(false); err != nil {
		slog.Warn("settings: addon install failed", "err", err)
		problems = append(problems, "the addon couldn't be installed: "+err.Error())
	}
	s.writeAddonConsent()
	s.mu.Lock()
	s.applyErr = strings.Join(problems, "; ")
	s.mu.Unlock()
	s.SyncNow()
}
