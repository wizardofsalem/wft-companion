package savedvars

import (
	"encoding/json"
	"sort"
	"time"
)

type Probe map[string]any

type ProbeSummary struct {
	StartedAt     time.Time `json:"startedAt"`
	Client        string    `json:"client"`
	Interface     int64     `json:"interface"`
	APIsMissing   int       `json:"apisMissing"`
	EventsUnknown int       `json:"eventsUnknown"`
	Blocked       int       `json:"blocked"`
	Errors        int       `json:"errors"`
	Unsupported   []string  `json:"unsupported"`
	CombatLog     string    `json:"combatLog"`
}

func readProbe(t *Table) []Probe {
	if t == nil {
		return nil
	}
	sessions := t.Table("sessions")
	if sessions == nil || !sessions.IsArray() {
		return nil
	}
	var out []Probe
	for _, f := range sessions.Fields {
		if m, ok := ToJSON(f.Value).(map[string]any); ok {
			out = append(out, Probe(m))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt().Before(out[j].StartedAt()) })
	return out
}

func (p Probe) int(key string) int64 {
	n, _ := p[key].(int64)
	return n
}

func (p Probe) obj(key string) map[string]any {
	m, _ := p[key].(map[string]any)
	return m
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func (p Probe) StartedAt() time.Time {
	if n := p.int("startedAt"); n > 0 {
		return time.Unix(n, 0)
	}
	return time.Time{}
}

func (p Probe) Summary() ProbeSummary {
	s := ProbeSummary{StartedAt: p.StartedAt(), CombatLog: "unknown", Unsupported: []string{}}
	if c := p.obj("client"); c != nil {
		v, _ := c["version"].(string)
		b, _ := c["build"].(string)
		s.Client = v
		if b != "" {
			s.Client += " (" + b + ")"
		}
		s.Interface, _ = c["interface"].(int64)
	}
	if a := p.obj("apis"); a != nil {
		s.APIsMissing = len(list(a["missing"]))
	}
	s.EventsUnknown = len(list(p["unknownEvents"]))
	for _, b := range list(p["blocked"]) {
		n, _ := b.(map[string]any)["count"].(int64)
		s.Blocked += int(n)
	}
	for _, e := range list(p["errors"]) {
		n, _ := e.(map[string]any)["count"].(int64)
		s.Errors += int(n)
	}
	for _, u := range list(p["unsupported"]) {
		if str, ok := u.(string); ok {
			s.Unsupported = append(s.Unsupported, str)
		}
	}
	if c := p.obj("combatLog"); c != nil {
		switch {
		case c["registered"] == false:
			s.CombatLog = "not registered"
		case c["secret"] == true:
			s.CombatLog = "secret"
		case c["readable"] == true:
			s.CombatLog = "readable"
		}
	}
	return s
}

func (p Probe) JSON() []byte {
	b, _ := json.MarshalIndent(map[string]any(p), "", " ")
	return b
}

func (a *Addon) LatestProbe() Probe {
	if len(a.Probe) == 0 {
		return nil
	}
	return a.Probe[len(a.Probe)-1]
}
