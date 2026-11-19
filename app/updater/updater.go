package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wizardofsalem/wft-companion/app/updatestate"
)

var baseURL = "https://wowforevertracker.com/downloads/companion"

func DefaultBase() string { return baseURL }

var (
	FirstCheck = 2 * time.Minute
	Every      = 6 * time.Hour
)

const (
	maxManifest = 256 << 10
	maxDownload = 128 << 20
)

const (
	fSequence = "sequence"
	fChannel  = "channel"
	fStaged   = "staged"
	fBad      = "bad"
	fPending  = "pending"
	fCurrent  = "current"
	fPrev     = "prev"
	fFailed   = "lastFailure"
)

type Staged struct {
	Version string `json:"version"`
	Path    string `json:"path"`
}

type Status struct {
	Channel     string    `json:"channel"`
	Running     string    `json:"running"`
	Available   string    `json:"available,omitempty"`
	Notes       string    `json:"notes,omitempty"`
	AskFirst    bool      `json:"askFirst"`
	LastCheck   time.Time `json:"lastCheck,omitzero"`
	LastError   string    `json:"lastError,omitempty"`
	Staged      string    `json:"staged,omitempty"`
	LastFailure string    `json:"lastFailure,omitempty"`
	Signed      bool      `json:"signed"`
}

type Updater struct {
	Dir      string
	Base     string
	Channel  string
	Version  string
	Client   *http.Client
	Now      func() time.Time
	AskFirst func() bool
	OnStaged func()

	mu       sync.Mutex
	st       Status
	manifest *Manifest
	kick     chan struct{}
}

func DefaultChannel() string {
	if SignedRelease() {
		return "stable"
	}
	return "beta"
}

func (u *Updater) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	st := u.st
	st.Channel, st.Running, st.Signed = u.Channel, u.Version, SignedRelease()
	if u.AskFirst != nil {
		st.AskFirst = u.AskFirst()
	}
	var failed string
	if updatestate.Get(u.Dir, fFailed, &failed) {
		st.LastFailure = failed
	}
	var staged Staged
	if updatestate.Get(u.Dir, fStaged, &staged) {
		st.Staged = staged.Version
	}
	return st
}

func (u *Updater) CheckNow() {
	u.mu.Lock()
	if u.kick == nil {
		u.kick = make(chan struct{}, 1)
	}
	k := u.kick
	u.mu.Unlock()
	select {
	case k <- struct{}{}:
	default:
	}
}

func (u *Updater) Loop(ctx context.Context) {
	u.mu.Lock()
	if u.kick == nil {
		u.kick = make(chan struct{}, 1)
	}
	k := u.kick
	u.mu.Unlock()
	u.cleanDownloads()
	wait := FirstCheck
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		case <-k:
		}
		if err := u.Check(ctx); err != nil && !errors.Is(err, errNoUpdate) {
			slog.Warn("update: check failed", "err", err)
		}
		wait = Every + time.Duration((rand.Float64()*2-1)*float64(time.Hour))
	}
}

func (u *Updater) Check(ctx context.Context) error {
	m, err := u.fetch(ctx)
	u.mu.Lock()
	u.st.LastCheck = u.now()
	u.st.LastError = ""
	if errors.Is(err, errNoUpdate) {
		slog.Info("update: nothing to install", "why", err.Error())
		u.st.Available, u.st.Notes = "", ""
		u.mu.Unlock()
		return err
	}
	if err != nil {
		u.st.LastError = err.Error()
		u.mu.Unlock()
		return err
	}
	var seq int64
	var seqChannel string
	updatestate.Get(u.Dir, fSequence, &seq)
	updatestate.Get(u.Dir, fChannel, &seqChannel)
	if seqChannel != u.Channel {
		seq = 0
	}
	var bad []string
	updatestate.Get(u.Dir, fBad, &bad)
	err = decide(m, u.Channel, u.Version, seq, bad, u.now())
	if err == nil || errors.Is(err, errNoUpdate) {
		if m.Channel == u.Channel && m.Sequence >= seq {
			updatestate.Set(u.Dir, map[string]any{fSequence: m.Sequence, fChannel: u.Channel})
		}
	}
	if err != nil {
		if errors.Is(err, errNoUpdate) {
			slog.Info("update: nothing to install", "why", err.Error())
			if strings.Contains(err.Error(), "expired") {
				u.st.LastError = "No update now: " + err.Error()
			}
			u.st.Available, u.st.Notes = "", ""
		} else {
			u.st.LastError = err.Error()
			slog.Warn("update: manifest refused", "err", err)
		}
		u.mu.Unlock()
		return err
	}
	u.st.Available, u.st.Notes = m.Version, m.Notes
	u.manifest = &m
	ask := u.AskFirst != nil && u.AskFirst()
	u.mu.Unlock()
	slog.Info("update: available", "version", m.Version, "askFirst", ask)
	if ask {
		return nil
	}
	return u.Install(ctx)
}

