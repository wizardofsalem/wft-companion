package updater

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	keyCurrent    = "r9XPXtTKvAIitoLuCGvMduUPe/CGUkichifgN3aTNzk="
	keyNext       = ""
	signedRelease = ""
	publisher     = ""
)

const Platform = "windows-amd64"

type Manifest struct {
	Schema      int             `json:"schema"`
	Channel     string          `json:"channel"`
	Sequence    int64           `json:"sequence"`
	Expires     time.Time       `json:"expires"`
	Version     string          `json:"version"`
	Released    time.Time       `json:"released"`
	MinSafe     string          `json:"minSafe,omitempty"`
	Notes       string          `json:"notes"`
	Files       map[string]File `json:"files"`
	RevokedKeys []string        `json:"revokedKeys"`
}

type File struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func SignedRelease() bool { return signedRelease == "true" }

func keys() []string {
	var out []string
	for _, k := range []string{keyCurrent, keyNext} {
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}

var errNoUpdate = errors.New("no update")

func verifyManifest(body, sig []byte) (Manifest, error) {
	var m Manifest
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return m, errors.New("manifest signature unreadable")
	}
	signer := ""
	for _, k := range keys() {
		pub, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(ed25519.PublicKey(pub), body, raw) {
			signer = k
			break
		}
	}
	if signer == "" {
		return m, errors.New("manifest signature does not verify with this app's update keys")
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return m, fmt.Errorf("manifest: %w", err)
	}
	for _, r := range m.RevokedKeys {
		if r == signer {
			return m, errors.New("manifest signed with a revoked update key")
		}
	}
	if m.Schema != 1 {
		return m, fmt.Errorf("manifest schema %d not understood", m.Schema)
	}
	return m, nil
}

func decide(m Manifest, channel, running string, lastSequence int64, bad []string, now time.Time) error {
	switch {
	case m.Channel != channel:
		return fmt.Errorf("manifest is for %q, this app follows %q", m.Channel, channel)
	case m.Sequence < lastSequence:
		return fmt.Errorf("manifest older than one already seen (%d < %d): refused", m.Sequence, lastSequence)
	case !now.Before(m.Expires):
		return fmt.Errorf("%w: the manifest expired %s (or this computer's clock is wrong)", errNoUpdate, m.Expires.Format(time.RFC3339))
	case !Newer(m.Version, running):
		return fmt.Errorf("%w: %s is the latest", errNoUpdate, running)
	}
	for _, b := range bad {
		if b == m.Version {
			return fmt.Errorf("%w: %s failed here before", errNoUpdate, m.Version)
		}
	}
	if _, ok := m.Files[Platform]; !ok {
		return fmt.Errorf("%w: no %s download in %s", errNoUpdate, Platform, m.Version)
	}
	return nil
}

func Newer(a, b string) bool { return compare(a, b) > 0 }

func compare(a, b string) int {
	an, ap, _ := strings.Cut(strings.TrimPrefix(a, "v"), "-")
	bn, bp, _ := strings.Cut(strings.TrimPrefix(b, "v"), "-")
	if c := compareDots(an, bn, true); c != 0 {
		return c
	}
	switch {
	case ap == "" && bp == "":
		return 0
	case ap == "":
		return 1
	case bp == "":
		return -1
	}
	return compareDots(ap, bp, false)
}

func compareDots(a, b string, numericOnly bool) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(as), len(bs)); i++ {
		if i >= len(as) {
			return -1
		}
		if i >= len(bs) {
			return 1
		}
		x, xerr := strconv.Atoi(as[i])
		y, yerr := strconv.Atoi(bs[i])
		switch {
		case xerr == nil && yerr == nil:
			if x != y {
				if x > y {
					return 1
				}
				return -1
			}
		case numericOnly:
			if xerr != nil && yerr == nil {
				return -1
			}
			if yerr != nil && xerr == nil {
				return 1
			}
		default:
			if xerr == nil {
				return -1
			}
			if yerr == nil {
				return 1
			}
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	return 0
}
