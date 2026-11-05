package core

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/YuanZhe-99/WSLPilot/internal/elevate"
	"github.com/YuanZhe-99/WSLPilot/internal/vhdx"
	"github.com/YuanZhe-99/WSLPilot/internal/volume"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// These tests restart the whole WSLc session; they run only with
// WSLPILOT_LIVE_SESSION=1, on a machine whose session holds nothing else.

func liveCore(t *testing.T) *Core {
	if os.Getenv("WSLPILOT_LIVE_SESSION") != "1" {
		t.Skip("set WSLPILOT_LIVE_SESSION=1 (restarts the WSLc session)")
	}
	t.Setenv("WSLPILOT_DATA", t.TempDir())
	k, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// TestLiveCompactVHDVolume fills and empties a vhd volume, then compacts it
// with diskpart in an elevated child (a UAC prompt appears).
func TestLiveCompactVHDVolume(t *testing.T) {
	k := liveCore(t)
	ctx := context.Background()
	const name = "wslpilot-test-compact"
	defer k.WSLc.RemoveVolumes(context.Background(), name)
	if err := k.WSLc.CreateVolume(ctx, name, "vhd", map[string]string{"SizeBytes": "2147483648"}, nil); err != nil {
		t.Fatal(err)
	}
	id, err := k.WSLc.Create(ctx, wslc.ContainerSpec{Name: name + "-c", Image: volume.DefaultHelperImage, Command: []string{"sh", "-c", "head -c 300000000 /dev/urandom > /v/big && sync && rm /v/big && sync && fstrim /v || true"},
		Mounts: []wslc.MountSpec{{Type: "volume", Source: name, Target: "/v"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer k.WSLc.Remove(context.Background(), true, false, id)
	var out bytes.Buffer
	k.WSLc.Start(ctx, id)
	k.WSLc.Logs(ctx, id, wslc.LogOptions{Follow: true}, &out)
	vs, err := k.WSLc.InspectVolumes(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	path := vs[0].Status.HostPath
	k.WSLc.Remove(ctx, true, false, id)
	if err := k.WSLc.TerminateSession(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := vhdx.Compact(ctx, elevate.Run, path, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("compacted %s: %d -> %d bytes", filepath.Base(path), res.Before, res.After)
	if res.After >= res.Before {
		t.Errorf("no space reclaimed")
	}
}

// TestLiveMoveData moves a session holding one test image and volume to a
// temporary folder, checks them there, and puts settings.yaml back.
func TestLiveMoveData(t *testing.T) {
	k := liveCore(t)
	ctx := context.Background()
	info, err := k.WSLc.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings := info.Client.SettingsFile
	orig, origErr := os.ReadFile(settings)
	defer func() {
		if origErr == nil {
			os.WriteFile(settings, orig, 0o644)
		} else {
			os.Remove(settings)
		}
		k.WSLc.TerminateSession(context.Background())
	}()
	const vol = "wslpilot-test-move"
	if _, _, err := k.Volumes.Import(ctx, vol, bytes.NewReader(tarOf(t, "hello.txt", "moved")), volume.ImportOptions{Create: true}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "wslc data")
	res, err := k.MoveWSLcData(ctx, dest, settings, filepath.Join(t.TempDir(), "staging"), func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", res)
	defer k.WSLc.RemoveVolumes(context.Background(), vol)
	var buf bytes.Buffer
	if _, err := k.Volumes.Export(ctx, vol, &buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("moved")) {
		t.Error("volume content missing after the move")
	}
	if _, err := os.Stat(filepath.Join(dest, "wslc", "sessions")); err != nil {
		t.Errorf("new storage not used: %v", err)
	}
}

func tarOf(t *testing.T, name, body string) []byte {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))})
	tw.Write([]byte(body))
	tw.Close()
	return b.Bytes()
}
