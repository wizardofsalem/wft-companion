package savedvars

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
)

const GlobalName = "WoWForeverGuildChronicleDB"

var Types = []string{"rankNames", "professions", "talents", "level", "quest", "instance", "loot", "sold", "gold", "death", "played", "reputations",
	"honor", "guildRoster", "guildLog", "guildInfo", "charter", "bossKill", "lootWon", "bgMatch",
	"inspect", "duel", "questsDone", "xp", "sighting", "groupDeath", "calendar", "guildChat", "zones", "moments", "social", "dayCounts", "lockouts", "session", "groupSession", "fight", "lootHistory", "kills", "gear", "gearChange", "itemFacts", "charStats", "currencies", "inventory", "spellbook", "spellUse", "collections", "guildNews", "featsProbe"}

func knownType(t string) bool {
	for _, k := range Types {
		if k == t {
			return true
		}
	}
	return false
}

type Record struct {
	ID       string
	Type     string
	At       int64
	Data     map[string]any
	Snapshot string
	Part     int
}

type Addon struct {
	Schema  int64
	Enabled map[string]bool
	Records []Record
	Skipped int
	Diag    Diag
	Probe   []Probe
}

type Diag struct {
	Version string
	Build   string
	Locale  string
	Log     []string
	Missing []string
	Events  map[string]int64
}

func readDiag(t *Table) Diag {
	d := Diag{Events: map[string]int64{}}
	if t == nil {
		return d
	}
	d.Version, _ = t.Get("version").(string)
	d.Build, _ = t.Get("build").(string)
	d.Locale, _ = t.Get("locale").(string)
	if log := t.Table("log"); log != nil && log.IsArray() {
		d.Log = make([]string, len(log.Fields))
		for _, f := range log.Fields {
			d.Log[f.Key.(int64)-1], _ = f.Value.(string)
		}
	}
	if missing := t.Table("missing"); missing != nil {
		for _, f := range missing.Fields {
			if name, ok := f.Key.(string); ok {
				d.Missing = append(d.Missing, name)
			}
		}
		sort.Strings(d.Missing)
	}
	if events := t.Table("events"); events != nil {
		for _, f := range events.Fields {
			name, ok := f.Key.(string)
			n, isInt := f.Value.(int64)
			if ok && isInt {
				d.Events[name] = n
			}
		}
	}
	return d
}

func (a *Addon) CountByType() map[string]int {
	out := map[string]int{}
	for _, r := range a.Records {
		out[r.Type]++
	}
	return out
}

const MaxFileSize = 64 << 20

func ReadFile(path string) (*Addon, error) {
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
	vars, err := Parse(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return Extract(vars), nil
}

func Extract(vars map[string]any) *Addon {
	a := &Addon{Enabled: map[string]bool{}}
	db, _ := vars[GlobalName].(*Table)
	if db == nil {
		return a
	}
	if n, ok := db.Get("schema").(int64); ok {
		a.Schema = n
	}
	a.Diag = readDiag(db.Table("diag"))
	a.Probe = readProbe(db.Table("probe"))
	if enabled := db.Table("enabled"); enabled != nil {
		for _, f := range enabled.Fields {
			if k, ok := f.Key.(string); ok {
				on, _ := f.Value.(bool)
				a.Enabled[k] = on
			}
		}
	}
	seen := map[string]bool{}
	var walk func(t *Table, depth int)
	walk = func(t *Table, depth int) {
		id, idOK := t.Get("id").(string)
		typ, typeOK := t.Get("type").(string)
		if idOK || typeOK {
			r, ok := toRecord(t, id, typ)
			switch {
			case !ok:
				a.Skipped++
			case !seen[r.ID]:
				seen[r.ID] = true
				a.Records = append(a.Records, r)
			}
			return
		}
		if depth >= 3 {
			return
		}
		for _, f := range t.Fields {
			if sub, ok := f.Value.(*Table); ok {
				walk(sub, depth+1)
			}
		}
	}
	for _, section := range []string{"history", "state"} {
		if t := db.Table(section); t != nil {
			walk(t, 0)
		}
	}
	sort.Slice(a.Records, func(i, j int) bool {
		if a.Records[i].At != a.Records[j].At {
			return a.Records[i].At < a.Records[j].At
		}
		return a.Records[i].ID < a.Records[j].ID
	})
	return a
}

var idShape = regexp.MustCompile(`^[A-Za-z0-9_.:-]{8,80}$`)

func toRecord(t *Table, id, typ string) (Record, bool) {
	if !knownType(typ) {
		return Record{}, false
	}
	data, ok := ToJSON(t).(map[string]any)
	if !ok {
		return Record{}, false
	}
	at, _ := t.Get("at").(int64)
	if at <= 0 {
		return Record{}, false
	}
	if !idShape.MatchString(id) {
		b, _ := json.Marshal(data)
		sum := sha256.Sum256(b)
		id = typ + "-" + hex.EncodeToString(sum[:8])
		data["id"] = id
	}
	return Record{ID: id, Type: typ, At: at, Data: data}, true
}
