package heartbeat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/wizardofsalem/wft-companion/app/config"
)

const FileName = "heartbeat.json"

const Interval = 30 * time.Second

type Beat struct {
	PID     int       `json:"pid"`
	Version string    `json:"version"`
	At      time.Time `json:"at"`
	CheckAt time.Time `json:"checkAt,omitzero"`
}

func Path(dir string) string { return filepath.Join(dir, FileName) }

func Write(dir string, b Beat) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(Path(dir), data)
}

func Read(dir string) (Beat, error) {
	var b Beat
	data, err := os.ReadFile(Path(dir))
	if err != nil {
		return b, err
	}
	return b, json.Unmarshal(data, &b)
}
