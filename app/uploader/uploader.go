package uploader

import (
	"bytes"
	"compress/gzip"
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wizardofsalem/wft-companion/app/config"
	"github.com/wizardofsalem/wft-companion/app/savedvars"
)

const (
	MaxRecords     = 500
	MaxBytes       = 256 << 10
	MaxRecordBytes = savedvars.MaxSnapshotBytes
	MaxInflated    = 1 << 20
)

var errPlain = errors.New("the tracker does not take gzip")

const unknownType = "unknown type"

const (
	firstBackoff  = 30 * time.Second
	maxBackoff    = 30 * time.Minute
	maxRetryAfter = 24 * time.Hour
	retryGiveUp   = 14 * 24 * time.Hour
	keepUploaded  = 45 * 24 * time.Hour
)

var ErrWaiting = errors.New("waiting before the next try")

const (
	ProblemOffline = "offline"
	ProblemServer  = "server"
	ProblemUpdate  = "update"
)

func (u *Uploader) refused(status int) {
	if !u.needsSignIn {
		slog.Warn("upload: the tracker refused this computer's code; waiting for a new sign-in", "status", status)
	}
	u.needsSignIn = true
	u.nextAttempt = time.Time{}
	u.lastErr, u.problem = SignInLine, ""
}

const checkID = "00000000000000000000000000000000"

func (u *Uploader) CheckCode(ctx context.Context) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.needsSignIn {
		return
	}
	url := fmt.Sprintf("%s/api/%s/reports/uploads/%s", strings.TrimRight(u.Base, "/"), u.Code.Region, checkID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+u.Code.String())
	req.Header.Set("User-Agent", "WFT-Companion/"+u.AppVersion)
	client := u.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("code check: no answer", "err", err)
		return
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		u.refused(resp.StatusCode)
	default:
		slog.Debug("code check", "status", resp.StatusCode)
	}
}

var ErrNeedsSignIn = errors.New("sign in again to keep uploading")

const SignInLine = "This computer was signed out. Sign in again to keep uploading."

type Rejection struct {
	At     int64  `json:"at"`
	Reason string `json:"reason"`
}

type State struct {
	Uploaded   map[string]int64     `json:"uploaded"`
	Rejected   map[string]Rejection `json:"rejected"`
	Totals     map[string]int       `json:"totals"`
	Retry      map[string]Retrying  `json:"retry,omitempty"`
	GaveUp     int                  `json:"gaveUp,omitempty"`
	LastUpload int64                `json:"lastUpload,omitempty"`
	Inflight   *Inflight            `json:"inflight,omitempty"`
	Tracked    []TrackedUpload      `json:"tracked,omitempty"`
	Linked     bool                 `json:"linked,omitempty"`
	LinkedAt   int64                `json:"linkedAt,omitempty"`
	NotKept    string               `json:"notKept,omitempty"`
}

type Inflight struct {
	BatchID string       `json:"batchId"`
	Region  string       `json:"region"`
	Created int64        `json:"created"`
	Gzipped bool         `json:"gzipped"`
	Body    []byte       `json:"body"`
	Records []SentRecord `json:"records"`
}

type SentRecord struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Part int    `json:"part,omitempty"`
}

const BatchHeader = "X-WFT-Batch-Id"

func newBatchID() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	return hex.EncodeToString(b)
}

type Retrying struct {
	First     int64 `json:"first"`
	Next      int64 `json:"next"`
	Tries     int   `json:"tries"`
	Untracked bool  `json:"untracked,omitempty"`
}

func retryDelay(untracked bool, tries int) time.Duration {
	if untracked {
		return 24 * time.Hour
	}
	switch tries {
	case 1:
		return 10 * time.Minute
	case 2:
		return 30 * time.Minute
	}
	return time.Hour
}

type Result struct {
	Requests    int
	Accepted    int
	Duplicates  int
	Untracked   int
	Retry       int
	Rejected    int
	Pending     int
	Answered    int
	OtherRegion int
}

type Uploader struct {
	Client     *http.Client
	Base       string
	Code       config.Code
	AppVersion string
	StatePath  string
	Now        func() time.Time
	Jitter     func() float64
	OnSent     func(n int)

	mu          sync.Mutex
	state       State
	loaded      bool
	nextAttempt time.Time
	failures    int
	lastErr     string
	plain       bool
	retryNow    atomic.Bool
	lastMu      sync.Mutex
	last        Status
	needsSignIn bool
	problem     string
	pagedPrune  bool
	skew        time.Duration
	rosters     map[string]rosterSeen
}

