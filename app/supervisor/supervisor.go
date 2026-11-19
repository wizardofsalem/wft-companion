package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wizardofsalem/wft-companion/app/hidden"

	"github.com/wizardofsalem/wft-companion/app/config"
	"github.com/wizardofsalem/wft-companion/app/diag"
	"github.com/wizardofsalem/wft-companion/app/heartbeat"
)

var ErrLocked = errors.New("another WFT Companion is running")

const LockName = "companion.lock"

const (
	CrashLog  = "crash.log"
	StateFile = "supervisor.json"
)

var (
	Pauses      = []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 5 * time.Minute}
	CrashWindow = 10 * time.Minute
	CrashLimit  = 5
	LongPause   = 30 * time.Minute
	HangAfter   = 3 * time.Minute
	WatchEvery  = 30 * time.Second
	StopGrace   = 5 * time.Second
)

const (
	maxCrashTail = 64 << 10
	maxCrashLog  = 256 << 10
)

type Config struct {
	Dir   string
	Exe   string
	Args  func() []string
	Stop  func()
	Homes []string

	UpdateCode int
	OnStaged   func() bool
	InGate     func() bool
	OnHealthy  func() error
	Rollback   func(reason string) error
}

var (
	GateWait    = 2 * time.Minute
	GateCrashes = 3
)

var errHandover = errors.New("handed over to the updated supervisor")

type State struct {
	StartedAt   time.Time `json:"startedAt"`
	Restarts    int       `json:"restarts"`
	LastCrashAt time.Time `json:"lastCrashAt,omitzero"`
	LastCrash   string    `json:"lastCrash,omitempty"`
	Waiting     bool      `json:"waiting,omitempty"`
}

func ReadState(dir string) (State, error) {
	var s State
	b, err := os.ReadFile(filepath.Join(dir, StateFile))
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

func Lock(dir string, wait time.Duration) (io.Closer, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		f, err := tryLock(filepath.Join(dir, LockName))
		if err == nil || !errors.Is(err, ErrLocked) || time.Now().After(deadline) {
			return f, err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func Run(ctx context.Context, cfg Config) error {
	st := State{StartedAt: time.Now()}
	saveState(cfg.Dir, st)
	var crashes []time.Time
	gateCrashes := 0
	for {
		started := time.Now()
		gate := cfg.InGate != nil && cfg.InGate()
		code, reason, tail, err := runOnce(ctx, cfg, gate)
		if errors.Is(err, errHandover) {
			slog.Info("supervisor: update healthy; handed over to the new supervisor")
			return nil
		}
		if err != nil && gate && cfg.Rollback != nil {
			slog.Error("supervisor: updated program won't start; rolling back", "err", err)
			if rerr := cfg.Rollback("won't start: " + err.Error()); rerr != nil {
				return fmt.Errorf("%w (and the rollback failed: %v)", err, rerr)
			}
			gateCrashes = 0
			continue
		}
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			slog.Info("supervisor: stopped")
			return nil
		}
		if cfg.UpdateCode != 0 && code == cfg.UpdateCode && cfg.OnStaged != nil {
			if cfg.OnStaged() {
				gateCrashes = 0
			}
			continue
		}
		if gate && reason != "" {
			gateCrashes++
			writeCrashLog(cfg, time.Now(), "updated version: "+reason, tail)
			if gateCrashes >= GateCrashes || strings.HasPrefix(reason, "not healthy") {
				if cfg.Rollback != nil {
					if err := cfg.Rollback(reason); err != nil {
						slog.Error("supervisor: rollback failed", "err", err)
					}
				}
				gateCrashes = 0
				continue
			}
		}
		if code == 0 && reason == "" {
			slog.Info("supervisor: the worker quit; stopping")
			return nil
		}
		now := time.Now()
		writeCrashLog(cfg, now, reason, tail)
		st.Restarts++
		st.LastCrashAt, st.LastCrash = now, reason
		if now.Sub(started) > CrashWindow {
			crashes = crashes[:0]
		}
		crashes = append(crashes, now)
		recent := crashes[:0:0]
		for _, c := range crashes {
			if now.Sub(c) <= CrashWindow {
				recent = append(recent, c)
			}
		}
		crashes = recent
		pause := Pauses[min(len(crashes)-1, len(Pauses)-1)]
		if len(crashes) >= CrashLimit {
			pause = LongPause
			st.Waiting = true
			crashes = crashes[:0]
		}
		saveState(cfg.Dir, st)
		slog.Warn("supervisor: worker stopped unexpectedly; restarting", "reason", reason, "in", pause.String(), "restarts", st.Restarts)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pause):
		}
		if st.Waiting {
			st.Waiting = false
			saveState(cfg.Dir, st)
		}
	}
}

