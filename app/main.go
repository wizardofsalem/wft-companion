package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wizardofsalem/wft-companion/addon"
	"github.com/wizardofsalem/wft-companion/app/autostart"
	"github.com/wizardofsalem/wft-companion/app/config"
	"github.com/wizardofsalem/wft-companion/app/heartbeat"
	"github.com/wizardofsalem/wft-companion/app/hidden"
	"github.com/wizardofsalem/wft-companion/app/install"
	"github.com/wizardofsalem/wft-companion/app/logx"
	"github.com/wizardofsalem/wft-companion/app/safe"
	"github.com/wizardofsalem/wft-companion/app/service"
	"github.com/wizardofsalem/wft-companion/app/supervisor"
	"github.com/wizardofsalem/wft-companion/app/syspath"
	"github.com/wizardofsalem/wft-companion/app/tray"
	"github.com/wizardofsalem/wft-companion/app/updater"
	"github.com/wizardofsalem/wft-companion/app/updatestate"
	"github.com/wizardofsalem/wft-companion/app/uploader"
	"github.com/wizardofsalem/wft-companion/app/web"
)

var version = "0.7.0-dev"

type options struct {
	background, noInstall, forceInstall bool
	worker                              bool
	postUpdate                          string
	takeover                            int
}

func main() {
	var o options
	flag.BoolVar(&o.background, autostart.BackgroundFlag[2:], false, "start without opening the browser (used at login)")
	flag.BoolVar(&o.noInstall, "no-install", false, "Windows: run from where it is instead of installing itself")
	flag.BoolVar(&o.forceInstall, "force-install", false, "Windows: install even over a newer installed version")
	flag.BoolVar(&o.worker, "worker", false, "run as the worker (started by the supervisor)")
	flag.StringVar(&o.postUpdate, "post-update", "", "the version this update replaced (started by the updater)")
	flag.IntVar(&o.takeover, "takeover", 0, "take over from the supervisor with this PID (started by the updater)")
	uninstall := flag.Bool("uninstall", false, "remove the app (Windows Apps & features)")
	uninstallFinish := flag.String("uninstall-finish", "", "finish removing the app folder (internal)")
	showVersion := flag.Bool("version", false, "print the version")
	flag.Parse()
	if *showVersion {
		fmt.Println("WFT Companion", version, "with addon", addon.Version())
		return
	}
	dir, dirErr := config.Dir()
	if dirErr == nil && !o.worker {
		updatestate.IncrementStarts(dir)
	}
	if *uninstallFinish != "" {
		if err := install.Finish(*uninstallFinish); err != nil {
			os.Exit(1)
		}
		return
	}
	if dirErr != nil {
		fmt.Fprintln(os.Stderr, "WFT Companion:", dirErr)
		os.Exit(1)
	}
	if *uninstall {
		os.Exit(runUninstall(dir))
	}
	run := runSupervisor
	if o.worker {
		run = runWorker
	}
	if err := safe.Call("main", func() {
		if err := run(dir, o); err != nil {
			slog.Error("stopped with an error", "err", err)
			os.Exit(1)
		}
	}); err != nil {
		os.Exit(1)
	}
	os.Exit(exitCode)
}

var exitCode int

func prepare(dir, logName string) ([]string, string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	logPaths := setupLog(dir, logName)
	exe, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return logPaths, exe, nil
}

