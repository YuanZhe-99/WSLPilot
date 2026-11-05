// Package configdoc edits configuration files as raw documents: a change
// rewrites only the line it concerns, comments and unknown keys stay as the
// user wrote them, and a save is refused when the file changed on disk since
// it was read. It covers the INI dialect of .wslconfig and wsl.conf and the
// two-level YAML of WSLc's settings.yaml, plus the key schemas behind the
// settings forms (schema.go).
package configdoc

import (
	"strings"
)

// INI is a parsed INI document that keeps every original line.
// Notes: WSL's parser treats section and key names case-insensitively
// (Microsoft's own example writes "localhostforwarding"), so lookups do too;
// lines starting with "#" or ";" are comments.
type INI struct {
	lines []string
	eol   string
}

// ParseINI parses text. It never fails: lines it cannot read are kept and
// reported by Problems.
func ParseINI(text string) *INI {
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var lines []string
	if text != "" {
		lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}
	return &INI{lines: lines, eol: eol}
}

// String renders the document with its original line endings.
func (d *INI) String() string {
	if len(d.lines) == 0 {
		return ""
	}
	return strings.Join(d.lines, d.eol) + d.eol
}

// lineKind classifies one line.
type lineKind int

const (
	blankLine lineKind = iota
	commentLine
	sectionLine
	keyLine
	badLine
)

// classify returns the kind of line and, for sections and keys, the
// lower-cased name and the value.
func classify(line string) (k lineKind, name, value string) {
	t := strings.TrimSpace(line)
	switch {
	case t == "":
		return blankLine, "", ""
	case strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";"):
		return commentLine, "", ""
	case strings.HasPrefix(t, "["):
		end := strings.Index(t, "]")
		if end < 0 {
			return badLine, "", ""
		}
		return sectionLine, strings.ToLower(strings.TrimSpace(t[1:end])), ""
	}
	eq := strings.Index(t, "=")
	if eq <= 0 {
		return badLine, "", ""
	}
	return keyLine, strings.ToLower(strings.TrimSpace(t[:eq])), strings.TrimSpace(t[eq+1:])
}

// find returns the line index of section.key (-1 if absent) and the index
// after the last line belonging to the section (-1 if the section is
// absent).
func (d *INI) find(section, key string) (at, sectionEnd int) {
	section, key = strings.ToLower(section), strings.ToLower(key)
	at, sectionEnd = -1, -1
	cur := ""
	lastContent := -1
	for i, l := range d.lines {
		k, name, _ := classify(l)
		switch k {
		case sectionLine:
			if cur == section && lastContent >= 0 {
				sectionEnd = lastContent + 1
			}
			cur = name
			if cur == section {
				lastContent = i
			}
		case keyLine:
			if cur == section {
				lastContent = i
				if name == key {
					at = i
				}
			}
		}
	}
	if cur == section && lastContent >= 0 {
		sectionEnd = lastContent + 1
	}
	return at, sectionEnd
}

// Get returns the value of section.key as written (quotes kept).
// Notes: when a key repeats, the last occurrence wins, as in WSL.
func (d *INI) Get(section, key string) (string, bool) {
	at, _ := d.find(section, key)
	if at < 0 {
		return "", false
	}
	_, _, v := classify(d.lines[at])
	return v, true
}

// Set gives section.key the value, editing only that line.
// Notes: an existing line keeps its key spelling, indentation and the
// spacing around "="; a new key goes after the section's last key; a new
// section is appended at the end, after a blank line.
func (d *INI) Set(section, key, value string) {
	at, end := d.find(section, key)
	if at >= 0 {
		l := d.lines[at]
		eq := strings.Index(l, "=")
		rest := l[eq+1:]
		lead := rest[:len(rest)-len(strings.TrimLeft(rest, " \t"))]
		d.lines[at] = l[:eq+1] + lead + value
		return
	}
	line := key + "=" + value
	if end >= 0 {
		d.lines = append(d.lines[:end], append([]string{line}, d.lines[end:]...)...)
		return
	}
	if n := len(d.lines); n > 0 && strings.TrimSpace(d.lines[n-1]) != "" {
		d.lines = append(d.lines, "")
	}
	d.lines = append(d.lines, "["+section+"]", line)
}

// Unset removes every occurrence of section.key.
func (d *INI) Unset(section, key string) {
	for {
		at, _ := d.find(section, key)
		if at < 0 {
			return
		}
		d.lines = append(d.lines[:at], d.lines[at+1:]...)
	}
}

// Entry is one key present in the document.
type Entry struct {
	Section, Key, Value string
	Line                int // 1-based
}

// Entries lists the keys in document order (names lower-cased).
func (d *INI) Entries() []Entry {
	var res []Entry
	cur := ""
	for i, l := range d.lines {
		k, name, v := classify(l)
		switch k {
		case sectionLine:
			cur = name
		case keyLine:
			res = append(res, Entry{Section: cur, Key: name, Value: v, Line: i + 1})
		}
	}
	return res
}

// Problems lists the 1-based numbers of lines that are neither blank,
// comments, sections nor key=value, and keys outside any section.
func (d *INI) Problems() []int {
	var bad []int
	cur := ""
	for i, l := range d.lines {
		k, name, _ := classify(l)
		switch {
		case k == badLine, k == keyLine && cur == "":
			bad = append(bad, i+1)
		case k == sectionLine:
			cur = name
		}
	}
	return bad
}

// Unquote strips one pair of surrounding double quotes.
func Unquote(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return v
}
