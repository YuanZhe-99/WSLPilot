package wtprofile

import (
	"path/filepath"
	"regexp"
	"testing"
)

func TestFragmentPutRemove(t *testing.T) {
	f := &Fragment{Path: filepath.Join(t.TempDir(), "Fragments", "WSLPilot", "wslpilot.json")}
	p := ContainerShell("web", "")
	if err := f.Put(p); err != nil {
		t.Fatal(err)
	}
	p.Commandline = "wslc.exe exec -it web bash"
	f.Put(p)
	f.Put(DistroShell("Debian", "root", "/srv"))
	ps, _ := f.List()
	if len(ps) != 2 || ps[1].Commandline != "wslc.exe exec -it web bash" {
		t.Fatalf("profiles %+v", ps)
	}
	f.Remove(p.GUID)
	if ps, _ := f.List(); len(ps) != 1 {
		t.Errorf("after remove %+v", ps)
	}
}

func TestGUIDStable(t *testing.T) {
	a, b := GUIDFor("container:web"), GUIDFor("container:web")
	if a != b || !regexp.MustCompile(`^\{[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\}$`).MatchString(a) {
		t.Errorf("guid %s %s", a, b)
	}
}
