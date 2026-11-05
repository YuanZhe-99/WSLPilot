package wsl

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/YuanZhe-99/WSLPilot/internal/procexec"
)

func TestParseListVerbose(t *testing.T) {
	cases := map[string][]VerboseRow{
		"3.0.3.0/list-verbose-utf16.txt": {
			{Name: "Debian", Default: true, Version: 2},
			{Name: "wslpilot-test-d1", Version: 2},
		},
		"synthetic/list-verbose-zh.txt": {
			{Name: "Debian", Default: true, Version: 2},
			{Name: "wslpilot-test-d1", Version: 2},
			{Name: "legacy", Version: 1},
		},
	}
	for path, want := range cases {
		got := ParseListVerbose(fixture(t, path))
		if !slices.Equal(got, want) {
			t.Errorf("%s: got %+v, want %+v", path, got, want)
		}
	}
}

func TestParseListOnline(t *testing.T) {
	got := ParseListOnline(fixture(t, "3.0.3.0/list-online-utf16.txt"))
	if len(got) < 10 {
		t.Fatalf("only %d entries: %+v", len(got), got)
	}
	if got[0] != (OnlineDistro{Name: "Ubuntu", FriendlyName: "Ubuntu"}) {
		t.Errorf("first = %+v", got[0])
	}
	for _, d := range got {
		if d.Name == "Ubuntu-24.04" && d.FriendlyName != "Ubuntu 24.04 LTS" {
			t.Errorf("friendly name = %q", d.FriendlyName)
		}
		if d.Name == "NAME" {
			t.Error("header parsed as an entry")
		}
	}
}

func TestParseDF(t *testing.T) {
	out := "Filesystem     1024-blocks    Used Available Capacity Mounted on\n/dev/sde 1055762868 206720 1001852676 0% /\n"
	got, err := ParseDF(out)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedBytes != 206720*1024 || got.SizeBytes != 1055762868*1024 {
		t.Errorf("got %+v", got)
	}
}

func TestInstallArgsAlwaysNoLaunch(t *testing.T) {
	got := InstallArgs("Ubuntu", InstallOptions{Name: "u2", Location: `D:\My WSL\u2`, Version: 2, WebDownload: true})
	want := []string{"--install", "Ubuntu", "--no-launch", "--name", "u2", "--location", `D:\My WSL\u2`, "--version", "2", "--web-download"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q", got)
	}
}

// fakeWSL serves recorded output per argument vector.
func fakeWSL(t *testing.T, outputs map[string]string) *Client {
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		key := strings.Join(args, " ")
		out, ok := outputs[key]
		if !ok {
			t.Fatalf("unexpected command: %s %s", name, key)
		}
		return out, nil
	}
	return &Client{
		Run: run,
		Stream: func(ctx context.Context, s procexec.Spec) error {
			out, err := run(ctx, s.Name, s.Args...)
			if s.Stdout != nil {
				io.WriteString(s.Stdout, out)
			}
			return err
		},
		ReadRegistry: func() []RegEntry {
			return []RegEntry{{GUID: "{g}", Name: "Debian", BasePath: `C:\wsl\debian`, VHDFileName: "ext4.vhdx", Flavor: "debian", DefaultUID: 1000}}
		},
	}
}

func TestListCombinesSources(t *testing.T) {
	c := fakeWSL(t, map[string]string{
		"--list --verbose":         fixture(t, "3.0.3.0/list-verbose-utf16.txt"),
		"--list --running --quiet": "Debian\n",
	})
	ds, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 {
		t.Fatalf("got %d distros", len(ds))
	}
	d := ds[0]
	if !d.Running || !d.Default || d.Flavor != "debian" || d.DefaultUID != 1000 || !strings.HasSuffix(d.VHDPath, "ext4.vhdx") {
		t.Errorf("debian = %+v", d)
	}
	if ds[1].Running || ds[1].GUID != "" {
		t.Errorf("test distro = %+v", ds[1])
	}
}

func TestExecArgsNoShell(t *testing.T) {
	got := ExecArgs("my distro", "root", "cat", "/etc/wsl.conf")
	want := []string{"--distribution", "my distro", "--user", "root", "--exec", "cat", "/etc/wsl.conf"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q", got)
	}
}