func (u *Updater) Install(ctx context.Context) error {
	u.mu.Lock()
	m := u.manifest
	u.mu.Unlock()
	if m == nil {
		return errors.New("no update to install: check first")
	}
	path, err := u.download(ctx, *m)
	if err == nil {
		err = checkSignature(path)
		if err != nil {
			os.Remove(path)
		}
	}
	if err != nil {
		u.mu.Lock()
		u.st.LastError = "Update " + m.Version + " not installed: " + err.Error()
		u.mu.Unlock()
		slog.Warn("update: download refused", "version", m.Version, "err", err)
		return err
	}
	if err := updatestate.Set(u.Dir, map[string]any{fStaged: Staged{Version: m.Version, Path: path}}); err != nil {
		return err
	}
	slog.Info("update: verified and staged; handing over to the supervisor", "version", m.Version)
	if u.OnStaged != nil {
		u.OnStaged()
	}
	return nil
}

func (u *Updater) client() *http.Client {
	c := http.Client{Timeout: 5 * time.Minute}
	if u.Client != nil {
		c = *u.Client
	}
	base, _ := url.Parse(u.Base)
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if base == nil || req.URL.Host != base.Host || req.URL.Scheme != base.Scheme {
			return errors.New("redirect to another host refused")
		}
		if len(via) > 5 {
			return errors.New("too many redirects")
		}
		return nil
	}
	return &c
}

func (u *Updater) allowedURL(raw string) error {
	base, err := url.Parse(u.Base)
	if err != nil {
		return err
	}
	p, err := url.Parse(raw)
	if err != nil {
		return err
	}
	local := p.Hostname() == "127.0.0.1" || p.Hostname() == "localhost"
	switch {
	case p.Host != base.Host:
		return fmt.Errorf("download host %s is not the release host", p.Host)
	case p.Scheme != "https" && !(p.Scheme == "http" && local):
		return errors.New("download is not HTTPS")
	}
	return nil
}

var errNoManifest = fmt.Errorf("%w: no manifest published for this channel", errNoUpdate)

func looksLikeManifest(body []byte, contentType string) bool {
	if strings.HasPrefix(strings.ToLower(contentType), "text/html") {
		return false
	}
	t := strings.TrimSpace(string(body))
	return strings.HasPrefix(t, "{")
}

func looksLikeSignature(sig []byte, contentType string) bool {
	if strings.HasPrefix(strings.ToLower(contentType), "text/html") {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	return err == nil && len(raw) == ed25519.SignatureSize
}

func (u *Updater) get(ctx context.Context, raw string, limit int64) ([]byte, error) {
	b, _, err := u.getTyped(ctx, raw, limit)
	return b, err
}

func (u *Updater) getTyped(ctx context.Context, raw string, limit int64) ([]byte, string, error) {
	if err := u.allowedURL(raw); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "WFTCompanion/"+u.Version)
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return nil, "", errNoManifest
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s: HTTP %d", raw, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > limit {
		return nil, "", fmt.Errorf("%s: larger than %d bytes", raw, limit)
	}
	return b, resp.Header.Get("Content-Type"), nil
}

func (u *Updater) fetch(ctx context.Context) (Manifest, error) {
	base := strings.TrimRight(u.Base, "/") + "/" + u.Channel + ".json"
	body, ct, err := u.getTyped(ctx, base, maxManifest)
	if err != nil {
		return Manifest{}, err
	}
	if !looksLikeManifest(body, ct) {
		return Manifest{}, errNoManifest
	}
	sig, sct, err := u.getTyped(ctx, base+".sig", 1<<10)
	if err != nil {
		return Manifest{}, err
	}
	if !looksLikeSignature(sig, sct) {
		return Manifest{}, errNoManifest
	}
	return verifyManifest(body, sig)
}

func (u *Updater) downloadDir() string { return filepath.Join(u.Dir, "update") }

func (u *Updater) download(ctx context.Context, m Manifest) (string, error) {
	f := m.Files[Platform]
	if f.Size <= 0 || f.Size > maxDownload {
		return "", fmt.Errorf("download size %d out of range", f.Size)
	}
	body, err := u.get(ctx, f.URL, f.Size)
	if err != nil {
		return "", err
	}
	if int64(len(body)) != f.Size {
		return "", fmt.Errorf("download is %d bytes, the manifest says %d", len(body), f.Size)
	}
	sum := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), f.SHA256) {
		return "", errors.New("download's SHA-256 doesn't match the manifest")
	}
	if err := os.MkdirAll(u.downloadDir(), 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(u.downloadDir(), "WFTCompanion-"+m.Version+".exe")
	tmp := path + ".part"
	if err := os.WriteFile(tmp, body, 0o755); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

func (u *Updater) cleanDownloads() {
	var staged Staged
	updatestate.Get(u.Dir, fStaged, &staged)
	files, _ := filepath.Glob(filepath.Join(u.downloadDir(), "*"))
	for _, f := range files {
		if f != staged.Path {
			os.Remove(f)
		}
	}
}

func checkSignature(path string) error {
	if !SignedRelease() {
		return nil
	}
	if publisher == "" {
		return errors.New("this build has no pinned publisher")
	}
	return verifyAuthenticode(path, publisher)
}
