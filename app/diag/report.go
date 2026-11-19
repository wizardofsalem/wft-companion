package diag

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wizardofsalem/wft-companion/app/savedvars"
)

type Input struct {
	Version     string
	OS, Arch    string
	Exe         string
	ConfigDir   string
	Settings    []string
	Candidates  []Candidate
	Status      string
	AppLogPaths []string
	Files       []AddonFile
	Now         time.Time
	Homes       []string
}

type Candidate struct {
	Path  string
	Found bool
}

type AddonFile struct {
	Path     string
	Size     int64
	Modified time.Time
	Addon    *savedvars.Addon
	Err      string
}

const MaxAppLog = 2 << 20

var (
	codePattern  = regexp.MustCompile(`(?i)wft1-[a-z2-7]{32}`)
	tokenPattern = regexp.MustCompile(`(127\.0\.0\.1:\d+/)[0-9a-f]{32}`)
)

var (
	accountPathPattern = regexp.MustCompile(`(?i)(WTF(?:\\\\|\\|/)+Account(?:\\\\|\\|/)+)[^\\/\r\n"]+`)
	accountJSONPattern = regexp.MustCompile(`("account":\s*")[^"]*"`)
)

func RedactPaths(s string, homes []string) string {
	for _, h := range homes {
		h = strings.TrimRight(h, `\/`)
		if len(h) < 3 {
			continue
		}
		for _, form := range []string{strings.ReplaceAll(h, `\`, `\\`), h} {
			s = regexp.MustCompile(`(?i)`+regexp.QuoteMeta(form)).ReplaceAllLiteralString(s, "~")
		}
	}
	s = accountPathPattern.ReplaceAllString(s, "${1}<account>")
	return accountJSONPattern.ReplaceAllString(s, `${1}<account>"`)
}

func Redact(s string) string {
	s = codePattern.ReplaceAllString(s, "wft1-[redacted]")
	return tokenPattern.ReplaceAllString(s, "${1}[token]")
}

func Build(in Input) []byte {
	var b bytes.Buffer
	section := func(title string) { fmt.Fprintf(&b, "\n===== %s =====\n", title) }
	fmt.Fprintf(&b, "WFT Companion logs, %s\n", in.Now.Format("2006-01-02 15:04:05 -0700"))
	fmt.Fprintf(&b, "app version: %s\nsystem: %s/%s\nprogram: %s\napp folder: %s\n", in.Version, in.OS, in.Arch, in.Exe, in.ConfigDir)

	section("Settings")
	for _, line := range in.Settings {
		fmt.Fprintln(&b, line)
	}

	section("WoW folders checked")
	for _, c := range in.Candidates {
		mark := "  "
		if c.Found {
			mark = "OK"
		}
		fmt.Fprintf(&b, "%s %s\n", mark, c.Path)
	}

	section("Status")
	fmt.Fprintln(&b, in.Status)

	section("Addon data (SavedVariables)")
	if len(in.Files) == 0 {
		fmt.Fprintln(&b, "no WoWForeverGuildChronicle.lua found")
	}
	for _, f := range in.Files {
		fmt.Fprintf(&b, "\n--- %s\nsize %d bytes, modified %s\n", f.Path, f.Size, f.Modified.Format(time.RFC3339))
		if f.Err != "" {
			fmt.Fprintf(&b, "read error: %s\n", f.Err)
		}
		if f.Addon == nil {
			continue
		}
		a := f.Addon
		counts := a.CountByType()
		var parts []string
		for _, t := range savedvars.Types {
			parts = append(parts, fmt.Sprintf("%s=%d", t, counts[t]))
		}
		fmt.Fprintf(&b, "records: %s (skipped %d)\n", strings.Join(parts, " "), a.Skipped)
		var switches []string
		for _, t := range savedvars.Types {
			if on, ok := a.Enabled[t]; ok {
				switches = append(switches, fmt.Sprintf("%s=%v", t, on))
			}
		}
		fmt.Fprintf(&b, "in-game switches: %s\n", strings.Join(switches, " "))
		fmt.Fprintf(&b, "addon version %s, client %s %s\n", a.Diag.Version, a.Diag.Build, a.Diag.Locale)
		fmt.Fprintf(&b, "missing on this client: %s\n", strings.Join(a.Diag.Missing, ", "))
		var events []string
		for name, n := range a.Diag.Events {
			events = append(events, fmt.Sprintf("%s=%d", name, n))
		}
		sort.Strings(events)
		fmt.Fprintf(&b, "events last session: %s\n", strings.Join(events, " "))
		fmt.Fprintf(&b, "addon log (%d lines):\n", len(a.Diag.Log))
		for _, line := range a.Diag.Log {
			fmt.Fprintf(&b, "  %s\n", line)
		}
		writeProbe(&b, a.Probe)
	}

	section("App log")
	b.Write(tail(in.AppLogPaths, MaxAppLog))
	return []byte(Redact(RedactPaths(b.String(), in.Homes)))
}

const (
	ProbeBegin = "----- probe sessions JSON begin -----"
	ProbeEnd   = "----- probe sessions JSON end -----"
)

func writeProbe(b *bytes.Buffer, sessions []savedvars.Probe) {
	fmt.Fprintf(b, "WoW Forever probe: %d sessions (diagnostics, never uploaded)\n", len(sessions))
	if len(sessions) == 0 {
		return
	}
	s := sessions[len(sessions)-1].Summary()
	fmt.Fprintf(b, "latest: client %s, interface %d, %d functions missing, %d events unknown, %d blocked actions, "+
		"%d Lua errors, combat log %s\n", s.Client, s.Interface, s.APIsMissing, s.EventsUnknown, s.Blocked, s.Errors, s.CombatLog)
	for _, u := range s.Unsupported {
		fmt.Fprintf(b, "  limited: %s\n", u)
	}
	all, _ := json.MarshalIndent(sessions, "", " ")
	fmt.Fprintf(b, "%s\n%s\n%s\n", ProbeBegin, all, ProbeEnd)
}

func tail(paths []string, max int) []byte {
	var all []byte
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			all = append(all, b...)
		}
	}
	if len(all) > max {
		all = append([]byte("[… earlier lines left out]\n"), all[len(all)-max:]...)
	}
	return all
}

func FileName(t time.Time) string {
	return "WFT Companion logs " + t.Format("2006-01-02 1504") + ".txt"
}

func Save(dir string, now time.Time, content []byte) (string, error) {
	base := strings.TrimSuffix(FileName(now), ".txt")
	for i := 1; i < 100; i++ {
		name := base + ".txt"
		if i > 1 {
			name = fmt.Sprintf("%s (%d).txt", base, i)
		}
		p := filepath.Join(dir, name)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := io.Copy(f, bytes.NewReader(content))
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return p, werr
	}
	return "", errors.New("too many logs files with the same name")
}
