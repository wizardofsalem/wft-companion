package uploader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	BatchQueued = "queued"
	BatchDone   = "done"
	BatchHeld   = "held"
)

const (
	checkAfterDue = 2 * time.Minute
	checkQueued   = 10 * time.Minute
	checkHeld     = 6 * time.Hour
	maxChecks     = 10
	keepDone      = 7 * 24 * time.Hour
	keepTracked   = 60 * 24 * time.Hour
	maxTracked    = 500
)

type BatchCounts struct {
	Counted    int `json:"counted"`
	Duplicates int `json:"duplicates"`
	Dropped    int `json:"dropped"`
	Rejected   int `json:"rejected"`
}

func (c *BatchCounts) add(o BatchCounts) {
	c.Counted += o.Counted
	c.Duplicates += o.Duplicates
	c.Dropped += o.Dropped
	c.Rejected += o.Rejected
}

type TrackedUpload struct {
	ID          string       `json:"id"`
	Window      string       `json:"window,omitempty"`
	State       string       `json:"state"`
	SentAt      int64        `json:"sentAt"`
	Accepted    int          `json:"accepted"`
	NextBatchAt int64        `json:"nextBatchAt,omitempty"`
	DoneAt      int64        `json:"doneAt,omitempty"`
	NextCheck   int64        `json:"nextCheck,omitempty"`
	Result      *BatchCounts `json:"result,omitempty"`
}

type queueAnswer struct {
	State       string    `json:"state"`
	Window      string    `json:"window"`
	NextBatchAt time.Time `json:"nextBatchAt"`
}

func (u *Uploader) track(id string, accepted int, q *queueAnswer) {
	if id == "" || q == nil {
		return
	}
	t := TrackedUpload{ID: id, Window: q.Window, State: q.State, SentAt: u.now().Unix(), Accepted: accepted}
	if t.State != BatchDone {
		t.State = BatchQueued
	}
	if !q.NextBatchAt.IsZero() {
		t.NextBatchAt = q.NextBatchAt.Unix()
	}
	t.NextCheck = u.dueCheck(t.NextBatchAt)
	for i, old := range u.state.Tracked {
		if old.ID == id {
			u.state.Tracked[i] = t
			return
		}
	}
	u.state.Tracked = append(u.state.Tracked, t)
	if n := len(u.state.Tracked); n > maxTracked {
		u.state.Tracked = append([]TrackedUpload(nil), u.state.Tracked[n-maxTracked:]...)
	}
}

func (u *Uploader) dueCheck(nextBatchAt int64) int64 {
	if nextBatchAt == 0 {
		return u.now().Add(checkQueued).Unix()
	}
	return nextBatchAt - int64(u.skew/time.Second) + int64(checkAfterDue/time.Second)
}

type statusAnswer struct {
	State       string       `json:"state"`
	Window      string       `json:"window"`
	NextBatchAt time.Time    `json:"nextBatchAt"`
	DoneAt      time.Time    `json:"doneAt"`
	Result      *BatchCounts `json:"result"`
}

func (u *Uploader) CheckBatches(ctx context.Context) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.load(); err != nil {
		return
	}
	now := u.now()
	changed := u.forgetTracked(now)
	checks := 0
	for i := range u.state.Tracked {
		t := &u.state.Tracked[i]
		if t.State == "" || t.State == BatchDone || t.NextCheck > now.Unix() {
			continue
		}
		if checks == maxChecks {
			break
		}
		checks++
		changed = true
		if !u.readStatus(ctx, t) {
			t.NextCheck = now.Add(checkQueued).Unix()
		}
	}
	if changed {
		u.state.Tracked = slicesDeleteGone(u.state.Tracked)
		if err := u.save(); err != nil {
			slog.Warn("upload: saving the batch status failed", "err", err)
		}
	}
}

func (u *Uploader) readStatus(ctx context.Context, t *TrackedUpload) bool {
	endpoint := fmt.Sprintf("%s/api/%s/reports/uploads/%s", strings.TrimRight(u.Base, "/"), u.Code.Region, url.PathEscape(t.ID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+u.Code.String())
	req.Header.Set("User-Agent", "WFT-Companion/"+u.AppVersion)
	client := u.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.Info("upload: batch status not read", "uploadId", t.ID, "err", err)
		return false
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		slog.Info("upload: batch status gone; no longer followed", "uploadId", t.ID)
		t.State = ""
		return true
	case resp.StatusCode != http.StatusOK:
		slog.Info("upload: batch status not read", "uploadId", t.ID, "status", resp.StatusCode)
		return false
	}
	var st statusAnswer
	if err := json.Unmarshal(raw, &st); err != nil {
		return false
	}
	if st.Window != "" {
		t.Window = st.Window
	}
	switch st.State {
	case BatchDone:
		t.State = BatchDone
		if !st.DoneAt.IsZero() {
			t.DoneAt = st.DoneAt.Unix()
		}
		t.Result = st.Result
		t.NextCheck = 0
		slog.Info("upload: counted in the guild's batch", "uploadId", t.ID, "result", fmt.Sprintf("%+v", st.Result))
	case BatchHeld:
		t.State = BatchHeld
		t.NextCheck = u.now().Add(checkHeld).Unix()
	default:
		t.State = BatchQueued
		if !st.NextBatchAt.IsZero() {
			t.NextBatchAt = st.NextBatchAt.Unix()
		}
		t.NextCheck = max(u.dueCheck(t.NextBatchAt), u.now().Add(checkQueued).Unix())
	}
	return true
}

func (u *Uploader) forgetTracked(now time.Time) bool {
	changed := false
	for i := range u.state.Tracked {
		t := &u.state.Tracked[i]
		if (t.State == BatchDone && now.Unix()-t.SentAt > int64(keepDone/time.Second)) ||
			now.Unix()-t.SentAt > int64(keepTracked/time.Second) {
			t.State = ""
			changed = true
		}
	}
	return changed
}

func slicesDeleteGone(ts []TrackedUpload) []TrackedUpload {
	out := ts[:0]
	for _, t := range ts {
		if t.State != "" {
			out = append(out, t)
		}
	}
	return out
}

type BatchStatus struct {
	Queued      int
	Held        int
	NextBatchAt time.Time
	DoneAt      time.Time
	Done        int
	Counts      BatchCounts
}

func (u *Uploader) batchStatus() *BatchStatus {
	if len(u.state.Tracked) == 0 {
		return nil
	}
	var b BatchStatus
	var next, doneAt int64
	newestWindow := ""
	done := make([]TrackedUpload, 0, len(u.state.Tracked))
	for _, t := range u.state.Tracked {
		switch t.State {
		case BatchQueued:
			b.Queued++
			next = max(next, t.NextBatchAt)
		case BatchHeld:
			b.Held++
		case BatchDone:
			done = append(done, t)
		}
	}
	sort.SliceStable(done, func(i, j int) bool { return done[i].Window < done[j].Window })
	if len(done) > 0 {
		newestWindow = done[len(done)-1].Window
	}
	for _, t := range done {
		if t.Window != newestWindow {
			continue
		}
		b.Done++
		doneAt = max(doneAt, t.DoneAt)
		if t.Result != nil {
			b.Counts.add(*t.Result)
		}
	}
	if next > 0 {
		b.NextBatchAt = time.Unix(next, 0)
	}
	if doneAt > 0 {
		b.DoneAt = time.Unix(doneAt, 0)
	}
	return &b
}