func runOnce(ctx context.Context, cfg Config, gate bool) (code int, reason string, tail []byte, err error) {
	var args []string
	if cfg.Args != nil {
		args = cfg.Args()
	}
	cmd := hidden.Command(cfg.Exe, args...)
	stderr := &ring{max: maxCrashTail}
	cmd.Stderr = stderr
	cmd.Stdout = io.Discard
	os.Remove(heartbeat.Path(cfg.Dir))
	if err := cmd.Start(); err != nil {
		return 0, "", nil, fmt.Errorf("starting the worker: %w", err)
	}
	slog.Info("supervisor: worker started", "pid", cmd.Process.Pid)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	started := time.Now()
	lastLook := time.Now()
	every := WatchEvery
	if gate {
		every = min(every, time.Second)
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	var hung, gateFailed bool
	for {
		select {
		case werr := <-exited:
			switch {
			case gateFailed:
				return -1, "not healthy within " + GateWait.String(), stderr.Bytes(), nil
			case hung:
				return -1, "no heartbeat for " + HangAfter.String() + " (stuck)", stderr.Bytes(), nil
			case werr == nil:
				return 0, "", nil, nil
			case ctx.Err() != nil:
				return 0, "", nil, nil
			}
			var exitErr *exec.ExitError
			if errors.As(werr, &exitErr) {
				return exitErr.ExitCode(), werr.Error(), stderr.Bytes(), nil
			}
			return -1, werr.Error(), stderr.Bytes(), nil
		case <-ctx.Done():
			stopWorker(cfg, cmd, exited)
			return 0, "", nil, nil
		case now := <-tick.C:
			if gate && !gateFailed {
				b, rerr := heartbeat.Read(cfg.Dir)
				switch {
				case rerr == nil && b.PID == cmd.Process.Pid && b.CheckAt.After(started):
					gate = false
					if cfg.OnHealthy != nil {
						if err := cfg.OnHealthy(); err != nil {
							slog.Error("supervisor: handover failed; keeping this worker", "err", err)
							continue
						}
						stopWorker(cfg, cmd, exited)
						return 0, "", nil, errHandover
					}
				case now.Sub(started) > GateWait:
					slog.Error("supervisor: updated version not healthy in time; stopping it", "pid", cmd.Process.Pid)
					gateFailed = true
					cmd.Process.Kill()
				}
				continue
			}
			slept := now.Sub(lastLook) > 2*WatchEvery+time.Minute
			lastLook = now
			if slept {
				started = now
				continue
			}
			if hung || now.Sub(started) < HangAfter {
				continue
			}
			b, rerr := heartbeat.Read(cfg.Dir)
			last := b.At
			if rerr != nil || b.PID != cmd.Process.Pid {
				last = started
			}
			if now.Sub(last) > HangAfter {
				slog.Error("supervisor: no heartbeat; stopping the worker", "pid", cmd.Process.Pid, "last", last)
				hung = true
				cmd.Process.Kill()
			}
		}
	}
}

func stopWorker(cfg Config, cmd *exec.Cmd, exited <-chan error) {
	if cfg.Stop != nil {
		cfg.Stop()
		select {
		case <-exited:
			return
		case <-time.After(StopGrace):
		}
	}
	cmd.Process.Kill()
	<-exited
}

func saveState(dir string, st State) {
	b, _ := json.Marshal(st)
	if err := config.WriteFileAtomic(filepath.Join(dir, StateFile), b); err != nil {
		slog.Warn("supervisor: saving state failed", "err", err)
	}
}

func writeCrashLog(cfg Config, at time.Time, reason string, tail []byte) {
	p := filepath.Join(cfg.Dir, CrashLog)
	entry := fmt.Sprintf("===== %s: %s =====\n%s\n", at.Format(time.RFC3339), reason, tail)
	entry = diag.RedactPaths(diag.Redact(entry), cfg.Homes)
	old, _ := os.ReadFile(p)
	all := append(old, entry...)
	if len(all) > maxCrashLog {
		all = all[len(all)-maxCrashLog:]
		if i := bytes.Index(all, []byte("\n===== ")); i >= 0 {
			all = all[i+1:]
		}
	}
	if err := config.WriteFileAtomic(p, all); err != nil {
		slog.Warn("supervisor: writing crash.log failed", "err", err)
	}
}

type ring struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (r *ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.max {
		r.buf = append([]byte(nil), r.buf[len(r.buf)-r.max:]...)
	}
	return len(p), nil
}

func (r *ring) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.buf...)
}
