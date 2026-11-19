package build

import (
	"fmt"
	"strings"
)

// A small reader for the part of YAML a pipeline file needs: mappings,
// lists, plain and quoted text, literal blocks (|) and comments. CirrusUX
// uses only the standard library, so this replaces a YAML package. What it
// reads, it reads the way YAML does; anything else (flow lists, anchors,
// tags, folded blocks, tabs) is refused with the line number, rather than
// read some other way.
//
// Values come back as map[string]any, []any, string, or nil for an empty
// value.

type line struct {
	n      int // 1-based, for errors
	indent int
	text   string // without the indent; comments still in
}

type yamlReader struct {
	raw   []string
	lines []line // the lines that hold something
	i     int
}

func parseYAML(src string) (any, error) {
	src = strings.TrimPrefix(src, "\ufeff")
	r := &yamlReader{raw: strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")}
	for i, s := range r.raw {
		body := strings.TrimLeft(s, " ")
		if strings.HasPrefix(body, "\t") || strings.Contains(s[:len(s)-len(body)], "\t") {
			return nil, fmt.Errorf("line %d: indent with spaces, not tabs", i+1)
		}
		t := strings.TrimSpace(body)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if len(r.lines) == 0 && t == "---" {
			continue
		}
		if t == "---" || t == "..." || strings.HasPrefix(t, "%") {
			return nil, fmt.Errorf("line %d: one document per file", i+1)
		}
		r.lines = append(r.lines, line{n: i + 1, indent: len(s) - len(body), text: strings.TrimRight(body, " ")})
	}
	if len(r.lines) == 0 {
		return nil, fmt.Errorf("the file is empty")
	}
	v, err := r.node(r.lines[0].indent)
	if err != nil {
		return nil, err
	}
	if r.i < len(r.lines) {
		return nil, fmt.Errorf("line %d: unexpected indentation", r.lines[r.i].n)
	}
	return v, nil
}

func isItem(t string) bool { return t == "-" || strings.HasPrefix(t, "- ") }

func (r *yamlReader) node(indent int) (any, error) {
	l := r.lines[r.i]
	if isItem(l.text) {
		return r.list(indent)
	}
	if _, _, ok, err := splitKey(l); err != nil {
		return nil, err
	} else if ok {
		return r.mapping(indent)
	}
	r.i++
	return scalar(l)
}

func (r *yamlReader) mapping(indent int) (any, error) {
	m := map[string]any{}
	for r.i < len(r.lines) {
		l := r.lines[r.i]
		if l.indent < indent {
			break
		}
		if l.indent > indent {
			return nil, fmt.Errorf("line %d: unexpected indentation", l.n)
		}
		if isItem(l.text) {
			// A list at the same indent as its key belongs to that key,
			// which value() reads; one here has no key.
			return nil, fmt.Errorf("line %d: a list item where a key was expected", l.n)
		}
		key, rest, ok, err := splitKey(l)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("line %d: expected key: value", l.n)
		}
		if _, dup := m[key]; dup {
			return nil, fmt.Errorf("line %d: %s is given twice", l.n, key)
		}
		r.i++
		v, err := r.value(l, indent, rest)
		if err != nil {
			return nil, err
		}
		m[key] = v
	}
	return m, nil
}

// value reads what follows "key:" on line l: text on the same line, a
// literal block, or a nested block on the lines below.
func (r *yamlReader) value(l line, indent int, rest string) (any, error) {
	if rest == "|" || rest == "|-" || rest == "|+" {
		return r.block(l, indent, rest)
	}
	if rest != "" {
		return scalar(line{n: l.n, text: rest})
	}
	if r.i >= len(r.lines) {
		return nil, nil
	}
	next := r.lines[r.i]
	switch {
	case next.indent > indent:
		return r.node(next.indent)
	case next.indent == indent && isItem(next.text):
		return r.list(indent)
	}
	return nil, nil
}

