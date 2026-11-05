package vhdx

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestScriptQuotesPath(t *testing.T) {
	p := `C:\Users\Zhe Yuan\wsl\ext4.vhdx`
	if runtime.GOOS != "windows" {
		p = "/tmp/a b/ext4.vhdx"
	}
	s, err := Script(p)
	if err != nil || !strings.Contains(s, `select vdisk file="`+p+`"`) || !strings.Contains(s, "attach vdisk readonly") {
		t.Fatalf("%v\n%s", err, s)
	}
	if _, err := Script(`C:\bad"name.vhdx`); err == nil {
		t.Error("quote accepted")
	}
	if _, err := Script(`relative.vhdx`); err == nil {
		t.Error("relative path accepted")
	}
}

func TestCompactRunsElevatedScript(t *testing.T) {
	p := filepath.Join(t.TempDir(), "disk.vhdx")
	os.WriteFile(p, make([]byte, 100), 0o644)
	var gotProg string
	var gotArgs []string
	res, err := Compact(context.Background(), func(ctx context.Context, program string, args []string) (int, error) {
		gotProg, gotArgs = program, args
		os.WriteFile(p, make([]byte, 10), 0o644)
		return 0, nil
	}, p, nil)
	if err != nil || res.Before != 100 || res.After != 10 {
		t.Fatalf("%v %+v", err, res)
	}
	if gotProg != "cmd.exe" || !strings.Contains(gotArgs[2], "diskpart.exe /s") {
		t.Errorf("%s %q", gotProg, gotArgs)
	}
}
