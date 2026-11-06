package core

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/compose"
	"github.com/YuanZhe-99/WSLPilot/internal/volume"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

const liveProjectCompose = `
services:
  app:
    image: alpine:3.20
    command: ["sh", "-c", "echo hi > /v/f && chown 1234:1234 /v/f && echo bound > /b/x && sleep 3600"]
    volumes:
      - data:/v
      - ./bind:/b
    stop_grace_period: 1s
volumes:
  data: {}
`

// TestLiveProjectBackup backs up a running wslpilot-test-* project (a
// named volume and a bind folder) into an archive, checks it stopped and
// started again, and restores both parts. Run with WSLPILOT_LIVE=1.
func TestLiveProjectBackup(t *testing.T) {
	if os.Getenv("WSLPILOT_LIVE") != "1" {
		t.Skip("set WSLPILOT_LIVE=1 to run against wslc.exe")
	}
	const name, restored = "wslpilot-test-pbk", "wslpilot-test-pbk-restored"
	t.Setenv("WSLPILOT_DATA", t.TempDir())
	k, err := New()
	if err != nil {
		t.Fatal(err)
	}
	k.WireCompose(false)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bind"), 0o755)
	file := filepath.Join(dir, "compose.yaml")
	os.WriteFile(file, []byte(liveProjectCompose), 0o644)
	p, err := compose.NewProject(name, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Projects.Save(p); err != nil {
		t.Fatal(err)
	}
	b, _ := k.Compose.For(p)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := b.Down(ctx, p, compose.DownOptions{RemoveVolumes: true}, nil); err != nil {
			t.Errorf("cleanup: %v", err)
		}
		k.WSLc.RemoveVolumes(ctx, restored)
	})
	if err := b.Up(ctx, p, compose.UpOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, "bind", "x")); err == nil {
			break
		}
		if i > 60 {
			t.Fatal("the service did not write its files")
		}
		time.Sleep(time.Second)
	}

	it, err := k.backupProject(ctx, name, &backup.ArchiveWriter{Dir: filepath.Join(dir, "bk"), Compression: backup.CompressZstd}, backup.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !it.Consistent {
		t.Error("a stopped-first backup is labeled inconsistent")
	}
	ps, _ := b.Ps(ctx, p)
	if len(ps) != 1 || ps[0].State != "running" {
		t.Errorf("not running again: %+v", ps)
	}
	m, err := backup.ReadProjectManifest(ctx, it)
	if err != nil || m == nil || len(m.Folders) != 1 || len(m.Volumes) != 1 {
		t.Fatalf("manifest %+v %v", m, err)
	}

	out := filepath.Join(dir, "restored")
	if _, err := backup.ExtractProject(ctx, it, out); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(out, "bind", "x")); err != nil || string(b) != "bound\n" {
		t.Errorf("bind file %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(out, "compose.yaml")); err != nil {
		t.Error(err)
	}

	if err := k.Backup.RestoreProjectVolume(ctx, it, m.Volumes[0].Name, restored, volume.ImportOptions{Create: true}); err != nil {
		t.Fatal(err)
	}
	id, err := k.WSLc.Create(ctx, wslc.ContainerSpec{Name: restored + "-c", Image: volume.DefaultHelperImage,
		Command: []string{"sh", "-c", "cat /v/f && stat -c %u /v/f"},
		Mounts:  []wslc.MountSpec{{Type: "volume", Source: restored, Target: "/v"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer k.WSLc.Remove(context.Background(), true, false, id)
	var logs bytes.Buffer
	k.WSLc.Start(ctx, id)
	k.WSLc.Logs(ctx, id, wslc.LogOptions{Follow: true}, &logs)
	if got := strings.Fields(logs.String()); len(got) != 2 || got[0] != "hi" || got[1] != "1234" {
		t.Errorf("restored volume: %q", logs.String())
	}
}
