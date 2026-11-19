package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wizardofsalem/wft-companion/app/secret"
)

const DefaultAPIBase = "https://wowforevertracker.com"

type Config struct {
	APIBase        string          `json:"apiBase,omitempty"`
	UploadCode     string          `json:"uploadCode,omitempty"`
	WoWDir         string          `json:"wowDir,omitempty"`
	Disabled       map[string]bool `json:"disabled,omitempty"`
	Autostart      bool            `json:"autostart"`
	Consent        *Consent        `json:"consent,omitempty"`
	UpdateAskFirst bool            `json:"updateAskFirst,omitempty"`
	UpdateBase     string          `json:"updateBase,omitempty"`
	UpdateChannel  string          `json:"updateChannel,omitempty"`
	Device         string          `json:"device,omitempty"`
	DeviceLabel    string          `json:"deviceLabel,omitempty"`
	SignedIn       bool            `json:"signedIn,omitempty"`
}

const ConsentVersion = 1

type Consent struct {
	Version         int              `json:"version"`
	At              int64            `json:"at"`
	IncludeExisting bool             `json:"includeExisting"`
	Known           []string         `json:"known"`
	Since           map[string]int64 `json:"since,omitempty"`
	NewAt           map[string]int64 `json:"newAt,omitempty"`
}

func (c *Consent) KnowsCategory(typ string) bool {
	for _, k := range c.Known {
		if k == typ {
			return true
		}
	}
	return false
}

func (c Config) Base() string {
	if c.APIBase == "" {
		return DefaultAPIBase
	}
	return strings.TrimRight(c.APIBase, "/")
}

func (c Config) Ready() bool {
	_, err := ParseCode(c.UploadCode)
	return err == nil && c.WoWDir != ""
}

type Code struct {
	Region string
	Key    string
}

var (
	codePattern    = regexp.MustCompile(`^([a-z]{2})-(wft1-[a-z2-7]{32})$`)
	oldCodePattern = regexp.MustCompile(`^[a-z]{2}-[0-9]+-wft1-`)
)

func ParseCode(s string) (Code, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return Code{}, errors.New("no upload code yet")
	}
	m := codePattern.FindStringSubmatch(s)
	if m == nil {
		switch {
		case strings.HasPrefix(s, "wft1-"):
			return Code{}, errors.New("copy the whole upload code from the website, including the region in front (like eu-)")
		case oldCodePattern.MatchString(s):
			return Code{}, errors.New("that is an old-style guild code: get a new upload code from the website (it looks like eu-wft1-…)")
		}
		return Code{}, errors.New("that doesn't look like an upload code (it looks like eu-wft1-…)")
	}
	return Code{Region: m[1], Key: m[2]}, nil
}

func (c Code) String() string {
	return c.Region + "-" + c.Key
}

func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "WFT Companion"), nil
}

func Load(dir string) (Config, error) {
	var c Config
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("config.json: %w", err)
	}
	if c.UploadCode != "" {
		if err := Save(dir, c); err != nil {
			return c, fmt.Errorf("moving the upload code to the secret store: %w", err)
		}
		slog.Info("config: upload code moved out of config.json into the secret store")
		return c, nil
	}
	code, err := secret.Load(dir)
	if err != nil {
		slog.Warn("config: saved upload code unreadable here; it must be pasted again", "err", err)
		return c, nil
	}
	c.UploadCode = code
	return c, nil
}

func Save(dir string, c Config) error {
	code := c.UploadCode
	c.UploadCode = ""
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if current, _ := secret.Load(dir); current != code {
		if err := secret.Save(dir, code); err != nil {
			return err
		}
	}
	return WriteFileAtomic(filepath.Join(dir, "config.json"), b)
}

func WriteFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
