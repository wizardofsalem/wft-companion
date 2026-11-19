package savedvars

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Field struct {
	Key   any
	Value any
}

type Table struct {
	Fields []Field
}

func (t *Table) Get(key any) any {
	if t == nil {
		return nil
	}
	for i := len(t.Fields) - 1; i >= 0; i-- {
		if t.Fields[i].Key == key {
			return t.Fields[i].Value
		}
	}
	return nil
}

func (t *Table) Table(key any) *Table {
	v, _ := t.Get(key).(*Table)
	return v
}

func (t *Table) IsArray() bool {
	seen := make(map[int64]bool, len(t.Fields))
	for _, f := range t.Fields {
		n, ok := f.Key.(int64)
		if !ok || n < 1 || n > int64(len(t.Fields)) || seen[n] {
			return false
		}
		seen[n] = true
	}
	return true
}

const MaxDepth = 64

func Parse(src []byte) (map[string]any, error) {
	return parse(src, nil)
}

func parse(src []byte, onTable func(path []any, t *Table) bool) (map[string]any, error) {
	p := &parser{src: src, line: 1, onTable: onTable}
	out := map[string]any{}
	for {
		if err := p.skipSpace(); err != nil {
			return nil, err
		}
		if p.pos >= len(p.src) {
			return out, nil
		}
		if p.src[p.pos] == ';' {
			p.pos++
			continue
		}
		name, ok := p.name()
		if !ok {
			return nil, p.errorf("expected a variable name")
		}
		if err := p.skipSpace(); err != nil {
			return nil, err
		}
		if !p.consume('=') {
			return nil, p.errorf("expected '=' after %s", name)
		}
		p.path = append(p.path[:0], name)
		v, err := p.value(0)
		if err != nil {
			return nil, err
		}
		out[name] = v
	}
}

type parser struct {
	src  []byte
	pos  int
	line int

	onTable func(path []any, t *Table) bool
	path    []any
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("line %d: %s", p.line, fmt.Sprintf(format, args...))
}

func (p *parser) consume(c byte) bool {
	if p.pos < len(p.src) && p.src[p.pos] == c {
		p.pos++
		return true
	}
	return false
}

func (p *parser) skipSpace() error {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == '\n':
			p.line++
			p.pos++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			p.pos++
		case c == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '-':
			p.pos += 2
			if level, ok := p.longBracketLevel(); ok {
				if _, err := p.longString(level); err != nil {
					return err
				}
				continue
			}
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		case c == 0xEF && p.pos+2 < len(p.src) && p.src[p.pos+1] == 0xBB && p.src[p.pos+2] == 0xBF:
			p.pos += 3
		default:
			return nil
		}
	}
	return nil
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameChar(c byte) bool {
	return isNameStart(c) || (c >= '0' && c <= '9')
}

func (p *parser) name() (string, bool) {
	start := p.pos
	if p.pos >= len(p.src) || !isNameStart(p.src[p.pos]) {
		return "", false
	}
	for p.pos < len(p.src) && isNameChar(p.src[p.pos]) {
		p.pos++
	}
	return string(p.src[start:p.pos]), true
}

func (p *parser) value(depth int) (any, error) {
	if err := p.skipSpace(); err != nil {
		return nil, err
	}
	if p.pos >= len(p.src) {
		return nil, p.errorf("unexpected end of file")
	}
	c := p.src[p.pos]
	switch {
	case c == '{':
		if depth >= MaxDepth {
			return nil, p.errorf("tables nested too deeply")
		}
		t, err := p.table(depth + 1)
		if t == nil {
			return nil, err
		}
		return t, err
	case c == '"' || c == '\'':
		return p.quoted()
	case c == '[':
		if level, ok := p.longBracketLevel(); ok {
			return p.longString(level)
		}
		return nil, p.errorf("unexpected '['")
	case c == '-' || c == '.' || (c >= '0' && c <= '9'):
		return p.number()
	case isNameStart(c):
		word, _ := p.name()
		switch word {
		case "nil":
			return nil, nil
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "inf", "nan":
			p.skipNaNSuffix()
			return nil, nil
		}
		return nil, p.errorf("unexpected word %q", word)
	}
	return nil, p.errorf("unexpected character %q", c)
}

