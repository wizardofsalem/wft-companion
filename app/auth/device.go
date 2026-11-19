package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	Enabled     bool   `json:"enabled"`
	Domain      string `json:"domain"`
	Audience    string `json:"audience"`
	AppClientID string `json:"appClientId"`
}

var (
	ErrNotSetUp    = errors.New("sign-in is not set up yet")
	ErrUnreachable = errors.New("can't reach the sign-in service right now")
	ErrExpired     = errors.New("the sign-in timed out")
	ErrDenied      = errors.New("sign-in was refused")
)

const Timeout = 30 * time.Second

func orDefault(c *http.Client) *http.Client {
	if c == nil {
		return &http.Client{Timeout: Timeout}
	}
	return c
}

func FetchConfig(ctx context.Context, c *http.Client, base string) (Config, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/auth/config", nil)
	if err != nil {
		return Config{}, err
	}
	resp, err := orDefault(c).Do(req)
	if err != nil {
		slog.Warn("sign-in: tracker unreachable", "err", err)
		return Config{}, ErrUnreachable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("sign-in: auth config", "status", resp.StatusCode)
		return Config{}, ErrUnreachable
	}
	var cfg Config
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&cfg); err != nil {
		return Config{}, ErrUnreachable
	}
	if !cfg.Enabled || cfg.Domain == "" || cfg.AppClientID == "" || cfg.Audience == "" {
		return Config{}, ErrNotSetUp
	}
	return cfg, nil
}

type Device struct {
	DeviceCode string
	UserCode   string
	VerifyURL  string
	ExpiresAt  time.Time
	Interval   time.Duration
}

type deviceAnswer struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func tenantURL(cfg Config, path string) string {
	d := strings.TrimRight(cfg.Domain, "/")
	if !strings.HasPrefix(d, "https://") {
		d = "https://" + strings.TrimPrefix(d, "http://")
	}
	return d + path
}

func Start(ctx context.Context, c *http.Client, cfg Config, now time.Time) (Device, error) {
	form := url.Values{"client_id": {cfg.AppClientID}, "audience": {cfg.Audience}, "scope": {"openid"}}
	var a deviceAnswer
	status, _, err := post(ctx, c, tenantURL(cfg, "/oauth/device/code"), form, &a)
	if err != nil {
		return Device{}, err
	}
	if status != http.StatusOK || a.DeviceCode == "" || a.UserCode == "" {
		slog.Warn("sign-in: device code refused", "status", status)
		return Device{}, ErrUnreachable
	}
	d := Device{DeviceCode: a.DeviceCode, UserCode: a.UserCode, VerifyURL: a.VerificationURIComplete,
		ExpiresAt: now.Add(time.Duration(a.ExpiresIn) * time.Second), Interval: time.Duration(a.Interval) * time.Second}
	if d.VerifyURL == "" {
		d.VerifyURL = a.VerificationURI
	}
	if !strings.HasPrefix(d.VerifyURL, "https://") {
		slog.Warn("sign-in: the tenant's confirm page is not https")
		return Device{}, ErrUnreachable
	}
	if d.Interval <= 0 {
		d.Interval = 5 * time.Second
	}
	if a.ExpiresIn <= 0 {
		d.ExpiresAt = now.Add(15 * time.Minute)
	}
	return d, nil
}

type tokenAnswer struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

func Poll(ctx context.Context, c *http.Client, cfg Config, d Device, now func() time.Time,
	sleep func(context.Context, time.Duration) error) (string, error) {
	interval := d.Interval
	failures := 0
	for {
		if err := sleep(ctx, interval); err != nil {
			return "", err
		}
		if now().After(d.ExpiresAt) {
			return "", ErrExpired
		}
		form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {d.DeviceCode}, "client_id": {cfg.AppClientID}}
		var a tokenAnswer
		status, _, err := post(ctx, c, tenantURL(cfg, "/oauth/token"), form, &a)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if failures++; failures >= 3 {
				return "", ErrUnreachable
			}
			continue
		}
		failures = 0
		switch {
		case status == http.StatusOK && a.AccessToken != "":
			return a.AccessToken, nil
		case a.Error == "authorization_pending":
		case a.Error == "slow_down":
			interval += 5 * time.Second
		case a.Error == "expired_token":
			return "", ErrExpired
		case a.Error == "access_denied":
			return "", ErrDenied
		default:
			slog.Warn("sign-in: token refused", "status", status, "error", a.Error)
			return "", ErrUnreachable
		}
	}
}

func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func post(ctx context.Context, c *http.Client, u string, form url.Values, out any) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := orDefault(c).Do(req)
	if err != nil {
		slog.Warn("sign-in: tenant unreachable", "err", err)
		return 0, nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	_ = json.Unmarshal(b, out)
	return resp.StatusCode, b, nil
}