func runSupervisor(dir string, o options) error {
	_, exe, err := prepare(dir, "supervisor")
	if err != nil {
		return err
	}
	slog.Info("supervisor: starting", "version", version, "os", runtime.GOOS, "arch", runtime.GOARCH,
		"exe", exe, "configDir", dir, "pid", os.Getpid(), "background", o.background, "takeover", o.takeover)

	instancePath := filepath.Join(dir, "instance.json")
	if runtime.GOOS == "windows" {
		installDir, err := install.Dir(os.Getenv)
		if err != nil {
			slog.Warn("install: no install folder", "err", err)
		} else if !o.noInstall && !install.IsInstalled(exe, installDir) {
			ok, why := install.ShouldInstall(version, install.InstalledVersion(installDir), o.forceInstall)
			slog.Info("install: decision", "install", ok, "why", why)
			target := filepath.Join(installDir, install.ExeName)
			if !ok {
				if url := readInstance(instancePath); url != "" && web.Ping(url) {
					openBrowser(url)
				} else if err := hidden.Program(target).Start(); err != nil {
					return fmt.Errorf("starting %s: %w", target, err)
				}
				return nil
			}
			if target, err := install.CopySelf(exe, installDir, install.ExeName, version); err != nil {
				slog.Error("install: copying the program failed; running from here", "err", err)
			} else {
				stopRunning(instancePath)
				slog.Info("install: starting the installed copy", "exe", target)
				if err := hidden.Program(target).Start(); err != nil {
					return fmt.Errorf("starting %s: %w", target, err)
				}
				return nil
			}
		} else {
			install.CleanupOld(installDir)
			if install.IsInstalled(exe, installDir) {
				if err := install.Register(install.AppEntry(exe, version)); err != nil {
					slog.Warn("install: couldn't add the app to Windows' installed apps", "err", err)
				}
			}
		}
	}

	wait := time.Duration(0)
	if o.takeover != 0 {
		wait = 30 * time.Second
	}
	lock, err := supervisor.Lock(dir, wait)
	if errors.Is(err, supervisor.ErrLocked) {
		slog.Info("already running; opening its page")
		if url := readInstance(instancePath); url != "" && !o.background && web.Ping(url) {
			openBrowser(url)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("single-instance lock: %w", err)
	}
	defer lock.Close()
	updater.CleanFailed(exe)
	if updater.BootGuard(dir, exe) {
		lock.Close()
		args := []string{}
		if o.background {
			args = append(args, autostart.BackgroundFlag)
		}
		return hidden.Program(exe, args...).Start()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var homes []string
	if h, err := os.UserHomeDir(); err == nil {
		homes = append(homes, h)
	}
	first := true
	return supervisor.Run(ctx, supervisor.Config{
		Dir: dir, Exe: exe, Homes: homes,
		Args: func() []string {
			args := []string{"--worker"}
			if o.background || !first {
				args = append(args, autostart.BackgroundFlag)
			}
			if first && o.postUpdate != "" {
				args = append(args, "--post-update", o.postUpdate)
			}
			first = false
			return args
		},
		Stop: func() {
			if url := readInstance(instancePath); url != "" {
				web.RequestQuit(url)
			}
		},
		UpdateCode: updater.ExitStaged,
		OnStaged: func() bool {
			v, err := updater.Swap(dir, exe, version)
			if err != nil {
				slog.Error("update: swap failed; keeping this version", "err", err)
				return false
			}
			slog.Info("update: swapped in; starting it on trial", "version", v)
			return true
		},
		InGate: func() bool { return updater.InGate(dir) },
		OnHealthy: func() error {
			if err := updater.Promote(dir); err != nil {
				return err
			}
			return hidden.Program(exe, "--takeover", strconv.Itoa(os.Getpid()), autostart.BackgroundFlag).Start()
		},
		Rollback: func(reason string) error { return updater.Rollback(dir, exe, reason) },
	})
}

func runUninstall(dir string) int {
	removeAddon, proceed := install.AskUninstall()
	if !proceed {
		return 1
	}
	_, exe, err := prepare(dir, "supervisor")
	if err != nil {
		return 1
	}
	slog.Info("uninstall: from the installed-apps list", "removeAddon", removeAddon)
	stopRunning(filepath.Join(dir, "instance.json"))
	if lock, err := supervisor.Lock(dir, 20*time.Second); err == nil {
		lock.Close()
	}
	svc, err := service.New(dir, version)
	if err != nil {
		slog.Error("uninstall: couldn't read the settings", "err", err)
		return 1
	}
	svc.Exe = exe
	if d, err := install.Dir(os.Getenv); err == nil {
		svc.InstallDir = d
	}
	if err := svc.Uninstall(removeAddon); err != nil {
		slog.Error("uninstall failed", "err", err)
		return 1
	}
	return 0
}

func runWorker(dir string, o options) error {
	logPaths, exe, err := prepare(dir, "companion")
	if err != nil {
		return err
	}
	slog.Info("starting", "version", version, "addon", addon.Version(), "os", runtime.GOOS, "arch", runtime.GOARCH,
		"exe", exe, "configDir", dir, "pid", os.Getpid(), "background", o.background, "postUpdate", o.postUpdate)
	installDir := ""
	if runtime.GOOS == "windows" {
		if d, err := install.Dir(os.Getenv); err == nil {
			installDir = d
		}
	}
	instancePath := filepath.Join(dir, "instance.json")
	debug.SetMemoryLimit(96 << 20)

	svc, err := service.New(dir, version)
	if err != nil {
		return err
	}
	svc.Exe, svc.InstallDir, svc.LogPaths = exe, installDir, append(supervisorLogPaths(dir), logPaths...)
	svc.OpenURL = openBrowser
	svc.Start()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &web.Server{Backend: svc, Quit: stop}
	url, err := srv.Start()
	if err != nil {
		return err
	}
	defer srv.Close()
	writeInstance(instancePath, url)
	defer os.Remove(instancePath)

	if !o.background || !svc.Consented() {
		openBrowser(url)
	}
	var staged atomic.Bool
	if up := newUpdater(dir, svc, func() { staged.Store(true); stop() }); up != nil {
		svc.Updater = up
		safe.Go(ctx, "updater", func(ctx context.Context) {
			for !svc.Consented() {
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Second):
				}
			}
			up.Loop(ctx)
		})
	}
	trayDone := startTray(ctx, url, svc, stop)
	beatDone := safe.Go(ctx, "heartbeat", func(ctx context.Context) { beat(ctx, dir, svc) })
	safe.Go(ctx, "watch", svc.Watch)
	<-safe.Go(ctx, "service", svc.Run)
	flushCtx, cancelFlush := context.WithTimeout(context.Background(), 10*time.Second)
	safe.Call("flush", func() { svc.Flush(flushCtx) })
	cancelFlush()
	<-beatDone
	select {
	case <-trayDone:
	case <-time.After(2 * time.Second):
	}
	if staged.Load() {
		slog.Info("stopped for an update")
		exitCode = updater.ExitStaged
		return nil
	}
	slog.Info("stopped")
	return nil
}

