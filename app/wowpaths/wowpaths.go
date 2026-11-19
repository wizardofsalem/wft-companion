package wowpaths

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/wizardofsalem/wft-companion/app/hidden"
	"github.com/wizardofsalem/wft-companion/app/syspath"
)

const ClassicFolder = "_classic_era_"

var Flavor = "era"

func GameFolders() []string {
	switch Flavor {
	case "tbc":
		return []string{"_anniversary_", "_classic_"}
	case "retail":
		return []string{"_retail_"}
	case "forever":
		return []string{"_forever_", "_retail_"}
	}
	return []string{ClassicFolder}
}

func GameName() string {
	switch Flavor {
	case "tbc":
		return "WoW Classic (TBC)"
	case "retail":
		return "World of Warcraft (retail)"
	case "forever":
		return "WoW Forever"
	}
	return "WoW Classic Era"
}

func TocInterface() string {
	switch Flavor {
	case "tbc":
		return "20504, 20505"
	case "retail":
		return "120105, 120100, 120007, 120005, 120001, 120000"
	}
	return ""
}

const AddonName = "WoWForeverGuildChronicle"

type Env struct {
	GOOS     string
	Home     string
	Getenv   func(string) string
	Stat     func(string) (os.FileInfo, error)
	RegQuery func(key string) (string, error)
	ReadDir  func(dir string) ([]string, error)
}

func System() Env {
	home, _ := os.UserHomeDir()
	return Env{
		GOOS:   runtime.GOOS,
		Home:   home,
		Getenv: os.Getenv,
		Stat:   os.Stat,
		RegQuery: func(key string) (string, error) {
			out, err := hidden.Command(syspath.System32("reg.exe"), "query", key, "/v", "InstallPath").Output()
			return string(out), err
		},
		ReadDir: func(dir string) ([]string, error) {
			entries, err := os.ReadDir(dir)
			var names []string
			for _, en := range entries {
				if en.IsDir() {
					names = append(names, en.Name())
				}
			}
			return names, err
		},
	}
}

var registryKeys = []string{
	`HKLM\SOFTWARE\WOW6432Node\Blizzard Entertainment\World of Warcraft`,
	`HKLM\SOFTWARE\Blizzard Entertainment\World of Warcraft`,
}