func (u *Uploader) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func (u *Uploader) load() error {
	if u.loaded {
		return nil
	}
	u.state = State{}
	b, err := os.ReadFile(u.StatePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(b, &u.state); err != nil {
			slog.Warn("upload: state file damaged; starting afresh", "path", u.StatePath, "err", err)
			u.state = State{}
		}
	}
	if u.state.Uploaded == nil {
		u.state.Uploaded = map[string]int64{}
	}
	if u.state.Rejected == nil {
		u.state.Rejected = map[string]Rejection{}
	}
	if u.state.Retry == nil {
		u.state.Retry = map[string]Retrying{}
	}
	if u.state.Totals == nil {
		u.state.Totals = map[string]int{}
	}
	u.loaded = true
	slog.Info("upload: state loaded", "path", u.StatePath, "uploaded", len(u.state.Uploaded), "rejected", len(u.state.Rejected))
	return nil
}

func (u *Uploader) save() error {
	b, err := json.Marshal(u.state)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(u.StatePath, b)
}

type Status struct {
	LastUpload   time.Time
	NextAttempt  time.Time
	LastError    string
	Waiting      int
	Untracked    int
	GaveUp       int
	Totals       map[string]int
	Rejected     map[string]Rejection
	ClockSkew    time.Duration
	Batch        *BatchStatus
	Linked       bool
	LinkedAt     time.Time
	NeedsSignIn  bool
	Problem      string
	UntrackedIDs map[string]bool
	NotKept      string
}

func (u *Uploader) Status() Status {
	if !u.mu.TryLock() {
		u.lastMu.Lock()
		defer u.lastMu.Unlock()
		return u.last
	}
	defer u.mu.Unlock()
	st := u.status()
	u.lastMu.Lock()
	u.last = st
	u.lastMu.Unlock()
	return st
}

func (u *Uploader) status() Status {
	_ = u.load()
	s := Status{NextAttempt: u.nextAttempt, LastError: u.lastErr, GaveUp: u.state.GaveUp, Totals: map[string]int{}, Rejected: map[string]Rejection{},
		ClockSkew: u.skew, Batch: u.batchStatus(), Linked: u.state.Linked, NeedsSignIn: u.needsSignIn,
		Problem: u.problem, UntrackedIDs: map[string]bool{}, NotKept: u.state.NotKept}
	if u.state.LinkedAt > 0 {
		s.LinkedAt = time.Unix(u.state.LinkedAt, 0)
	}
	if u.state.LastUpload > 0 {
		s.LastUpload = time.Unix(u.state.LastUpload, 0)
	}
	for k, v := range u.state.Totals {
		s.Totals[k] = v
	}
	for k, v := range u.state.Rejected {
		s.Rejected[k] = v
	}
	for id, rt := range u.state.Retry {
		if rt.Untracked {
			s.UntrackedIDs[id] = true
			s.UntrackedIDs[snapshotOf(id)] = true
			s.Untracked++
		} else {
			s.Waiting++
		}
	}
	return s
}

const (
	NotKeptRegion  = "region"
	NotKeptRefused = "refused"
)

func (u *Uploader) NoteRun(res Result) {
	if res.Answered == 0 {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.load() != nil {
		return
	}
	kind := ""
	if res.Accepted+res.Duplicates+res.Retry == 0 {
		switch {
		case res.OtherRegion > 0:
			kind = NotKeptRegion
		case res.Rejected > 0:
			kind = NotKeptRefused
		}
	}
	if kind == u.state.NotKept {
		return
	}
	if kind != "" {
		slog.Warn("upload: the tracker kept nothing of this run", "why", kind, "uploads", res.Answered,
			"otherRegion", res.OtherRegion, "rejected", res.Rejected, "region", u.Code.Region)
	} else {
		slog.Info("upload: the tracker keeps this run's records again")
	}
	u.state.NotKept = kind
	if err := u.save(); err != nil {
		slog.Warn("upload: saving the state failed", "err", err)
	}
}

func (u *Uploader) RetryNow() {
	if u.mu.TryLock() {
		u.nextAttempt = time.Time{}
		u.mu.Unlock()
		return
	}
	u.retryNow.Store(true)
}

func presentIDs(records []savedvars.Record) func(string) bool {
	m := make(map[string]bool, len(records))
	for _, r := range records {
		m[r.ID] = true
	}
	return func(id string) bool { return m[id] || m[snapshotOf(id)] }
}

func snapshotOf(id string) string {
	if i := strings.LastIndex(id, ".p"); i > 0 {
		if _, err := strconv.Atoi(id[i+2:]); err == nil {
			return id[:i]
		}
	}
	return id
}

func (u *Uploader) Prune(waiting []string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.load(); err != nil {
		return
	}
	u.pagedPrune = true
	m := make(map[string]bool, len(waiting))
	for _, id := range waiting {
		m[id] = true
	}
	u.prune(func(id string) bool { return m[id] || m[snapshotOf(id)] })
	if inf := u.state.Inflight; inf != nil {
		for _, r := range inf.Records {
			if !m[r.ID] && !m[snapshotOf(r.ID)] {
				slog.Info("upload: unanswered upload dropped: its records left the queue", "batchId", inf.BatchID)
				u.state.Inflight = nil
				break
			}
		}
	}
	if err := u.save(); err != nil {
		slog.Warn("upload: saving state after pruning failed", "err", err)
	}
}

func (u *Uploader) Settled(r savedvars.Record) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.load(); err != nil {
		return false
	}
	for _, part := range split([]savedvars.Record{r}) {
		_, done := u.state.Uploaded[part.ID]
		_, final := u.state.Rejected[part.ID]
		if !done && !final {
			return false
		}
	}
	return true
}

