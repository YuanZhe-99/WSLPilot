package main

import (
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