func (p *parser) table(depth int) (*Table, error) {
	p.pos++
	t := &Table{}
	var next int64 = 1
	for {
		if err := p.skipSpace(); err != nil {
			return nil, err
		}
		if p.pos >= len(p.src) {
			return nil, p.errorf("unclosed table")
		}
		if p.consume('}') {
			if p.onTable != nil && !p.onTable(p.path, t) {
				return nil, nil
			}
			return t, nil
		}
		var key, val any
		var err error
		switch c := p.src[p.pos]; {
		case c == '[' && !p.atLongBracket():
			p.pos++
			if key, err = p.value(depth); err != nil {
				return nil, err
			}
			if err = p.skipSpace(); err != nil {
				return nil, err
			}
			if !p.consume(']') {
				return nil, p.errorf("expected ']'")
			}
			if err = p.expectEquals(); err != nil {
				return nil, err
			}
			if key == nil {
				return nil, p.errorf("table key is nil")
			}
			if f, ok := key.(float64); ok && f == math.Trunc(f) && math.Abs(f) < 1<<53 {
				key = int64(f)
			}
		case isNameStart(c) && p.isNamedField():
			key, _ = p.name()
			if err = p.expectEquals(); err != nil {
				return nil, err
			}
		default:
			key = next
			next++
		}
		if p.onTable != nil {
			p.path = append(p.path, key)
		}
		val, err = p.value(depth)
		if p.onTable != nil {
			p.path = p.path[:len(p.path)-1]
		}
		if err != nil {
			return nil, err
		}
		if val != nil {
			t.Fields = append(t.Fields, Field{Key: key, Value: val})
		}
		if err = p.skipSpace(); err != nil {
			return nil, err
		}
		if !p.consume(',') && !p.consume(';') {
			if err = p.skipSpace(); err != nil {
				return nil, err
			}
			if p.pos < len(p.src) && p.src[p.pos] == '}' {
				continue
			}
			return nil, p.errorf("expected ',' or '}' in table")
		}
	}
}

func (p *parser) isNamedField() bool {
	save, line := p.pos, p.line
	defer func() { p.pos, p.line = save, line }()
	p.name()
	if p.skipSpace() != nil {
		return false
	}
	return p.pos < len(p.src) && p.src[p.pos] == '=' && (p.pos+1 >= len(p.src) || p.src[p.pos+1] != '=')
}

func (p *parser) expectEquals() error {
	if err := p.skipSpace(); err != nil {
		return err
	}
	if !p.consume('=') {
		return p.errorf("expected '='")
	}
	return nil
}

func (p *parser) atLongBracket() bool {
	_, ok := p.longBracketLevel()
	return ok
}

func (p *parser) longBracketLevel() (int, bool) {
	if p.pos >= len(p.src) || p.src[p.pos] != '[' {
		return 0, false
	}
	i := p.pos + 1
	for i < len(p.src) && p.src[i] == '=' {
		i++
	}
	if i < len(p.src) && p.src[i] == '[' {
		return i - p.pos - 1, true
	}
	return 0, false
}

func (p *parser) longString(level int) (string, error) {
	p.pos += level + 2
	if p.pos < len(p.src) && p.src[p.pos] == '\r' {
		p.pos++
	}
	if p.pos < len(p.src) && p.src[p.pos] == '\n' {
		p.line++
		p.pos++
	}
	closing := "]" + strings.Repeat("=", level) + "]"
	end := strings.Index(string(p.src[p.pos:]), closing)
	if end < 0 {
		return "", p.errorf("unclosed long string")
	}
	s := string(p.src[p.pos : p.pos+end])
	p.line += strings.Count(s, "\n")
	p.pos += end + len(closing)
	return s, nil
}

