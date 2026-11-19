package schema

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type Kind int

const (
	Str Kind = iota + 1
	Int
	Num
	Bool
	Obj
	Arr
)

func (k Kind) String() string {
	return [...]string{"", "string", "integer", "number", "boolean", "object", "list"}[k]
}

type Field struct {
	Kind     Kind
	Max      int
	Ranged   bool
	Lo, Hi   float64
	Fields   map[string]Field
	Open     bool
	Required []string
	Elem     *Field
	Line     bool
	Time     bool
}

const (
	DefaultStr = 200
	DefaultArr = 1000
)

type Problem struct {
	Field string
	Rule  string
}

func (p Problem) String() string { return p.Field + ": " + p.Rule }

var (
	Oldest      = time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	FutureSlack = 10 * time.Minute
)

func Check(rec map[string]any, now time.Time) (p Problem, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			p, ok = Problem{Field: "", Rule: fmt.Sprintf("check failed: %v", r)}, false
		}
	}()
	typ, _ := rec["type"].(string)
	spec, known := Types[typ]
	if !known {
		return Problem{Field: "type", Rule: "unknown type"}, false
	}
	if p, bad := check("", rec, spec); bad {
		return p, false
	}
	for name, sub := range spec.Fields {
		if t, ok := asFloat(rec[name]); ok && sub.Time && t > float64(now.Add(FutureSlack).Unix()) {
			return Problem{Field: name, Rule: "in the future"}, false
		}
	}
	if extra := orderChecks[typ]; extra != nil {
		if p, bad := extra(rec); bad {
			return p, false
		}
	}
	at, _ := asFloat(rec["at"])
	switch {
	case at < float64(Oldest.Unix()):
		return Problem{Field: "at", Rule: "before 2019"}, false
	case at > float64(now.Add(FutureSlack).Unix()):
		return Problem{Field: "at", Rule: "in the future"}, false
	}
	return Problem{}, true
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func kindOf(v any) Kind {
	switch n := v.(type) {
	case string:
		return Str
	case int64, int:
		return Int
	case float64:
		if n == math.Trunc(n) && !math.IsInf(n, 0) {
			return Int
		}
		return Num
	case bool:
		return Bool
	case map[string]any:
		return Obj
	case []any:
		return Arr
	}
	return 0
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func check(path string, v any, f Field) (Problem, bool) {
	k := kindOf(v)
	switch {
	case f.Kind == Num && k == Int:
	case k != f.Kind:
		return Problem{path, "should be a " + f.Kind.String()}, true
	}
	switch f.Kind {
	case Str:
		s := v.(string)
		limit := f.Max
		if limit == 0 {
			limit = DefaultStr
		}
		if len(s) > limit {
			return Problem{path, fmt.Sprintf("longer than %d", limit)}, true
		}
		if f.Line && (s == "" || strings.ContainsAny(s, "\n\r\x00")) {
			return Problem{path, "not one line of text"}, true
		}
	case Int, Num:
		n, _ := asFloat(v)
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return Problem{path, "not a number"}, true
		}
		if f.Ranged && (n < f.Lo || n > f.Hi) {
			return Problem{path, fmt.Sprintf("outside %g..%g", f.Lo, f.Hi)}, true
		}
	case Obj:
		m := v.(map[string]any)
		for _, name := range f.Required {
			if _, ok := m[name]; !ok {
				return Problem{join(path, name), "missing"}, true
			}
		}
		names := make([]string, 0, len(m))
		for name := range m {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			sub, known := f.Fields[name]
			if !known {
				if f.Open {
					continue
				}
				return Problem{join(path, name), "field not allowed"}, true
			}
			if p, bad := check(join(path, name), m[name], sub); bad {
				return p, true
			}
		}
	case Arr:
		list := v.([]any)
		limit := f.Max
		if limit == 0 {
			limit = DefaultArr
		}
		if len(list) > limit {
			return Problem{path, fmt.Sprintf("more than %d entries", limit)}, true
		}
		if f.Elem != nil {
			for i, e := range list {
				if p, bad := check(fmt.Sprintf("%s[%d]", path, i), e, *f.Elem); bad {
					return p, true
				}
			}
		}
	}
	return Problem{}, false
}

var orderChecks = map[string]func(rec map[string]any) (Problem, bool){
	"kills": func(rec map[string]any) (Problem, bool) {
		at, _ := asFloat(rec["at"])
		login, _ := asFloat(rec["sessionLoginAt"])
		first, hasFirst := asFloat(rec["firstAt"])
		last, hasLast := asFloat(rec["lastAt"])
		slack := FutureSlack.Seconds()
		switch {
		case login > at+slack || at-login > 7*24*3600:
			return Problem{"sessionLoginAt", "not within the week before the record"}, true
		case hasLast && (last < login || last > at+slack):
			return Problem{"lastAt", "outside the session"}, true
		case hasFirst && (first < login || first > at+slack || (hasLast && first > last)):
			return Problem{"firstAt", "outside the session"}, true
		}
		return Problem{}, false
	},
}