func (e Env) join(parts ...string) string {
	if e.GOOS == "windows" {
		for i := range parts {
			if i > 0 {
				parts[i] = strings.Trim(parts[i], `\`)
			}
		}
		return strings.TrimRight(parts[0], `\`) + `\` + strings.Join(parts[1:], `\`)
	}
	return filepath.Join(parts...)
}

func Candidates(e Env) []string {
	var roots []string
	switch e.GOOS {
	case "windows":
		if e.RegQuery != nil {
			for _, key := range registryKeys {
				out, err := e.RegQuery(key)
				if err != nil {
					slog.Info("detect: registry key not found", "key", key)
					continue
				}
				if root := wowRootFromRegistry(out); root != "" {
					slog.Info("detect: registry install path", "key", key, "root", root)
					roots = append(roots, root)
				} else {
					slog.Info("detect: registry key has no InstallPath", "key", key)
				}
			}
		}
		for _, v := range []string{"ProgramFiles(x86)", "ProgramFiles", "ProgramW6432"} {
			if dir := e.Getenv(v); dir != "" {
				roots = append(roots, e.join(dir, "World of Warcraft"))
			}
		}
		for _, drive := range "CDEFGH" {
			d := string(drive) + `:\`
			for _, sub := range []string{"Program Files (x86)", "Program Files", "", "Games", "Blizzard", "Battle.net"} {
				if sub == "" {
					roots = append(roots, d+"World of Warcraft")
				} else {
					roots = append(roots, e.join(d, sub, "World of Warcraft"))
				}
			}
		}
	case "darwin":
		roots = append(roots, "/Applications/World of Warcraft")
		if e.Home != "" {
			roots = append(roots, filepath.Join(e.Home, "Applications", "World of Warcraft"))
		}
		roots = append(roots, "/Users/Shared/World of Warcraft", "/Applications/Games/World of Warcraft")
	default:
		if e.Home != "" {
			for _, prefix := range []string{
				filepath.Join(e.Home, "Games", "world-of-warcraft"),
				filepath.Join(e.Home, "Games", "battlenet"),
				filepath.Join(e.Home, ".wine"),
			} {
				roots = append(roots, filepath.Join(prefix, "drive_c", "Program Files (x86)", "World of Warcraft"))
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range roots {
		for _, folder := range GameFolders() {
			dir := e.join(r, folder)
			key := dir
			if e.GOOS == "windows" || e.GOOS == "darwin" {
				key = strings.ToLower(dir)
			}
			if !seen[key] {
				seen[key] = true
				out = append(out, dir)
			}
		}
	}
	return out
}

func wowRootFromRegistry(out string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.EqualFold(fields[0], "InstallPath") || !strings.HasPrefix(fields[1], "REG_") {
			continue
		}
		i := strings.Index(line, fields[1]) + len(fields[1])
		path := strings.TrimRight(strings.TrimSpace(line[i:]), `\`)
		if path == "" {
			return ""
		}
		if last := path[strings.LastIndex(path, `\`)+1:]; strings.HasPrefix(last, "_") && strings.HasSuffix(last, "_") {
			path = path[:strings.LastIndex(path, `\`)]
		}
		return path
	}
	return ""
}

func Detect(e Env) []string {
	var found []string
	candidates := Candidates(e)
	for _, dir := range candidates {
		ok := IsClassicDir(e, dir)
		slog.Info("detect: candidate", "path", dir, "found", ok)
		if ok {
			found = append(found, dir)
		}
	}
	slog.Info("detect: done", "checked", len(candidates), "found", len(found))
	return found
}

var clientExes = []string{"WowClassic.exe", "WowClassicT.exe", "WowClassicB.exe", "Wow.exe", "World of Warcraft Classic.app"}

func IsClassicDir(e Env, dir string) bool {
	isDir := func(p string) bool { info, err := e.Stat(p); return err == nil && info.IsDir() }
	for _, sub := range []string{"WTF", "Interface"} {
		if isDir(e.join(dir, sub)) {
			return true
		}
	}
	for _, exe := range clientExes {
		if _, err := e.Stat(e.join(dir, exe)); err == nil {
			return true
		}
	}
	if isDir(dir) {
		base := dir
		if i := strings.LastIndexAny(dir, `\/`); i >= 0 {
			base = dir[i+1:]
		}
		for _, folder := range GameFolders() {
			if strings.EqualFold(base, folder) {
				return true
			}
		}
	}
	return false
}

func Normalize(e Env, dir string) string {
	dir = strings.Trim(strings.TrimSpace(dir), `"'`)
	if dir == "" {
		return ""
	}
	if e.GOOS != "windows" {
		dir = filepath.Clean(dir)
	} else {
		dir = strings.TrimRight(dir, `\/`)
	}
	parts := strings.FieldsFunc(dir, func(r rune) bool { return r == '\\' || r == '/' })
	for i := len(parts) - 1; i >= 0; i-- {
		for _, folder := range GameFolders() {
			if strings.EqualFold(parts[i], folder) && i < len(parts)-1 {
				cut := 0
				for j := 0; j <= i; j++ {
					cut = strings.Index(dir[cut:], parts[j]) + cut + len(parts[j])
				}
				dir = dir[:cut]
				i = -1
				break
			}
		}
	}
	base := dir
	if i := strings.LastIndexAny(dir, `\/`); i >= 0 {
		base = dir[i+1:]
	}
	for _, folder := range GameFolders() {
		if strings.EqualFold(base, folder) {
			return dir
		}
	}
	for _, folder := range GameFolders() {
		if c := e.join(dir, folder); IsClassicDir(e, c) {
			return c
		}
	}
	for i := len(parts) - 1; i >= 1; i-- {
		if (strings.EqualFold(parts[i], "Interface") || strings.EqualFold(parts[i], "WTF")) && !otherClients[strings.ToLower(parts[i-1])] {
			if c := prefix(dir, parts, i); IsClassicDir(e, c) {
				return c
			}
		}
	}
	if e.ReadDir != nil {
		names, _ := e.ReadDir(dir)
		var found []string
		for _, name := range names {
			if otherClients[strings.ToLower(name)] {
				continue
			}
			c := e.join(dir, name)
			for _, sub := range []string{"WTF", "Interface"} {
				if info, err := e.Stat(e.join(c, sub)); err == nil && info.IsDir() {
					found = append(found, c)
					break
				}
			}
		}
		if len(found) == 1 {
			return found[0]
		}
	}
	return dir
}

var otherClients = map[string]bool{
	"_classic_era_": true, "_classic_": true, "_anniversary_": true, "_classic_era_ptr_": true,
	"_classic_ptr_": true, "_classic_beta_": true,
}

func prefix(dir string, parts []string, i int) string {
	cut := 0
	for j := 0; j < i; j++ {
		cut = strings.Index(dir[cut:], parts[j]) + cut + len(parts[j])
	}
	return dir[:cut]
}

func SavedVariablesFiles(classicDir string) ([]string, error) {
	accounts := filepath.Join(classicDir, "WTF", "Account")
	entries, err := os.ReadDir(accounts)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, a := range entries {
		if !a.IsDir() {
			continue
		}
		f := filepath.Join(accounts, a.Name(), "SavedVariables", AddonName+".lua")
		if info, err := os.Stat(f); err == nil && info.Mode().IsRegular() {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return files, nil
}

func AddonInstalled(classicDir string) bool {
	_, err := os.Stat(filepath.Join(classicDir, "Interface", "AddOns", AddonName, AddonName+".toc"))
	return err == nil
}
