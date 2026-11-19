package savedvars

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
)

const (
	MaxSnapshotBytes = 128 << 10
	MaxPartEntries   = 500
	MaxParts         = 100
)

var listFields = map[string]string{"guildRoster": "members", "guildLog": "events", "calendar": "events"}

func Split(records []Record) []Record {
	var out []Record
	for _, r := range records {
		out = append(out, parts(r)...)
	}
	return out
}

func parts(r Record) []Record {
	whole := []Record{r}
	field, ok := listFields[r.Type]
	if !ok {
		return whole
	}
	list, ok := r.Data[field].([]any)
	if !ok {
		return whole
	}
	b, err := json.Marshal(r.Data)
	if err != nil || (len(b) <= MaxSnapshotBytes && len(list) <= MaxPartEntries) {
		return whole
	}
	base := maps.Clone(r.Data)
	base["id"] = r.ID + ".p100"
	base[field] = []any{}
	base["snapshot"] = map[string]any{"id": r.ID, "part": MaxParts, "parts": MaxParts}
	b, err = json.Marshal(base)
	if err != nil {
		return whole
	}
	budget := MaxSnapshotBytes - len(b)
	var groups [][]any
	var cur []any
	size := 0
	for _, e := range list {
		eb, err := json.Marshal(e)
		if err != nil {
			return whole
		}
		n := len(eb) + 1
		if len(cur) > 0 && (len(cur) == MaxPartEntries || size+n > budget) {
			groups = append(groups, cur)
			cur, size = nil, 0
		}
		cur = append(cur, e)
		size += n
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	if len(groups) > MaxParts {
		slog.Warn("savedvars: snapshot needs more parts than the tracker takes", "id", r.ID, "type", r.Type, "entries", len(list))
		return whole
	}
	out := make([]Record, len(groups))
	for i, g := range groups {
		id := fmt.Sprintf("%s.p%d", r.ID, i+1)
		d := maps.Clone(r.Data)
		d["id"] = id
		d[field] = g
		d["snapshot"] = map[string]any{"id": r.ID, "part": i + 1, "parts": len(groups)}
		out[i] = Record{ID: id, Type: r.Type, At: r.At, Data: d, Snapshot: r.ID, Part: i + 1}
	}
	return out
}
