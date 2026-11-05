package configdoc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = "# my settings\r\n[wsl2]\r\nmemory = 4GB  \r\n# keep this\r\nlocalhostforwarding=true\r\n\r\n[experimental]\r\nsparseVhd=true\r\nunknownKey=1\r\n"

func TestINIEditsOnlyChangedLines(t *testing.T) {
	d := ParseINI(sample)
	if v, _ := d.Get("WSL2", "Memory"); v != "4GB" {
		t.Fatalf("get = %q", v)
	}
	d.Set("wsl2", "memory", "8GB")
	d.Set("wsl2", "processors", "4")
	d.Set("general", "instanceIdleTimeout", "-1")
	d.Unset("experimental", "sparseVhd")
	want := "# my settings\r\n[wsl2]\r\nmemory = 8GB\r\n# keep this\r\nlocalhostforwarding=true\r\nprocessors=4\r\n\r\n[experimental]\r\nunknownKey=1\r\n\r\n[general]\r\ninstanceIdleTimeout=-1\r\n"
	if got := d.String(); got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
	if n := len(d.Entries()); n != 5 {
		t.Errorf("entries = %d", n)
	}
}

func TestINIProblems(t *testing.T) {
	d := ParseINI("orphan=1\n[ok]\nnot a key\n")
	if p := d.Problems(); len(p) != 2 || p[0] != 1 || p[1] != 3 {
		t.Errorf("problems = %v", p)
	}
}

func TestApplyValidatesFirst(t *testing.T) {
	d := ParseINI("")
	mem := WSLConfig[0]
	err := ApplyINI(d, []Change{{Field: mem, New: "8GB"}, {Field: WSLConfig[1], New: "four"}})
	if err == nil || d.String() != "" {
		t.Fatalf("invalid draft applied: %v %q", err, d.String())
	}
	kernel := WSLConfig[13]
	if err := ApplyINI(d, []Change{{Field: kernel, New: `C:\k\my kernel`}}); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.Get("wsl2", "kernel"); v != `C:\\k\\my kernel` || kernel.Decode(v) != `C:\k\my kernel` {
		t.Errorf("kernel = %q", v)
	}
}

const template = `# WSL container settings
session:
  # Number of CPUs
  # cpuCount: default
  memorySize: default # comment
#credentialStore: default
`

func TestYAMLUncommentsTemplate(t *testing.T) {
	y := ParseYAML(template)
	if v, _ := y.Get("session.memorySize"); v != "default" {
		t.Fatalf("get = %q", v)
	}
	if err := ApplyYAML(y, []Change{
		{Field: WSLcSettings[0], New: "4"},
		{Field: WSLcSettings[1], New: "8GB"},
		{Field: WSLcSettings[6], New: "60"},
		{Field: WSLcSettings[7], New: "file"},
	}); err != nil {
		t.Fatal(err)
	}
	want := `# WSL container settings
session:
  # Number of CPUs
  cpuCount: 4
  memorySize: 8GB # comment
  idleTimeout: 60
credentialStore: file
`
	if got := y.String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestYAMLNewParent(t *testing.T) {
	y := ParseYAML("")
	y.Set("session.cpuCount", "2")
	if got := y.String(); got != "session: \n  cpuCount: 2\n" && got != "session:\n  cpuCount: 2\n" {
		t.Errorf("got %q", got)
	}
	if err := y.Validate(); err != nil {
		t.Error(err)
	}
}

func TestSaveConflictAndBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "My Dir", ".wslconfig")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("[wsl2]\n"), 0o644)
	s, _ := ReadFile(p)
	os.WriteFile(p, []byte("[wsl2]\nmemory=1GB\n"), 0o644)
	if _, err := s.Save("x"); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict not detected: %v", err)
	}
	s, _ = ReadFile(p)
	bak, err := s.Save("[wsl2]\nmemory=2GB\n")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(bak)
	if !strings.Contains(string(b), "1GB") {
		t.Errorf("backup = %q", b)
	}
	missing, _ := ReadFile(filepath.Join(t.TempDir(), "none"))
	if missing.Exists {
		t.Error("missing file exists")
	}
}

func TestDiffLines(t *testing.T) {
	got := DiffLines("a\nb\nc\n", "a\nB\nc\nd\n")
	want := []string{"- b", "+ B", "+ d"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("diff = %q", got)
	}
}
