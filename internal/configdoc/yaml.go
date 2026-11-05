package configdoc

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// YAML is a line-preserving editor for a YAML document of nested mappings
// with scalar leaves, the shape of WSLc's settings.yaml.
// Notes: `wslc settings` writes every setting commented out
// ("#   cpuCount: default"); Set uncomments such a line instead of adding a
// second one, so the template's layout survives.
type YAML struct {
	lines []string
	eol   string
}

// ParseYAML parses text; Validate reports syntax errors.
func ParseYAML(text string) *YAML {
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var lines []string
	if text != "" {
		lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}
	return &YAML{lines: lines, eol: eol}
}

// String renders the document.
func (y *YAML) String() string {
	if len(y.lines) == 0 {
		return ""
	}
	return strings.Join(y.lines, y.eol) + y.eol
}

// Validate parses the document with a full YAML parser.
func (y *YAML) Validate() error {
	var v any
	if err := yaml.Unmarshal([]byte(y.String()), &v); err != nil {
		return err
	}
	return nil
}

// yline is one mapping line: its indentation, key, value and whether it is
// commented out.
type yline struct {
	indent    int
	key, val  string
	commented bool
	ok        bool
}

func parseYLine(l string) yline {
	t := strings.TrimLeft(l, " ")
	ind := len(l) - len(t)
	commented := false
	if strings.HasPrefix(t, "#") {
		commented = true
		t = strings.TrimLeft(strings.TrimPrefix(t, "#"), " ")
	}
	c := strings.Index(t, ":")
	if c <= 0 || strings.ContainsAny(t[:c], " #\"'") {
		return yline{}
	}
	val := strings.TrimSpace(t[c+1:])
	if i := strings.Index(val, " #"); i >= 0 && !strings.HasPrefix(val, "\"") && !strings.HasPrefix(val, "'") {
		val = strings.TrimSpace(val[:i])
	}
	return yline{indent: ind, key: t[:c], val: val, commented: commented, ok: true}
}

// locate finds the line of a dotted path ("session.cpuCount").
// Returns: the active line index (or -1), a commented-out candidate (or
// -1), and the index of the parent mapping line (-1 for the root or when
// the parent is absent).
func (y *YAML) locate(path string) (active, commented, parent int) {
	parts := strings.Split(path, ".")
	active, commented, parent = -1, -1, -1
	var stack []yline // active ancestors by indentation
	var cstack []yline
	for i, l := range y.lines {
		p := parseYLine(l)
		if !p.ok {
			continue
		}
		st := &stack
		if p.commented {
			st = &cstack
			// a commented key may sit under an active or commented parent
			*st = append([]yline(nil), stack...)
		}
		for len(*st) > 0 && (*st)[len(*st)-1].indent >= p.indent {
			*st = (*st)[:len(*st)-1]
		}
		depth := len(*st)
		match := depth < len(parts) && p.key == parts[depth]
		for k := 0; k < depth && match; k++ {
			match = (*st)[k].key == parts[k]
		}
		if match && depth == len(parts)-1 {
			if p.commented {
				if commented < 0 {
					commented = i
				}
			} else {
				active = i
			}
		} else if match && !p.commented && depth == len(parts)-2 {
			parent = i
		}
		*st = append(*st, p)
	}
	return
}

// Get returns the value at path, ignoring commented-out lines.
func (y *YAML) Get(path string) (string, bool) {
	a, _, _ := y.locate(path)
	if a < 0 {
		return "", false
	}
	return parseYLine(y.lines[a]).val, true
}

// Set gives path the scalar value, editing a single line.
// Notes: order of preference: the active line; a commented-out line,
// uncommented; a new line after the parent's last child; a new parent
// mapping at the end.
func (y *YAML) Set(path, value string) {
	a, c, parent := y.locate(path)
	parts := strings.Split(path, ".")
	key := parts[len(parts)-1]
	switch {
	case a >= 0:
		y.lines[a] = replaceValue(y.lines[a], value)
	case c >= 0:
		p := parseYLine(y.lines[c])
		y.lines[c] = strings.Repeat(" ", indentFor(len(parts), p.indent)) + key + ": " + value
	case parent >= 0 || len(parts) == 1:
		ind := 0
		insert := len(y.lines)
		if parent >= 0 {
			pi := parseYLine(y.lines[parent]).indent
			ind = pi + 2
			insert = parent + 1
			for i := parent + 1; i < len(y.lines); i++ {
				p := parseYLine(y.lines[i])
				if strings.TrimSpace(y.lines[i]) == "" {
					continue
				}
				if p.ok && p.indent <= pi {
					break
				}
				insert = i + 1
			}
		}
		line := strings.Repeat(" ", ind) + key + ": " + value
		y.lines = append(y.lines[:insert], append([]string{line}, y.lines[insert:]...)...)
	default:
		y.Set(strings.Join(parts[:len(parts)-1], "."), "")
		y.Set(path, value)
	}
}

// indentFor keeps a commented line's indentation, but at least 2 per level.
func indentFor(levels, have int) int {
	if levels <= 1 {
		return 0
	}
	if have < 2*(levels-1) {
		return 2 * (levels - 1)
	}
	return have
}

// Unset comments nothing out: it removes the active line at path.
func (y *YAML) Unset(path string) {
	if a, _, _ := y.locate(path); a >= 0 {
		y.lines = append(y.lines[:a], y.lines[a+1:]...)
	}
}

// replaceValue swaps the scalar after "key:" keeping a trailing comment.
func replaceValue(line, value string) string {
	c := strings.Index(line, ":")
	rest := line[c+1:]
	comment := ""
	if i := strings.Index(rest, " #"); i >= 0 {
		comment = rest[i:]
	}
	return line[:c+1] + " " + value + comment
}

// QuoteYAML renders s as a YAML scalar, quoting when needed.
func QuoteYAML(s string) string {
	if s == "" || strings.ContainsAny(s, ":#'\"{}[],&*!|>%@`") || strings.TrimSpace(s) != s {
		return fmt.Sprintf("%q", s)
	}
	return s
}
