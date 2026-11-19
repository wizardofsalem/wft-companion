package outbox

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wizardofsalem/wft-companion/app/config"
	"github.com/wizardofsalem/wft-companion/app/savedvars"
)

var (
	SegmentBytes int64 = 1 << 20
	MaxBytes     int64 = 100 << 20
	MaxAge             = 90 * 24 * time.Hour
)

type line struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	At     int64          `json:"at"`
	Data   map[string]any `json:"data"`
	Queued int64          `json:"queued"`
}

type header struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	At     int64  `json:"at"`
	Queued int64  `json:"queued"`
}

type Meta struct {
	ID   string
	Type string
	At   int64
}

type entry struct {
	Meta
	queued int64
	seg    int
	off    int64
	size   int64
}

type segment struct {
	live, total int
	bytes       int64
}

type Outbox struct {
	dir string
	now func() time.Time

	mu      sync.Mutex
	recs    map[string]*entry
	segs    map[int]*segment
	cur     int
	bytes   int64
	dropped int
}

type Stats struct {
	Records int            `json:"records"`
	Bytes   int64          `json:"bytes"`
	ByType  map[string]int `json:"byType"`
	Dropped int            `json:"dropped,omitempty"`
}

func segName(n int) string { return fmt.Sprintf("seg-%06d.jsonl", n) }

func (o *Outbox) segPath(n int) string { return filepath.Join(o.dir, segName(n)) }

func Open(dir string, now func() time.Time) (*Outbox, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	o := &Outbox{dir: dir, now: now, recs: map[string]*entry{}, segs: map[int]*segment{}}
	names, err := filepath.Glob(filepath.Join(dir, "seg-*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	for _, p := range names {
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "seg-"), ".jsonl"))
		if err != nil {
			continue
		}
		o.load(p, n)
		o.cur = max(o.cur, n)
	}
	o.cur++
	tmps, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	for _, t := range tmps {
		os.Remove(t)
	}
	o.enforce()
	slog.Info("outbox: loaded", "records", len(o.recs), "segments", len(o.segs), "bytes", o.bytes)
	return o, nil
}

func (o *Outbox) load(p string, n int) {
	f, err := os.Open(p)
	if err != nil {
		slog.Warn("outbox: can't read a segment", "path", p, "err", err)
		return
	}
	defer f.Close()
	seg := &segment{}
	o.segs[n] = seg
	r := bufio.NewReaderSize(f, 64<<10)
	var off int64
	bad := 0
	for {
		raw, err := r.ReadBytes('\n')
		if len(raw) == 0 && err != nil {
			break
		}
		size := int64(len(raw))
		lineOff := off
		off += size
		if raw[len(raw)-1] != '\n' {
			bad++
			break
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var h header
		if json.Unmarshal(raw, &h) != nil || h.ID == "" || h.Type == "" {
			bad++
			continue
		}
		seg.total++
		seg.bytes += size
		if old, dup := o.recs[h.ID]; dup {
			o.segs[old.seg].live--
			o.bytes -= old.size
		}
		o.recs[h.ID] = &entry{Meta: Meta{ID: h.ID, Type: h.Type, At: h.At}, queued: h.Queued, seg: n, off: lineOff, size: size}
		seg.live++
		o.bytes += size
	}
	if bad > 0 {
		slog.Warn("outbox: skipped unreadable lines (a torn write?)", "path", p, "lines", bad)
	}
}

func (o *Outbox) Has(id string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.recs[id]
	return ok
}

func (o *Outbox) Add(records []savedvars.Record) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var buf bytes.Buffer
	var added []Meta
	var sizes []int64
	queued := o.now().Unix()
	for _, r := range records {
		if _, ok := o.recs[r.ID]; ok {
			continue
		}
		b, err := json.Marshal(line{ID: r.ID, Type: r.Type, At: r.At, Data: r.Data, Queued: queued})
		if err != nil {
			slog.Warn("outbox: record can't be stored", "id", r.ID, "type", r.Type, "err", err)
			continue
		}
		buf.Write(b)
		buf.WriteByte('\n')
		added = append(added, Meta{ID: r.ID, Type: r.Type, At: r.At})
		sizes = append(sizes, int64(len(b)+1))
	}
	if len(added) == 0 {
		return 0, nil
	}
	if err := o.append(buf.Bytes(), added, sizes, queued); err != nil {
		return 0, err
	}
	o.enforce()
	return len(added), nil
}

