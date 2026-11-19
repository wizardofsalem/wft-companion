package diag

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type DesktopEnv struct {
	GOOS        string
	Home        string
	Getenv      func(string) string
	Stat        func(string) (os.FileInfo, error)
	KnownFolder func() (string, error)
	RegQuery    func(key, value string) (string, error)
}

func SystemDesktop() DesktopEnv {
	home, _ := os.UserHomeDir()
	return DesktopEnv{GOOS: runtime.GOOS, Home: home, Getenv: os.Getenv, Stat: os.Stat,
		KnownFolder: knownDesktop, RegQuery: regQuery}
}

const shellFolders = `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`

func (e DesktopEnv) isDir(p string) bool {
	info, err := e.Stat(p)
	return err == nil && info.IsDir()
}

func (e DesktopEnv) join(parts ...string) string {
	if e.GOOS == "windows" {
		return strings.Join(parts, `\`)
	}
	return filepath.Join(parts...)
}

func Desktop(e DesktopEnv) (dir, how string, err error) {
	if e.GOOS == "windows" {
		if e.KnownFolder != nil {
			if d, err := e.KnownFolder(); err == nil && d != "" && e.isDir(d) {
				return d, "known folder", nil
			}
		}
		if e.RegQuery != nil {
			if out, err := e.RegQuery(shellFolders, "Desktop"); err == nil {
				if d := expandWindows(regValue(out, "Desktop"), e.Getenv); d != "" && e.isDir(d) {
					return d, "registry", nil
				}
			}
		}
		profile := e.Getenv("USERPROFILE")
		if profile != "" {
			for _, d := range []string{e.join(profile, "OneDrive", "Desktop"), e.join(profile, "Desktop")} {
				if e.isDir(d) {
					return d, "profile", nil
				}
			}
		}
		return "", "", errors.New("no Desktop folder found")
	}
	if e.Home != "" {
		if d := filepath.Join(e.Home, "Desktop"); e.isDir(d) {
			return d, "home", nil
		}
	}
	return "", "", errors.New("no Desktop folder found")
}

func regValue(out, name string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.EqualFold(fields[0], name) || !strings.HasPrefix(fields[1], "REG_") {
			continue
		}
		i := strings.Index(line, fields[1]) + len(fields[1])
		return strings.TrimSpace(line[i:])
	}
	return ""
}

func expandWindows(s string, getenv func(string) string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '%')
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := strings.IndexByte(s[i+1:], '%')
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		name := s[i+1 : i+1+j]
		if v := getenv(name); v != "" {
			b.WriteString(v)
		} else {
			b.WriteString("%" + name + "%")
		}
		s = s[i+2+j:]
	}
}