var simpleEscapes = map[byte]byte{
	'a': '\a', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v',
	'\\': '\\', '"': '"', '\'': '\'', '\n': '\n',
}

func (p *parser) quoted() (string, error) {
	quote := p.src[p.pos]
	p.pos++
	var b strings.Builder
	for {
		if p.pos >= len(p.src) {
			return "", p.errorf("unclosed string")
		}
		c := p.src[p.pos]
		switch {
		case c == quote:
			p.pos++
			return b.String(), nil
		case c == '\n':
			return "", p.errorf("newline in string")
		case c != '\\':
			b.WriteByte(c)
			p.pos++
			continue
		}
		p.pos++
		if p.pos >= len(p.src) {
			return "", p.errorf("unclosed string")
		}
		e := p.src[p.pos]
		if r, ok := simpleEscapes[e]; ok {
			if e == '\n' {
				p.line++
			}
			b.WriteByte(r)
			p.pos++
			continue
		}
		if e == '\r' {
			p.pos++
			if p.consume('\n') {
				p.line++
			}
			b.WriteByte('\n')
			continue
		}
		if e >= '0' && e <= '9' {
			n, digits := 0, 0
			for digits < 3 && p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
				n = n*10 + int(p.src[p.pos]-'0')
				p.pos++
				digits++
			}
			if n > 255 {
				return "", p.errorf("escape \\%d out of range", n)
			}
			b.WriteByte(byte(n))
			continue
		}
		return "", p.errorf("invalid escape \\%c", e)
	}
}

func (p *parser) number() (any, error) {
	start := p.pos
	p.consume('-')
	if p.pos < len(p.src) && isNameStart(p.src[p.pos]) {
		word, _ := p.name()
		if word == "inf" || word == "nan" {
			p.skipNaNSuffix()
			return nil, nil
		}
		return nil, p.errorf("unexpected word %q", word)
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if isNameChar(c) || c == '.' || c == '#' ||
			((c == '+' || c == '-') && (p.src[p.pos-1] == 'e' || p.src[p.pos-1] == 'E')) {
			p.pos++
			continue
		}
		break
	}
	text := string(p.src[start:p.pos])
	if strings.Contains(text, "#") {
		return nil, nil
	}
	neg := strings.HasPrefix(text, "-")
	digits := strings.TrimPrefix(text, "-")
	if strings.HasPrefix(digits, "0x") || strings.HasPrefix(digits, "0X") {
		n, err := strconv.ParseUint(digits[2:], 16, 64)
		if err != nil {
			return nil, p.errorf("bad number %q", text)
		}
		if neg {
			return -int64(n), nil
		}
		return int64(n), nil
	}
	if !strings.ContainsAny(digits, ".eE") {
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			return n, nil
		}
	}
	f, err := strconv.ParseFloat(text, 64)
	if math.IsInf(f, 0) {
		return nil, nil
	}
	if err != nil {
		return nil, p.errorf("bad number %q", text)
	}
	return f, nil
}

func (p *parser) skipNaNSuffix() {
	if p.pos < len(p.src) && p.src[p.pos] == '(' {
		if end := strings.IndexByte(string(p.src[p.pos:]), ')'); end > 0 && end < 10 {
			p.pos += end + 1
		}
	}
}

func ToJSON(v any) any {
	t, ok := v.(*Table)
	if !ok {
		return v
	}
	if t.IsArray() {
		out := make([]any, len(t.Fields))
		for _, f := range t.Fields {
			out[f.Key.(int64)-1] = ToJSON(f.Value)
		}
		return out
	}
	out := make(map[string]any, len(t.Fields))
	for _, f := range t.Fields {
		out[keyString(f.Key)] = ToJSON(f.Value)
	}
	return out
}

func keyString(k any) string {
	switch k := k.(type) {
	case string:
		return k
	case int64:
		return strconv.FormatInt(k, 10)
	case float64:
		return strconv.FormatFloat(k, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(k)
	}
	return fmt.Sprint(k)
}
