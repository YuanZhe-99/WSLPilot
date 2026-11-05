package wsl

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/YuanZhe-99/WSLPilot/internal/procexec"
)

func fixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + path)
	if err != nil {
		t.Fatal(err)
	}
	return procexec.Decode(b)
}

func TestParseVersion(t *testing.T) {
	want := Version{WSL: "3.0.3.0", Kernel: "6.18.54.1-1", WSLg: "1.0.79", Windows: "10.0.26300.9711"}
	for _, path := range []string{
		"3.0.3.0/version-en-utf16.txt",
		"3.0.3.0/version-en-utf8.txt",
		"synthetic/version-zh-utf16.txt", // localized labels, written by hand
	} {
		t.Run(path, func(t *testing.T) {
			got, err := ParseVersion(fixture(t, path))
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestParseVersionNoVersion(t *testing.T) {
	if _, err := ParseVersion("Usage: wsl.exe [Argument]\n"); err != ErrNoVersion {
		t.Errorf("err = %v, want ErrNoVersion", err)
	}
}

func TestParseNames(t *testing.T) {
	got := ParseNames(fixture(t, "3.0.3.0/list-quiet-utf16.txt"))
	if !slices.Equal(got, []string{"Debian"}) {
		t.Errorf("got %q", got)
	}
}

func TestNamesNoDistributions(t *testing.T) {
	c := &Client{Run: func(context.Context, string, ...string) (string, error) {
		return "", &procexec.ExitError{Name: Exe, Code: -1}
	}}
	names, err := c.Names(context.Background())
	if err != nil || names != nil {
		t.Errorf("names %q, err %v; want none and no error", names, err)
	}
}
