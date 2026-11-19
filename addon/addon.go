package addon

import (
	"bufio"
	"embed"
	"io/fs"
	"strings"
)

//go:embed WoWForeverGuildChronicle
var embedded embed.FS

const Name = "WoWForeverGuildChronicle"

func Files() fs.FS {
	sub, err := fs.Sub(embedded, Name)
	if err != nil {
		panic(err)
	}
	return sub
}

func Version() string {
	f, err := Files().Open(Name + ".toc")
	if err != nil {
		return ""
	}
	defer f.Close()
	return TocVersion(f)
}

func tocLine(l string) string { return strings.TrimSpace(strings.TrimPrefix(l, "\ufeff")) }

func TocSource(toc []byte) string {
	sc := bufio.NewScanner(strings.NewReader(string(toc)))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(tocLine(sc.Text()), "## X-WFT-Source:"); ok {
			if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
				return v
			}
		}
	}
	return "app"
}

func TocVersion(r interface{ Read([]byte) (int, error) }) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(tocLine(sc.Text()), "## Version:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
