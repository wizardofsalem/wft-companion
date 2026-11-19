package savedvars

import (
	"fmt"
	"os"
)

func Stream(path string, fn func(Record) error) (*Addon, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxFileSize {
		return nil, fmt.Errorf("%s is too large (%d bytes)", path, info.Size())
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	a := &Addon{Enabled: map[string]bool{}}
	seen := map[string]bool{}
	var fnErr error
	hook := func(p []any, t *Table) bool {
		if fnErr != nil || !recordPath(p) {
			return true
		}
		id, idOK := t.Get("id").(string)
		typ, typeOK := t.Get("type").(string)
		if !idOK && !typeOK {
			return true
		}
		r, ok := toRecord(t, id, typ)
		switch {
		case !ok:
			a.Skipped++
		case !seen[r.ID]:
			seen[r.ID] = true
			fnErr = fn(r)
		}
		return false
	}
	vars, err := parse(src, hook)
	src = nil
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if fnErr != nil {
		return nil, fnErr
	}
	meta := Extract(vars)
	meta.Skipped += a.Skipped
	meta.Records = nil
	return meta, nil
}

func recordPath(p []any) bool {
	if len(p) < 3 || p[0] != GlobalName {
		return false
	}
	switch p[1] {
	case "history":
		return len(p) == 4 || len(p) == 5
	case "state":
		return len(p) == 4
	}
	return false
}
