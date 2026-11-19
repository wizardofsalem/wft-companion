package uploader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wizardofsalem/wft-companion/app/config"
)

type AccountError struct {
	Status int
	Reason string
	Line   string
}

func (e *AccountError) Error() string { return e.Line }

func accountLine(status int, reason string) string {
	switch reason {
	case "tooManyCodes":
		return "Your account has too many computers. Remove one on your account page, then sign in."
	case "ipLimit":
		return "Too many sign-ins. Try again later."
	case "dayLimit":
		return "Too many sign-ins today. Try again tomorrow."
	case "notOpen":
		return "Sign-ups aren't open yet. Try again later."
	case "hourFull":
		return "We're busy right now. Try again later."
	case "emailUnverified":
		return "Confirm your email first, then sign in."
	case "linkedElsewhere":
		return "This computer belongs to another account."
	}
	switch {
	case status == http.StatusUnauthorized:
		return "Sign-in didn't work. Try again."
	case status == http.StatusForbidden:
		return "This account can't sign in here. Try again later."
	case status >= 500:
		return "Something went wrong. Try again later."
	}
	return "Sign-in didn't work. Try again later."
}

func accountRequest(ctx context.Context, c *http.Client, method, url, bearer string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, b, nil
}

func refusal(status int, b []byte) *AccountError {
	var r struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(b, &r)
	return &AccountError{Status: status, Reason: r.Reason, Line: accountLine(status, r.Reason)}
}

var ErrOffline = errors.New("can't reach the tracker right now")

func NewAppCode(ctx context.Context, c *http.Client, base, token, region, label, device string) (config.Code, error) {
	body := map[string]string{"region": region, "label": label, "device": device}
	status, b, err := accountRequest(ctx, c, http.MethodPost, strings.TrimRight(base, "/")+"/api/me/codes", token, body)
	if err != nil {
		slog.Warn("sign-in: code request failed", "err", err)
		return config.Code{}, ErrOffline
	}
	if status != http.StatusCreated && status != http.StatusOK {
		e := refusal(status, b)
		slog.Warn("sign-in: code refused", "status", status, "reason", e.Reason)
		return config.Code{}, e
	}
	var r struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return config.Code{}, fmt.Errorf("the tracker's answer: %w", err)
	}
	code, err := config.ParseCode(r.Code)
	if err != nil {
		return config.Code{}, fmt.Errorf("the tracker's code: %w", err)
	}
	return code, nil
}

func AttachCode(ctx context.Context, c *http.Client, base, token string, code config.Code) error {
	status, b, err := accountRequest(ctx, c, http.MethodPost, strings.TrimRight(base, "/")+"/api/me/links", token,
		map[string]string{"code": code.String()})
	if err != nil {
		slog.Warn("sign-in: attach failed", "err", err)
		return ErrOffline
	}
	if status != http.StatusOK && status != http.StatusCreated {
		e := refusal(status, b)
		slog.Warn("sign-in: attach refused", "status", status, "reason", e.Reason)
		return e
	}
	return nil
}

func RevokeCode(ctx context.Context, c *http.Client, base string, code config.Code) error {
	status, _, err := accountRequest(ctx, c, http.MethodPost,
		strings.TrimRight(base, "/")+"/api/"+code.Region+"/installs/revoke", code.String(), nil)
	if err != nil {
		slog.Warn("sign out: revoke failed", "err", err)
		return ErrOffline
	}
	switch status {
	case http.StatusNoContent, http.StatusOK, http.StatusUnauthorized:
		slog.Info("sign out: code revoked", "status", status)
		return nil
	}
	slog.Warn("sign out: revoke refused", "status", status)
	return fmt.Errorf("HTTP %d", status)
}
