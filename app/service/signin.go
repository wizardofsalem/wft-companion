package service

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wizardofsalem/wft-companion/app/auth"
	"github.com/wizardofsalem/wft-companion/app/config"
	"github.com/wizardofsalem/wft-companion/app/savedvars"
	"github.com/wizardofsalem/wft-companion/app/uploader"
	"github.com/wizardofsalem/wft-companion/app/wowpaths"
)

const (
	AccountNone        = "none"
	AccountSigningIn   = "signingIn"
	AccountLinked      = "linked"
	AccountSignInAgain = "signInAgain"
	AccountOlderCode   = "olderCode"
)

type Account struct {
	State     string     `json:"state"`
	UserCode  string     `json:"userCode,omitempty"`
	VerifyURL string     `json:"verifyUrl,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Message   string     `json:"message,omitempty"`
	Label     string     `json:"label,omitempty"`
	Region    string     `json:"region,omitempty"`
	LinkedAt  *time.Time `json:"linkedAt,omitempty"`
}

var ErrRegionNeeded = errors.New("Choose where you play.")

type signing struct {
	cancel    context.CancelFunc
	region    string
	userCode  string
	verifyURL string
	expiresAt time.Time
}

const signInWait = 20 * time.Minute

func (s *Service) StartSignIn(region string) error {
	s.mu.Lock()
	if s.signing != nil {
		s.mu.Unlock()
		return nil
	}
	if s.cfg.Consent == nil {
		s.mu.Unlock()
		return errors.New("Finish setting up first.")
	}
	cfg := s.cfg
	s.mu.Unlock()
	region = strings.ToLower(strings.TrimSpace(region))
	if region != "" && region != "eu" && region != "us" {
		return errors.New("Choose Europe or Americas.")
	}
	if region == "" {
		region = s.detectRegion(cfg)
	}
	if region == "" {
		return ErrRegionNeeded
	}
	s.mu.Lock()
	if s.signing != nil {
		s.mu.Unlock()
		return nil
	}
	if s.cfg.Device == "" {
		c := s.cfg
		c.Device = newDeviceID()
		if c.DeviceLabel == "" {
			c.DeviceLabel = deviceLabel()
		}
		if err := config.Save(s.Dir, c); err != nil {
			s.mu.Unlock()
			return err
		}
		s.cfg.Device, s.cfg.DeviceLabel = c.Device, c.DeviceLabel
	}
	ctx, cancel := context.WithTimeout(context.Background(), signInWait)
	s.signing = &signing{cancel: cancel, region: region}
	s.signMsg = ""
	s.mu.Unlock()
	slog.Info("sign-in: started", "region", region)
	go s.runSignIn(ctx, region)
	return nil
}

func (s *Service) CancelSignIn() {
	s.mu.Lock()
	sg := s.signing
	s.signing = nil
	s.mu.Unlock()
	if sg != nil {
		sg.cancel()
		slog.Info("sign-in: cancelled")
	}
}

func (s *Service) runSignIn(ctx context.Context, region string) {
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("sign-in: panic", "panic", fmt.Sprint(r))
				err = errors.New("the sign-in failed")
			}
		}()
		return s.signIn(ctx, region)
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.signing == nil {
		return
	}
	s.signing.cancel()
	s.signing = nil
	switch {
	case err == nil:
		s.signMsg = ""
	case errors.Is(err, context.Canceled):
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, auth.ErrExpired):
		s.signMsg = "Sign-in timed out. Try again."
	case errors.Is(err, auth.ErrDenied):
		s.signMsg = "Sign-in was refused. Try again."
	case errors.Is(err, auth.ErrNotSetUp):
		s.signMsg = "Sign-in isn't available yet. Try again later."
	case errors.Is(err, auth.ErrUnreachable), errors.Is(err, uploader.ErrOffline):
		s.signMsg = "Can't connect. Check your internet and try again."
	default:
		var ae *uploader.AccountError
		if errors.As(err, &ae) {
			s.signMsg = ae.Line
		} else {
			s.signMsg = "Sign-in didn't work. Try again."
		}
	}
	if err != nil {
		slog.Warn("sign-in: ended", "err", err)
	}
}

func (s *Service) signIn(ctx context.Context, region string) error {
	s.mu.Lock()
	base, device, label := s.cfg.Base(), s.cfg.Device, s.cfg.DeviceLabel
	s.mu.Unlock()
	client := s.httpClient()
	acfg, err := auth.FetchConfig(ctx, client, base)
	if err != nil {
		return err
	}
	dev, err := auth.Start(ctx, client, acfg, s.Now())
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.signing == nil {
		s.mu.Unlock()
		return context.Canceled
	}
	s.signing.userCode, s.signing.verifyURL, s.signing.expiresAt = dev.UserCode, dev.VerifyURL, dev.ExpiresAt
	open := s.OpenURL
	s.mu.Unlock()
	if open != nil {
		open(dev.VerifyURL)
	}
	sleep := s.SignInSleep
	if sleep == nil {
		sleep = auth.Sleep
	}
	token, err := auth.Poll(ctx, client, acfg, dev, s.Now, sleep)
	if err != nil {
		return err
	}
	slog.Info("sign-in: confirmed in the browser")

	s.mu.Lock()
	old, signedIn := s.cfg.UploadCode, s.cfg.SignedIn
	refused := s.up != nil && s.up.Status().NeedsSignIn
	s.mu.Unlock()
	if code, perr := config.ParseCode(old); perr == nil && !signedIn && !refused && code.Region == region {
		err := uploader.AttachCode(ctx, client, base, token, code)
		if err == nil {
			slog.Info("sign-in: the older code joined the account")
			return s.keepCode(code)
		}
		if errors.Is(err, uploader.ErrOffline) {
			return err
		}
		slog.Info("sign-in: the older code can't join this account; asking for a new one", "err", err)
	}
	code, err := uploader.NewAppCode(ctx, client, base, token, region, label, device)
	if err != nil {
		return err
	}
	slog.Info("sign-in: done", "region", code.Region)
	if err := s.keepCode(code); err != nil {
		return err
	}
	if prev, perr := config.ParseCode(old); perr == nil && signedIn && prev.Region != code.Region {
		if err := uploader.RevokeCode(ctx, client, base, prev); err != nil {
			slog.Warn("sign-in: the old region's code wasn't removed", "region", prev.Region, "err", err)
		} else {
			slog.Info("sign-in: the old region's code was removed", "region", prev.Region)
		}
	}
	return nil
}

func (s *Service) keepCode(code config.Code) error {
	s.mu.Lock()
	c := s.cfg
	c.UploadCode, c.SignedIn = code.String(), true
	if err := config.Save(s.Dir, c); err != nil {
		s.mu.Unlock()
		return err
	}
	changed := s.cfg.UploadCode != c.UploadCode
	if prev, err := config.ParseCode(s.cfg.UploadCode); err == nil && prev.Region != code.Region {
		slog.Info("sign-in: the region changed", "from", prev.Region, "to", code.Region)
		s.sent = map[string]sentState{}
		s.saveSent()
	}
	s.cfg = c
	if changed || s.up == nil || s.up.Status().NeedsSignIn {
		s.rebuildUploader()
	}
	s.force = true
	s.invalidate()
	s.mu.Unlock()
	s.Kick()
	return nil
}

func (s *Service) SignOut() error {
	s.CancelSignIn()
	s.mu.Lock()
	old, base := s.cfg.UploadCode, s.cfg.Base()
	s.mu.Unlock()
	note := ""
	client := s.httpClient()
	if code, err := config.ParseCode(old); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := uploader.RevokeCode(ctx, client, base, code)
		cancel()
		if err != nil {
			note = "Signed out. You can also remove this computer on your account page."
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cfg
	c.UploadCode, c.SignedIn = "", false
	if err := config.Save(s.Dir, c); err != nil {
		return err
	}
	s.cfg = c
	s.up = nil
	s.signMsg = note
	s.invalidate()
	slog.Info("sign out: done")
	return nil
}

func (s *Service) accountLocked(st *uploader.Status) Account {
	a := Account{Message: s.signMsg, Label: s.cfg.DeviceLabel}
	code, err := config.ParseCode(s.cfg.UploadCode)
	if err == nil {
		a.Region = code.Region
	}
	switch {
	case s.signing != nil:
		a.State = AccountSigningIn
		a.Region = s.signing.region
		a.UserCode, a.VerifyURL = s.signing.userCode, s.signing.verifyURL
		if !s.signing.expiresAt.IsZero() {
			a.ExpiresAt = timePtr(s.signing.expiresAt)
		}
	case err != nil:
		a.State = AccountNone
	case st != nil && st.NeedsSignIn:
		a.State = AccountSignInAgain
	case !s.cfg.SignedIn:
		a.State = AccountOlderCode
	default:
		a.State = AccountLinked
	}
	if st != nil && st.Linked && !st.LinkedAt.IsZero() {
		a.LinkedAt = timePtr(st.LinkedAt)
	}
	return a
}

func (a Account) NeedsYou() bool {
	return a.State == AccountNone || a.State == AccountSignInAgain
}

func newDeviceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var labelClean = regexp.MustCompile(`[^A-Za-z0-9 ._-]+`)

func deviceLabel() string {
	h, _ := os.Hostname()
	h = strings.TrimSpace(labelClean.ReplaceAllString(h, ""))
	if len(h) > 40 {
		h = h[:40]
	}
	if h == "" {
		h = "My computer"
	}
	return h
}

func (s *Service) detectRegion(cfg config.Config) string {
	if cfg.WoWDir != "" {
		if r := s.recordsRegion(cfg.WoWDir); r != "" {
			slog.Info("sign-in: region from the addon's records", "region", r)
			return r
		}
		slog.Info("sign-in: the addon's records name no region")
	}
	if code, err := config.ParseCode(cfg.UploadCode); err == nil {
		slog.Info("sign-in: region from the code this computer holds", "region", code.Region)
		return code.Region
	}
	if cfg.WoWDir == "" {
		slog.Info("sign-in: no game folder; asking for the region")
		return ""
	}
	if r := portalRegion(filepath.Join(cfg.WoWDir, "WTF", "Config.wtf")); r != "" {
		slog.Info("sign-in: region from the game's settings", "region", r)
		return r
	}
	slog.Info("sign-in: no region found; asking for it")
	return ""
}

func (s *Service) recordsRegion(dir string) string {
	paths, err := wowpaths.SavedVariablesFiles(dir)
	if err != nil {
		return ""
	}
	region, at := "", int64(0)
	for _, p := range paths {
		s.mu.Lock()
		c := s.files[p]
		read := c != nil && c.meta != nil
		var rg string
		var rat int64
		if read {
			rg, rat = c.region, c.regionAt
		}
		s.mu.Unlock()
		if !read {
			a, err := savedvars.ReadFile(p)
			if err != nil {
				continue
			}
			for _, r := range a.Records {
				if g := recordRegion(r); g != "" && (rg == "" || r.At > rat) {
					rg, rat = g, r.At
				}
			}
		}
		if rg != "" && (region == "" || rat > at) {
			region, at = rg, rat
		}
	}
	return region
}

func (s *Service) filesRegionLocked() (string, int64) {
	region, at := "", int64(0)
	for _, c := range s.files {
		if c.region != "" && (region == "" || c.regionAt > at) {
			region, at = c.region, c.regionAt
		}
	}
	return region, at
}

var portalLine = regexp.MustCompile(`(?i)^\s*SET\s+portal\s+"([a-z]+)"`)

func portalRegion(path string) string {
	f, err := os.Open(path)
	if err != nil {
		slog.Info("sign-in: the game's settings can't be read", "path", path, "err", err)
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		if m := portalLine.FindStringSubmatch(sc.Text()); m != nil {
			switch v := strings.ToLower(m[1]); v {
			case "eu", "us":
				return v
			default:
				slog.Info("sign-in: the game's portal is neither EU nor US", "portal", v)
			}
			return ""
		}
	}
	if err := sc.Err(); err != nil {
		slog.Info("sign-in: the game's settings can't be read", "path", path, "err", err)
		return ""
	}
	slog.Info("sign-in: the game's settings name no portal", "path", path)
	return ""
}

var realmWord = regexp.MustCompile(`\b(EU|US)[0-9]+\b`)

func recordRegion(r savedvars.Record) string {
	ch, _ := r.Data["character"].(map[string]any)
	realm, _ := ch["realm"].(string)
	if m := realmWord.FindStringSubmatch(realm); m != nil {
		return strings.ToLower(m[1])
	}
	cl, _ := ch["client"].(map[string]any)
	var n int64
	switch v := cl["region"].(type) {
	case int64:
		n = v
	case float64:
		n = int64(v)
	}
	switch n {
	case 1:
		return "us"
	case 3:
		return "eu"
	}
	return ""
}

type Brief struct {
	Account     Account
	LastUpload  time.Time
	Uploading   bool
	Problem     string
	NotKept     string
	WrongRegion bool
}

func (s *Service) Brief() Brief {
	s.mu.Lock()
	up := s.up
	s.mu.Unlock()
	var b Brief
	var st *uploader.Status
	if up != nil {
		u := up.Status()
		st = &u
		b.LastUpload = u.LastUpload
		b.Problem = u.Problem
		b.NotKept = u.NotKept
	}
	b.Uploading = s.runActive.Load()
	s.mu.Lock()
	b.Account = s.accountLocked(st)
	found, _ := s.filesRegionLocked()
	s.mu.Unlock()
	if st != nil {
		b.WrongRegion = wrongRegion(strings.ToUpper(b.Account.Region), strings.ToUpper(found), b.NotKept)
	}
	return b
}

func (s *Service) httpClient() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: auth.Timeout}
}
