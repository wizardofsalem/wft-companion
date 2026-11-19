package updatestate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/wizardofsalem/wft-companion/app/config"
)

const FileName = "update.json"

func Path(dir string) string { return filepath.Join(dir, FileName) }

func load(dir string) map[string]json.RawMessage {
	fields := map[string]json.RawMessage{}
	b, err := os.ReadFile(Path(dir))
	if err != nil {
		return fields
	}
	if json.Unmarshal(b, &fields) != nil {
		os.Rename(Path(dir), Path(dir)+".broken")
		return map[string]json.RawMessage{}
	}
	return fields
}

func save(dir string, fields map[string]json.RawMessage) error {
	b, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(Path(dir), b)
}

func Starts(dir string) int {
	var n int
	json.Unmarshal(load(dir)["starts"], &n)
	return n
}

func IncrementStarts(dir string) (int, error) {
	if dir == "" {
		return 0, errors.New("no app folder")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	fields := load(dir)
	var n int
	json.Unmarshal(fields["starts"], &n)
	n++
	fields["starts"], _ = json.Marshal(n)
	return n, save(dir, fields)
}

func ResetStarts(dir string) error {
	fields := load(dir)
	var n int
	json.Unmarshal(fields["starts"], &n)
	if n == 0 && fields["starts"] != nil {
		return nil
	}
	fields["starts"], _ = json.Marshal(0)
	return save(dir, fields)
}

func Get(dir, key string, v any) bool {
	raw, ok := load(dir)[key]
	return ok && json.Unmarshal(raw, v) == nil
}

func Set(dir string, fields map[string]any) error {
	all := load(dir)
	for k, v := range fields {
		if v == nil {
			delete(all, k)
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		all[k] = b
	}
	return save(dir, all)
}