func (u *Uploader) Pending(records []savedvars.Record) ([]savedvars.Record, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.load(); err != nil {
		return nil, err
	}
	return u.pending(split(records)), nil
}

func (u *Uploader) pending(records []savedvars.Record) []savedvars.Record {
	now := u.now().Unix()
	var out []savedvars.Record
	for _, r := range records {
		if _, done := u.state.Uploaded[r.ID]; done {
			continue
		}
		if _, final := u.state.Rejected[r.ID]; final {
			continue
		}
		if rt, ok := u.state.Retry[r.ID]; ok && now < rt.Next {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (u *Uploader) Sync(ctx context.Context, records []savedvars.Record) (Result, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	var res Result
	if err := u.load(); err != nil {
		return res, err
	}
	records = split(records)
	if u.retryNow.Swap(false) {
		u.nextAttempt = time.Time{}
	}
	if u.needsSignIn {
		res.Pending = len(u.pending(records))
		return res, ErrNeedsSignIn
	}
	if u.now().Before(u.nextAttempt) {
		res.Pending = len(u.pending(records))
		return res, ErrWaiting
	}
	if !u.pagedPrune {
		u.prune(presentIDs(records))
	}

	var err error
	if inf := u.state.Inflight; inf != nil && inf.Region != u.Code.Region {
		slog.Info("upload: unanswered upload dropped: the upload code is for another region", "batchId", inf.BatchID)
		u.state.Inflight = nil
	}
	if inf := u.state.Inflight; inf != nil {
		slog.Info("upload: sending the unanswered upload again", "batchId", inf.BatchID, "records", len(inf.Records))
		err = u.resend(ctx, &res)
	}
	var queue [][]savedvars.Record
	if err == nil {
		var tooLarge []savedvars.Record
		queue, tooLarge = batches(u.skipFreshRosters(u.pending(records)), u.plain)
		for _, r := range tooLarge {
			slog.Warn("upload: record too large to send", "id", r.ID, "type", r.Type)
			u.reject(r.ID, "record larger than the upload limit")
			res.Rejected++
		}
	}
	for len(queue) > 0 && err == nil {
		batch := queue[0]
		queue = queue[1:]
		var halve bool
		halve, err = u.send(ctx, batch, &res)
		if halve {
			mid := len(batch) / 2
			queue = append([][]savedvars.Record{batch[:mid], batch[mid:]}, queue...)
		}
		if errors.Is(err, errPlain) {
			rest := batch
			for _, b := range queue {
				rest = append(rest[:len(rest):len(rest)], b...)
			}
			queue, _ = batches(rest, true)
			err = nil
		}
	}
	if saveErr := u.save(); saveErr != nil && err == nil {
		err = saveErr
	}
	res.Pending = len(u.pending(records))
	if err == nil {
		u.lastErr, u.problem = "", ""
	}
	return res, err
}

type response struct {
	Accepted     int      `json:"accepted"`
	Duplicates   int      `json:"duplicates"`
	Dropped      int      `json:"dropped"`
	OtherRegion  *int     `json:"otherRegion"`
	Untracked    int      `json:"untracked"`
	Retry        []string `json:"retry"`
	UntrackedIDs []string `json:"untrackedIds"`
	Rejected     []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"rejected"`
	Error            string               `json:"error"`
	RosterReceivedAt map[string]time.Time `json:"rosterReceivedAt"`
	UploadID         string               `json:"uploadId"`
	Linked           *bool                `json:"linked"`
	LinkedAt         time.Time            `json:"linkedAt"`
	Resent           bool                 `json:"resent"`
	Queue            *queueAnswer         `json:"queue"`
}

const RosterFresh = time.Hour

type rosterSeen struct {
	received   time.Time
	serverDate time.Time
	local      time.Time
}

func (u *Uploader) rosterIsFresh(guild string) bool {
	seen, ok := u.rosters[strings.ToLower(guild)]
	if !ok || guild == "" {
		return false
	}
	serverNow := seen.serverDate.Add(u.now().Sub(seen.local))
	return serverNow.Sub(seen.received) < RosterFresh
}

func (u *Uploader) skipFreshRosters(records []savedvars.Record) []savedvars.Record {
	out := records[:0:0]
	skipped := 0
	for _, r := range records {
		if r.Type == "guildRoster" {
			g, _ := r.Data["guild"].(string)
			if u.rosterIsFresh(g) {
				u.state.Uploaded[r.ID] = u.now().Unix()
				skipped++
				continue
			}
		}
		out = append(out, r)
	}
	if skipped > 0 {
		slog.Info("upload: guild roster not sent: the tracker got one less than an hour ago", "parts", skipped)
	}
	return out
}

func (u *Uploader) send(ctx context.Context, batch []savedvars.Record, res *Result) (halve bool, err error) {
	inf := &Inflight{BatchID: newBatchID(), Region: u.Code.Region, Created: u.now().Unix(), Gzipped: !u.plain}
	body, err := encode(batch, u.AppVersion, inf.BatchID)
	if err != nil {
		return false, err
	}
	if inf.Gzipped {
		if body, err = compress(body); err != nil {
			return false, err
		}
		if len(body) > MaxBytes && len(batch) > 1 {
			slog.Info("upload: batch too large even gzipped; splitting it", "records", len(batch), "bytes", len(body))
			return true, nil
		}
	}
	inf.Body = body
	inf.Records = make([]SentRecord, len(batch))
	for i, r := range batch {
		inf.Records[i] = SentRecord{ID: r.ID, Type: r.Type, Part: r.Part}
	}
	u.state.Inflight = inf
	if err := u.save(); err != nil {
		u.state.Inflight = nil
		return false, err
	}
	return u.post(ctx, res)
}

func (u *Uploader) resend(ctx context.Context, res *Result) error {
	_, err := u.post(ctx, res)
	if errors.Is(err, errPlain) {
		err = nil
	}
	return err
}

func (u *Uploader) post(ctx context.Context, res *Result) (halve bool, err error) {
	inf := u.state.Inflight
	body, gzipped, batch := inf.Body, inf.Gzipped, inf.Records
	done := func() { u.state.Inflight = nil }
	url := fmt.Sprintf("%s/api/%s/reports", strings.TrimRight(u.Base, "/"), u.Code.Region)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if gzipped {
		req.Header.Set("Content-Encoding", "gzip")
	}
	req.Header.Set("Authorization", "Bearer "+u.Code.String())
	req.Header.Set("User-Agent", "WFT-Companion/"+u.AppVersion)
	req.Header.Set(BatchHeader, inf.BatchID)

	client := u.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	res.Requests++
	start := time.Now()
	slog.Info("upload: sending", "url", url, "batchId", inf.BatchID, "records", len(batch), "bytes", len(body), "gzip", gzipped)
	resp, err := client.Do(req)
	if err != nil {
		return false, u.fail(ProblemOffline, "Can't reach the tracker right now (offline?). Trying again automatically.", err)
	}
	defer resp.Body.Close()
	if date, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
		u.skew = date.Sub(u.now()).Round(time.Second)
		if u.skew > 10*time.Minute || u.skew < -10*time.Minute {
			slog.Warn("upload: this computer's clock is off", "skew", u.skew.String())
		}
	}
	var parsed response
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = json.Unmarshal(raw, &parsed)
	otherRegion := 0
	if parsed.OtherRegion != nil {
		otherRegion = *parsed.OtherRegion
	}
	slog.Info("upload: response", "status", resp.StatusCode, "ms", time.Since(start).Milliseconds(),
		"accepted", parsed.Accepted, "duplicates", parsed.Duplicates, "dropped", parsed.Dropped, "otherRegion", otherRegion, "untracked", parsed.Untracked, "untrackedIds", len(parsed.UntrackedIDs), "retry", len(parsed.Retry), "rejected", len(parsed.Rejected), "error", parsed.Error,
		"uploadId", parsed.UploadID, "resent", parsed.Resent)
	for i, r := range parsed.Rejected {
		if i == 5 {
			slog.Info("upload: more refused records not logged", "count", len(parsed.Rejected)-5)
			break
		}
		slog.Info("upload: record refused", "id", r.ID, "reason", r.Reason)
	}

	switch {
	case gzipped && (resp.StatusCode == http.StatusUnsupportedMediaType || resp.StatusCode == http.StatusBadRequest):
		done()
		u.plain = true
		slog.Warn("upload: gzip refused; sending plain JSON from now on", "status", resp.StatusCode)
		return false, errPlain
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		done()
		if parsed.Linked != nil {
			if *parsed.Linked != u.state.Linked {
				slog.Info("upload: this computer's link to an account changed", "linked", *parsed.Linked)
			}
			u.state.Linked = *parsed.Linked
			u.state.LinkedAt = 0
			if *parsed.Linked && !parsed.LinkedAt.IsZero() {
				u.state.LinkedAt = parsed.LinkedAt.Unix()
			}
		}
		if len(parsed.RosterReceivedAt) > 0 {
			date, derr := http.ParseTime(resp.Header.Get("Date"))
			if derr != nil {
				date = u.now()
			}
			if u.rosters == nil {
				u.rosters = map[string]rosterSeen{}
			}
			for g, at := range parsed.RosterReceivedAt {
				u.rosters[strings.ToLower(g)] = rosterSeen{received: at, serverDate: date, local: u.now()}
			}
		}
		now := u.now().Unix()
		rejected := map[string]string{}
		retry := map[string]bool{}
		for _, r := range parsed.Rejected {
			if r.Reason == unknownType {
				retry[r.ID] = false
				continue
			}
			rejected[r.ID] = r.Reason
		}
		for _, id := range parsed.Retry {
			retry[id] = false
		}
		for _, id := range parsed.UntrackedIDs {
			retry[id] = true
		}
		for _, r := range batch {
			if reason, ok := rejected[r.ID]; ok {
				u.reject(r.ID, reason)
				delete(u.state.Retry, r.ID)
				continue
			}
			if untracked, again := retry[r.ID]; again {
				rt, ok := u.state.Retry[r.ID]
				if !ok {
					rt.First = now
				}
				if rt.Untracked != untracked {
					rt.Tries = 0
				}
				rt.Untracked = untracked
				rt.Tries++
				rt.Next = now + int64(retryDelay(untracked, rt.Tries)/time.Second)
				u.state.Retry[r.ID] = rt
				res.Retry++
				continue
			}
			u.state.Uploaded[r.ID] = now
			delete(u.state.Retry, r.ID)
			if r.Part <= 1 {
				u.state.Totals[r.Type]++
			}
		}
		if u.OnSent != nil {
			n := 0
			for _, r := range batch {
				if r.Part <= 1 {
					n++
				}
			}
			u.OnSent(n)
		}
		u.track(parsed.UploadID, parsed.Accepted, parsed.Queue)
		res.Answered++
		res.OtherRegion += otherRegion
		res.Accepted += parsed.Accepted
		res.Duplicates += parsed.Duplicates
		res.Untracked += parsed.Untracked
		res.Rejected += len(rejected)
		u.state.LastUpload = now
		u.failures = 0
		return false, u.save()
	case resp.StatusCode == http.StatusTooManyRequests:
		done()
		wait := retryAfter(resp.Header.Get("Retry-After"), u.now())
		if wait <= 0 {
			u.failures++
			wait = u.backoff()
		}
		u.nextAttempt = u.now().Add(wait)
		u.lastErr, u.problem = "The tracker asked us to slow down. Trying again later.", ""
		slog.Warn("upload: rate limited", "retryAfter", resp.Header.Get("Retry-After"), "wait", wait.String())
		return false, fmt.Errorf("rate limited until %s", u.nextAttempt.Format(time.RFC3339))
	case resp.StatusCode == http.StatusUnauthorized:
		done()
		u.refused(resp.StatusCode)
		return false, ErrNeedsSignIn
	case resp.StatusCode == http.StatusForbidden:
		done()
		return false, u.fail(ProblemServer, "The tracker refused this computer's code for this region.", errors.New("HTTP 403"))
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		done()
		if len(batch) > 1 {
			slog.Warn("upload: batch too large for the server; splitting it", "records", len(batch))
			return true, nil
		}
		u.reject(batch[0].ID, "record larger than the server accepts")
		res.Rejected++
		return false, nil
	case resp.StatusCode == http.StatusBadRequest:
		done()
		reason := parsed.Error
		if reason == "" {
			reason = "bad request"
		}
		return false, u.fail(ProblemUpdate, "The tracker refused the upload ("+reason+"). This app may need an update; trying again later.",
			fmt.Errorf("HTTP 400: %s", reason))
	default:
		return false, u.fail(ProblemServer, "The tracker had a problem. Trying again automatically.", fmt.Errorf("HTTP %d", resp.StatusCode))
	}
}

func (u *Uploader) reject(id, reason string) {
	if len(reason) > 200 {
		reason = reason[:200]
	}
	u.state.Rejected[id] = Rejection{At: u.now().Unix(), Reason: reason}
}

func (u *Uploader) fail(problem, message string, err error) error {
	u.failures++
	wait := u.backoff()
	u.nextAttempt = u.now().Add(wait)
	u.lastErr, u.problem = message, problem
	slog.Warn("upload: failed; backing off", "err", err, "failures", u.failures, "wait", wait.Round(time.Second).String())
	return fmt.Errorf("%s (%w)", message, err)
}

func (u *Uploader) backoff() time.Duration {
	d := firstBackoff
	for i := 1; i < u.failures && d < maxBackoff; i++ {
		d *= 2
	}
	d = min(d, maxBackoff)
	j := rand.Float64()
	if u.Jitter != nil {
		j = u.Jitter()
	}
	return time.Duration(float64(d) * (0.8 + 0.4*j))
}

func retryAfter(h string, now time.Time) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	var d time.Duration
	if secs, err := strconv.Atoi(h); err == nil {
		d = time.Duration(secs) * time.Second
	} else if t, err := http.ParseTime(h); err == nil {
		d = t.Sub(now)
	}
	if d <= 0 {
		return 0
	}
	return min(max(d, time.Second), maxRetryAfter)
}

func (u *Uploader) prune(present func(id string) bool) {
	cutoff := u.now().Add(-keepUploaded).Unix()
	for id, at := range u.state.Uploaded {
		if !present(id) && at < cutoff {
			delete(u.state.Uploaded, id)
		}
	}
	for id, rej := range u.state.Rejected {
		if !present(id) && rej.At < cutoff {
			delete(u.state.Rejected, id)
		}
	}
	giveUp := u.now().Add(-retryGiveUp).Unix()
	for id, rt := range u.state.Retry {
		if rt.First < giveUp || !present(id) {
			if present(id) {
				u.state.GaveUp++
				u.state.Uploaded[id] = u.now().Unix()
			}
			delete(u.state.Retry, id)
			slog.Info("upload: gave up resending", "id", id, "tries", rt.Tries, "stillInFiles", present(id))
		}
	}
}

type envelope struct {
	Schema  int            `json:"schema"`
	App     map[string]any `json:"app"`
	Records []any          `json:"records"`
}

func encode(batch []savedvars.Record, version, batchID string) ([]byte, error) {
	env := envelope{Schema: 1, App: map[string]any{"version": version, "batchId": batchID}, Records: make([]any, len(batch))}
	for i, r := range batch {
		env.Records[i] = r.Data
	}
	return json.Marshal(env)
}

func compress(body []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const MaxProbeBytes = 512 << 10

func recordLimit(typ string) int {
	if typ == "featsProbe" {
		return MaxProbeBytes
	}
	return MaxRecordBytes
}

func batches(records []savedvars.Record, plain bool) (out [][]savedvars.Record, tooLarge []savedvars.Record) {
	overhead, _ := encode(nil, strings.Repeat("x", 64), newBatchID())
	budget := MaxInflated - len(overhead)
	if plain {
		budget = MaxBytes - len(overhead)
	}
	var cur []savedvars.Record
	size := 0
	for _, r := range records {
		b, err := json.Marshal(r.Data)
		if err != nil || len(b) > recordLimit(r.Type) {
			tooLarge = append(tooLarge, r)
			continue
		}
		n := len(b) + 1
		if len(cur) == MaxRecords || size+n > budget {
			out = append(out, cur)
			cur, size = nil, 0
		}
		cur = append(cur, r)
		size += n
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out, tooLarge
}