func (o *Outbox) append(data []byte, added []Meta, sizes []int64, queued int64) error {
	if seg := o.segs[o.cur]; seg != nil && seg.bytes >= SegmentBytes {
		o.cur++
	}
	if o.segs[o.cur] == nil {
		o.segs[o.cur] = &segment{}
	}
	f, err := os.OpenFile(o.segPath(o.cur), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	off := info.Size()
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	seg := o.segs[o.cur]
	for i, m := range added {
		o.recs[m.ID] = &entry{Meta: m, queued: queued, seg: o.cur, off: off, size: sizes[i]}
		off += sizes[i]
		seg.live++
		seg.total++
		seg.bytes += sizes[i]
		o.bytes += sizes[i]
	}
	return nil
}

func (o *Outbox) Index() []Meta {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Meta, 0, len(o.recs))
	for _, e := range o.recs {
		out = append(out, e.Meta)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At != out[j].At {
			return out[i].At < out[j].At
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (o *Outbox) Load(ids []string) []savedvars.Record {
	o.mu.Lock()
	defer o.mu.Unlock()
	files := map[int]*os.File{}
	defer closeAll(files)
	out := make([]savedvars.Record, 0, len(ids))
	for _, id := range ids {
		e, ok := o.recs[id]
		if !ok {
			continue
		}
		raw, err := o.readLine(files, e)
		if err != nil {
			slog.Warn("outbox: can't read a queued record back", "id", id, "err", err)
			continue
		}
		var l line
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&l); err != nil || l.ID != id {
			slog.Warn("outbox: queued record unreadable", "id", id)
			continue
		}
		out = append(out, savedvars.Record{ID: l.ID, Type: l.Type, At: l.At, Data: l.Data})
	}
	return out
}

func closeAll(files map[int]*os.File) {
	for _, f := range files {
		f.Close()
	}
}

func (o *Outbox) readLine(files map[int]*os.File, e *entry) ([]byte, error) {
	f := files[e.seg]
	if f == nil {
		var err error
		if f, err = os.Open(o.segPath(e.seg)); err != nil {
			return nil, err
		}
		files[e.seg] = f
	}
	raw := make([]byte, e.size)
	if _, err := f.ReadAt(raw, e.off); err != nil {
		return nil, err
	}
	return raw, nil
}

func (o *Outbox) All() []savedvars.Record {
	idx := o.Index()
	ids := make([]string, len(idx))
	for i, m := range idx {
		ids[i] = m.ID
	}
	return o.Load(ids)
}

func (o *Outbox) Remove(ids []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, id := range ids {
		o.removeLocked(id)
	}
	o.compact()
}

func (o *Outbox) RemoveType(typ string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for id, e := range o.recs {
		if e.Type == typ {
			o.removeLocked(id)
			n++
		}
	}
	o.compact()
	return n
}

func (o *Outbox) removeLocked(id string) {
	e, ok := o.recs[id]
	if !ok {
		return
	}
	delete(o.recs, id)
	o.bytes -= e.size
	if seg := o.segs[e.seg]; seg != nil {
		seg.live--
	}
}

func (o *Outbox) enforce() {
	cutoff := o.now().Add(-MaxAge).Unix()
	var old []string
	for id, e := range o.recs {
		if e.queued < cutoff {
			old = append(old, id)
		}
	}
	if o.bytes > MaxBytes {
		all := make([]*entry, 0, len(o.recs))
		for _, e := range o.recs {
			if e.queued >= cutoff {
				all = append(all, e)
			}
		}
		sort.Slice(all, func(i, j int) bool {
			if all[i].queued != all[j].queued {
				return all[i].queued < all[j].queued
			}
			return all[i].At < all[j].At
		})
		over := o.bytes
		for _, id := range old {
			over -= o.recs[id].size
		}
		for _, e := range all {
			if over <= MaxBytes {
				break
			}
			old = append(old, e.ID)
			over -= e.size
		}
	}
	if len(old) == 0 {
		return
	}
	for _, id := range old {
		o.removeLocked(id)
	}
	o.dropped += len(old)
	slog.Warn("outbox: dropped the oldest records (over the queue's limits)", "dropped", len(old),
		"maxAge", MaxAge.String(), "maxBytes", MaxBytes)
	o.compact()
}

func (o *Outbox) compact() {
	for n, seg := range o.segs {
		switch {
		case seg.live <= 0:
			if err := os.Remove(o.segPath(n)); err == nil || os.IsNotExist(err) {
				delete(o.segs, n)
				if n == o.cur {
					o.cur++
				}
			}
		case seg.live*2 < seg.total && (n != o.cur || seg.bytes >= SegmentBytes):
			o.rewrite(n)
		}
	}
}

func (o *Outbox) rewrite(n int) {
	var live []*entry
	for _, e := range o.recs {
		if e.seg == n {
			live = append(live, e)
		}
	}
	sort.Slice(live, func(i, j int) bool { return live[i].off < live[j].off })
	files := map[int]*os.File{}
	var buf bytes.Buffer
	offs := make([]int64, len(live))
	for i, e := range live {
		raw, err := o.readLine(files, e)
		if err != nil {
			closeAll(files)
			slog.Warn("outbox: compaction failed (will try again)", "segment", n, "err", err)
			return
		}
		offs[i] = int64(buf.Len())
		buf.Write(raw)
	}
	closeAll(files)
	if err := config.WriteFileAtomic(o.segPath(n), buf.Bytes()); err != nil {
		slog.Warn("outbox: compaction failed (will try again)", "segment", n, "err", err)
		return
	}
	for i, e := range live {
		e.off = offs[i]
	}
	o.segs[n] = &segment{live: len(live), total: len(live), bytes: int64(buf.Len())}
}

func (o *Outbox) Stats() Stats {
	o.mu.Lock()
	defer o.mu.Unlock()
	st := Stats{Records: len(o.recs), Bytes: o.bytes, ByType: map[string]int{}, Dropped: o.dropped}
	for _, e := range o.recs {
		st.ByType[e.Type]++
	}
	return st
}
