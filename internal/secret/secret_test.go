package secret

import (
	"runtime"
	"strings"
	"testing"
)

func TestRoundTripNeverPlain(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI only")
	}
	p, err := Protect("correct horse battery")
	if err != nil || p == "" || strings.Contains(p, "horse") {
		t.Fatalf("%q %v", p, err)
	}
	s, err := Unprotect(p)
	if err != nil || s != "correct horse battery" {
		t.Fatalf("%q %v", s, err)
	}
	if _, err := Unprotect(p[:len(p)-8] + "AAAAAAA="); err == nil {
		t.Error("tampered value accepted")
	}
}
