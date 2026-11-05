package store

import (
	"errors"
	"io/fs"
	"testing"
)

type doc struct{ Name string }

func TestRoundTripAndNewerSchema(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if _, err := s.Load("a/b.json", 1, &doc{}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	if err := s.Save("a/b.json", 1, doc{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	var d doc
	if v, err := s.Load("a/b.json", 1, &d); err != nil || v != 1 || d.Name != "x" {
		t.Fatalf("load: %v %d %+v", err, v, d)
	}
	s.Save("a/c.json", 2, doc{Name: "new"})
	d = doc{}
	if _, err := s.Load("a/c.json", 1, &d); !errors.Is(err, ErrNewerSchema) || d.Name != "" {
		t.Fatalf("newer: %v %+v", err, d)
	}
	names, _ := s.List("a")
	if len(names) != 2 {
		t.Fatalf("list: %v (temporary files must not remain)", names)
	}
}