func newUpdater(dir string, svc *service.Service, onStaged func()) *updater.Updater {
	if runtime.GOOS != "windows" || strings.HasSuffix(version, "-dev") {
		return nil
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return nil
	}
	u := &updater.Updater{Dir: dir, Base: updater.DefaultBase(), Channel: updater.DefaultChannel(), Version: version,
		AskFirst: svc.AskFirst, OnStaged: onStaged}
	if cfg.UpdateBase != "" {
		u.Base = cfg.UpdateBase
	}
	if cfg.UpdateChannel == "stable" || cfg.UpdateChannel == "beta" {
		u.Channel = cfg.UpdateChannel
	}
	return u
}

func beat(ctx context.Context, dir string, svc *service.Service) {
	healthy := false
	t := time.NewTicker(heartbeat.Interval)
	defer t.Stop()
	quick := time.NewTicker(time.Second)
	defer quick.Stop()
	for {
		cycle := svc.CycleAt()
		if err := heartbeat.Write(dir, heartbeat.Beat{PID: os.Getpid(), Version: version, At: time.Now(), CheckAt: cycle}); err != nil {
			slog.Warn("heartbeat: write failed", "err", err)
		}
		if !healthy && !cycle.IsZero() {
			healthy = true
			quick.Stop()
			if err := updatestate.ResetStarts(dir); err != nil {
				slog.Warn("boot guard: reset failed", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-quick.C:
		}
	}
}

func supervisorLogPaths(dir string) []string {
	r := logx.Rotator{Dir: filepath.Join(dir, "logs"), Name: "supervisor"}
	return r.Paths()
}

func startTray(ctx context.Context, url string, svc *service.Service, stop func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := tray.Run(ctx, tray.Menu{
			Tooltip:   "WFT Companion",
			Default:   0,
			State:     func() tray.State { return trayState(svc.Brief()) },
			AlertItem: tray.Item{Label: "Sign in…", Do: func() string { openBrowser(url); return "" }},
			Items: []tray.Item{
				{Label: "Open", Do: func() string { openBrowser(url); return "" }},
				{Label: "Save logs", Do: func() string {
					p, err := svc.SaveLogs()
					if err != nil {
						slog.Warn("tray: save logs failed", "err", err)
						return "Couldn't save the logs."
					}
					slog.Info("tray: logs saved", "path", p)
					if strings.EqualFold(filepath.Base(filepath.Dir(p)), "Desktop") {
						return "Logs saved to your desktop."
					}
					return "Logs saved."
				}},
				{},
				{Label: "Quit", Do: func() string {
					slog.Info("tray: quit")
					stop()
					return ""
				}},
			},
		})
		if err != nil && err != tray.ErrUnsupported {
			slog.Warn("tray: not shown", "err", err)
		}
	}()
	return done
}

func trayState(b service.Brief) tray.State {
	if b.Account.NeedsYou() {
		tip := "WFT Companion: not signed in"
		if b.Account.State == service.AccountSignInAgain {
			tip = "WFT Companion: sign in again"
		}
		return tray.State{Tooltip: tip, Alert: true, Notice: "Sign in again to keep uploading."}
	}
	switch {
	case b.WrongRegion:
		return tray.State{Tooltip: "WFT Companion: wrong region, open the app"}
	case b.NotKept != "":
		return tray.State{Tooltip: "WFT Companion: last upload not kept"}
	}
	switch b.Problem {
	case uploader.ProblemOffline:
		return tray.State{Tooltip: "WFT Companion: can't connect"}
	case uploader.ProblemUpdate:
		return tray.State{Tooltip: "WFT Companion: upload failed, update the app"}
	case uploader.ProblemServer:
		return tray.State{Tooltip: "WFT Companion: upload failed, trying again"}
	}
	if b.Uploading {
		return tray.State{Tooltip: "WFT Companion: uploading"}
	}
	return tray.State{Tooltip: "WFT Companion: up to date"}
}

func stopRunning(instancePath string) {
	url := readInstance(instancePath)
	if url == "" || !web.Ping(url) {
		return
	}
	slog.Info("install: asking the running copy to stop")
	web.RequestQuit(url)
	for i := 0; i < 20 && web.Ping(url); i++ {
		time.Sleep(250 * time.Millisecond)
	}
}

func setupLog(dir, name string) []string {
	os.Remove(filepath.Join(dir, "companion.log"))
	os.Remove(filepath.Join(dir, "companion.log.old"))
	level := slog.LevelInfo
	if os.Getenv("WFT_DEBUG") != "" {
		level = slog.LevelDebug
	}
	var homes []string
	if h, err := os.UserHomeDir(); err == nil {
		homes = append(homes, h)
	}
	r := &logx.Rotator{Dir: filepath.Join(dir, "logs"), Name: name, Homes: homes}
	slog.SetDefault(logx.New(r, level))
	return r.Paths()
}

type instance struct {
	URL string `json:"url"`
	PID int    `json:"pid"`
}

func readInstance(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var in instance
	if json.Unmarshal(b, &in) != nil {
		return ""
	}
	return in.URL
}

func writeInstance(p, url string) {
	b, _ := json.Marshal(instance{URL: url, PID: os.Getpid()})
	if err := config.WriteFileAtomic(p, b); err != nil {
		slog.Warn("instance file", "err", err)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = hidden.Program(syspath.System32("rundll32.exe"), "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = hidden.Program("/usr/bin/open", url)
	default:
		cmd = hidden.Program("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		slog.Warn("couldn't open the browser", "err", err)
		fmt.Println("Open", url)
		return
	}
	slog.Info("browser opened")
	go cmd.Wait()
}
