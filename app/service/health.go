package service

import (
	"encoding/json"
	"os"
	"time"

	"github.com/wizardofsalem/wft-companion/addon"
	"github.com/wizardofsalem/wft-companion/app/install"
	"github.com/wizardofsalem/wft-companion/app/outbox"
	"github.com/wizardofsalem/wft-companion/app/safe"
	"github.com/wizardofsalem/wft-companion/app/supervisor"
	"github.com/wizardofsalem/wft-companion/app/updatestate"
)

type Health struct {
	Version      string    `json:"version"`
	AddonBundled string    `json:"addonBundled"`
	AddonVersion string    `json:"addonVersion,omitempty"`
	PID          int       `json:"pid"`
	StartedAt    time.Time `json:"startedAt"`
	UptimeSec    int64     `json:"uptimeSeconds"`

	Supervisor   *supervisor.State `json:"supervisor,omitempty"`
	LoopRestarts map[string]int    `json:"loopRestarts,omitempty"`

	CycleAt     *time.Time `json:"cycleAt,omitempty"`
	LastScan    *time.Time `json:"lastScan,omitempty"`
	LastUpload  *time.Time `json:"lastUpload,omitempty"`
	NextAttempt *time.Time `json:"nextAttempt,omitempty"`
	LastError   string     `json:"lastError,omitempty"`
	Resumes     int        `json:"resumes"`

	Queue      *outbox.Stats `json:"queue,omitempty"`
	QueueError string        `json:"queueError,omitempty"`
	DiskFull   bool          `json:"diskFull,omitempty"`
	Held       int           `json:"held"`

	ClockSkewSec int64 `json:"clockSkewSeconds"`
	ClockOff     bool  `json:"clockOff,omitempty"`

	BootStarts int            `json:"bootStarts"`
	Update     map[string]any `json:"update,omitempty"`
}

func (s *Service) Health() Health {
	s.mu.Lock()
	h := Health{
		Version:      s.Version,
		AddonBundled: addon.Version(),
		PID:          os.Getpid(),
		StartedAt:    s.started,
		UptimeSec:    int64(time.Since(s.started).Seconds()),
		CycleAt:      timePtr(s.cycleAt),
		LastScan:     timePtr(s.lastScan),
		Resumes:      s.resumes,
		QueueError:   s.queueErr,
		LoopRestarts: safe.Restarts(),
	}
	if dir := s.wowDirLocked(); dir != "" {
		h.AddonVersion = install.InstalledAddonVersion(install.AddOnsDir(dir))
	}
	for _, c := range s.files {
		h.Held += len(c.held)
	}
	up, ob := s.up, s.ob
	s.mu.Unlock()

	h.DiskFull = isDiskFull(h.QueueError)
	if ob != nil {
		q := ob.Stats()
		h.Queue = &q
	}
	if up != nil {
		u := up.Status()
		h.LastUpload = timePtr(u.LastUpload)
		if u.NextAttempt.After(s.Now()) {
			h.NextAttempt = timePtr(u.NextAttempt)
		}
		h.LastError = u.LastError
		h.ClockSkewSec = int64(u.ClockSkew.Seconds())
		h.ClockOff = u.ClockSkew > 10*time.Minute || u.ClockSkew < -10*time.Minute
	}
	if st, err := supervisor.ReadState(s.Dir); err == nil {
		h.Supervisor = &st
	}
	h.BootStarts = updatestate.Starts(s.Dir)
	if b, err := os.ReadFile(updatestate.Path(s.Dir)); err == nil {
		var all map[string]any
		if json.Unmarshal(b, &all) == nil {
			delete(all, "starts")
			if len(all) > 0 {
				h.Update = all
			}
		}
	}
	return h
}
