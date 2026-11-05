// Command catalog merges locales/messages.tsv into the ARB catalogs
// locales/en.json and locales/zh-Hans.json. Each line of the TSV is
// Key<TAB>English<TAB>Chinese; "{name}" is a text placeholder and
// "{name:int}" a number. Run from the repository root, then
// `go tool mygo generate`:
//
//	go run ./tools/catalog
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// entry is one message of a catalog, in file order.
type entry struct {
	key   string
	value json.RawMessage
}

var placeholder = regexp.MustCompile(`\{([a-zA-Z]+)(:int)?\}`)

func main() {
	rows, err := readTSV("locales/messages.tsv")
	if err != nil {
		fail(err)
	}
	for _, c := range []struct {
		file string
		col  int
		meta bool
	}{{"locales/en.json", 1, true}, {"locales/zh-Hans.json", 2, false}} {
		es, err := readCatalog(c.file)
		if err != nil {
			fail(err)
		}
		for _, r := range rows {
			text, types := expand(r[c.col])
			es = set(es, r[0], mustJSON(text))
			if c.meta && len(types) > 0 {
				ph := map[string]map[string]string{}
				for name, t := range types {
					ph[name] = map[string]string{"type": t}
				}
				es = set(es, "@"+r[0], mustJSON(map[string]any{"placeholders": ph}))
			}
		}
		if err := writeCatalog(c.file, es); err != nil {
			fail(err)
		}
	}
	fmt.Printf("catalog: %d messages merged\n", len(rows))
}

// expand turns {name:int} into {name} and returns the placeholder types.
func expand(s string) (string, map[string]string) {
	types := map[string]string{}
	out := placeholder.ReplaceAllStringFunc(s, func(m string) string {
		sub := placeholder.FindStringSubmatch(m)
		t := "String"
		if sub[2] != "" {
			t = "int"
		}
		types[sub[1]] = t
		return "{" + sub[1] + "}"
	})
	return out, types
}

func readTSV(path string) ([][3]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows [][3]string
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := strings.Split(line, "\t")
		if len(p) != 3 {
			return nil, fmt.Errorf("%s:%d: want 3 tab-separated fields, have %d", path, n, len(p))
		}
		if seen[p[0]] {
			return nil, fmt.Errorf("%s:%d: duplicate key %s", path, n, p[0])
		}
		seen[p[0]] = true
		_, te := expand(p[1])
		_, tz := expand(p[2])
		if fmt.Sprint(te) != fmt.Sprint(tz) {
			return nil, fmt.Errorf("%s:%d: placeholders differ between languages", path, n)
		}
		rows = append(rows, [3]string{p[0], p[1], p[2]})
	}
	return rows, sc.Err()
}

func readCatalog(path string) ([]entry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	var es []entry
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		es = append(es, entry{t.(string), v})
	}
	return es, nil
}

func set(es []entry, key string, v json.RawMessage) []entry {
	for i := range es {
		if es[i].key == key {
			es[i].value = v
			return es
		}
	}
	return append(es, entry{key, v})
}

func writeCatalog(path string, es []entry) error {
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, e := range es {
		var v bytes.Buffer
		json.Indent(&v, e.value, "  ", "  ")
		k, _ := json.Marshal(e.key)
		fmt.Fprintf(&b, "  %s: %s", k, v.String())
		if i < len(es)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func mustJSON(v any) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	return bytes.TrimSpace(b.Bytes())
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "catalog:", err)
	os.Exit(1)
}