func (r *yamlReader) list(indent int) (any, error) {
	out := []any{}
	for r.i < len(r.lines) {
		l := r.lines[r.i]
		if l.indent != indent || !isItem(l.text) {
			if l.indent > indent {
				return nil, fmt.Errorf("line %d: unexpected indentation", l.n)
			}
			break
		}
		rest := strings.TrimLeft(strings.TrimPrefix(l.text, "-"), " ")
		if rest == "" || strings.HasPrefix(rest, "#") {
			r.i++
			v, err := r.value(l, indent, "")
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			continue
		}
		// "- key: value" starts a mapping at the column of "key": read it
		// as if it were on its own line there.
		col := l.indent + len(l.text) - len(rest)
		inner := line{n: l.n, indent: col, text: rest}
		if _, _, ok, err := splitKey(inner); err != nil {
			return nil, err
		} else if ok {
			r.lines[r.i] = inner
			v, err := r.mapping(col)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			continue
		}
		r.i++
		if rest == "|" || rest == "|-" || rest == "|+" {
			v, err := r.block(l, indent, rest)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			continue
		}
		v, err := scalar(inner)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// block reads a literal block (|) from the raw lines after l: every line
// indented deeper than the key, blank ones included.
func (r *yamlReader) block(l line, indent int, style string) (any, error) {
	var body []string
	blockIndent := -1
	last := l.n // raw index of the line after the header
	for j := l.n; j < len(r.raw); j++ {
		s := r.raw[j]
		t := strings.TrimLeft(s, " ")
		if t == "" {
			body = append(body, "")
			continue
		}
		ind := len(s) - len(t)
		if ind <= indent {
			break
		}
		if blockIndent < 0 {
			blockIndent = ind
		}
		if ind < blockIndent {
			return nil, fmt.Errorf("line %d: a block's lines need the same indentation as its first", j+1)
		}
		body = append(body, strings.TrimRight(s[blockIndent:], " "))
		last = j + 1
	}
	// Drop trailing blank lines that belong after the block, and skip the
	// block's lines in the line list.
	body = body[:len(body)-countTrailingBlank(body)]
	for r.i < len(r.lines) && r.lines[r.i].n <= last {
		r.i++
	}
	text := strings.Join(body, "\n")
	switch style {
	case "|":
		if text != "" {
			text += "\n"
		}
	case "|+":
		return nil, fmt.Errorf("line %d: |+ isn't supported; use |", l.n)
	}
	return text, nil
}

func countTrailingBlank(b []string) int {
	n := 0
	for i := len(b) - 1; i >= 0 && b[i] == ""; i-- {
		n++
	}
	return n
}

// splitKey finds "key:" at the start of a line, the way YAML does: a
// quoted key ends at its closing quote; a plain one at the first ": " or a
// final ":".
func splitKey(l line) (key, rest string, ok bool, err error) {
	t := l.text
	if t == "" {
		return "", "", false, nil
	}
	if t[0] == '"' || t[0] == '\'' {
		end := closingQuote(t)
		if end < 0 {
			return "", "", false, fmt.Errorf("line %d: unclosed quote", l.n)
		}
		after := strings.TrimLeft(t[end+1:], " ")
		if after != ":" && !strings.HasPrefix(after, ": ") {
			return "", "", false, nil
		}
		k, err := scalar(line{n: l.n, text: t[:end+1]})
		if err != nil {
			return "", "", false, err
		}
		return k.(string), cleanRest(after[1:]), true, nil
	}
	i := strings.Index(t, ": ")
	if i < 0 {
		if !strings.HasSuffix(t, ":") {
			return "", "", false, nil
		}
		i = len(t) - 1
	}
	// "# a: b" is a comment, and a key can't hold " #".
	if c := strings.Index(t, " #"); c >= 0 && c < i {
		return "", "", false, nil
	}
	return strings.TrimRight(t[:i], " "), cleanRest(t[i+1:]), true, nil
}

func cleanRest(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		return ""
	}
	return s
}

func closingQuote(t string) int {
	q := t[0]
	for i := 1; i < len(t); i++ {
		switch {
		case q == '"' && t[i] == '\\':
			i++
		case t[i] == q && q == '\'' && i+1 < len(t) && t[i+1] == '\'':
			i++
		case t[i] == q:
			return i
		}
	}
	return -1
}

// scalar reads one value written on a line: quoted, or plain up to a
// comment.
func scalar(l line) (any, error) {
	t := l.text
	if t == "" {
		return nil, nil
	}
	switch t[0] {
	case '"', '\'':
		end := closingQuote(t)
		if end < 0 {
			return nil, fmt.Errorf("line %d: unclosed quote", l.n)
		}
		if after := strings.TrimSpace(t[end+1:]); after != "" && !strings.HasPrefix(after, "#") {
			return nil, fmt.Errorf("line %d: text after a closing quote", l.n)
		}
		if t[0] == '\'' {
			return strings.ReplaceAll(t[1:end], "''", "'"), nil
		}
		return unescape(l, t[1:end])
	case '[', '{', '&', '*', '!', '>', '%', '@', '`':
		if t == "[]" || t == "{}" {
			if t == "[]" {
				return []any{}, nil
			}
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("line %d: %q at the start of a value isn't supported here; put the value in quotes, or write a list one item per line", l.n, t[:1])
	}
	if c := strings.Index(t, " #"); c >= 0 {
		t = strings.TrimRight(t[:c], " ")
	}
	if t == "~" || t == "null" {
		return nil, nil
	}
	return t, nil
}

func unescape(l line, s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i == len(s) {
			return "", fmt.Errorf("line %d: a lone \\ at the end of quoted text", l.n)
		}
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '"', '\\', '/':
			b.WriteByte(s[i])
		case '0':
			return "", fmt.Errorf("line %d: \\0 isn't allowed", l.n)
		default:
			return "", fmt.Errorf("line %d: unknown escape \\%c", l.n, s[i])
		}
	}
	return b.String(), nil
}
