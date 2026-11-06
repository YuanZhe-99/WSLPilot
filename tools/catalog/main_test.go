package main

import (
	"fmt"
	"strings"
	"testing"

	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// Regression: placeholders were written as a Go map, so JSON sorted them
// and the generated accessors swapped arguments ("after → before").
func TestPlaceholderOrderKept(t *testing.T) {
	got := string(placeholderMeta("Disk size: {before} → {after}, {n:int} files"))
	if strings.Index(got, `"before"`) > strings.Index(got, `"after"`) || !strings.Contains(got, `"n":{"type":"int"}`) {
		t.Fatalf("meta %s", got)
	}
	if s := L.In("en").WSLCompacted("10 GB", "2 GB"); s != "Disk size: 10 GB → 2 GB" {
		t.Errorf("generated accessor: %q", s)
	}
}

// A plural's count is a number in both languages, so a row with a plural
// in English and {count:int} in Chinese is accepted, and kept as written.
func TestPluralCountsAsInt(t *testing.T) {
	en, te := expand("{count, plural, one {# image} other {# images}} · {name}")
	_, tz := expand("{count:int} 个镜像 · {name}")
	if fmt.Sprint(te) != fmt.Sprint(tz) || te["count"] != "int" || !strings.HasPrefix(en, "{count, plural,") {
		t.Errorf("en %q %v, zh %v", en, te, tz)
	}
	if got := string(placeholderMeta("{n, plural, one {# x} other {# xs}}")); !strings.Contains(got, `"n":{"type":"int"}`) {
		t.Errorf("meta %s", got)
	}
	if s := L.In("en").DashContainersN(1); s != "1 container" {
		t.Errorf("generated accessor: %q", s)
	}
}
