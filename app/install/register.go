package install

import (
	"fmt"
	"os"
	"path/filepath"
)

const UninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\WFTCompanion`

type Entry struct {
	DisplayName     string
	DisplayVersion  string
	Publisher       string
	InstallLocation string
	DisplayIcon     string
	UninstallString string
	EstimatedSizeKB uint32
}

var Publisher = "WoW Forever Tracker"

func AppEntry(exe, version string) Entry {
	e := Entry{
		DisplayName:     "WFT Companion",
		DisplayVersion:  version,
		Publisher:       Publisher,
		InstallLocation: filepath.Dir(exe),
		DisplayIcon:     exe + ",0",
		UninstallString: fmt.Sprintf(`"%s" --uninstall`, exe),
	}
	if info, err := os.Stat(exe); err == nil {
		e.EstimatedSizeKB = uint32((info.Size() + 1023) / 1024)
	}
	return e
}
