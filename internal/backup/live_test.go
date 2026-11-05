package backup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YuanZhe-99/WSLPilot/internal/volume"
	"github.com/YuanZhe-99/WSLPilot/internal/wsl"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// TestLiveDistroBackupRestore backs up wslpilot-test-d1 into each kind,
// restores the repository snapshot as wslpilot-test-restored and compares
// a file. Needs WSLPILOT_LIVE=1 and an existing wslpilot-test-d1.
func TestLiveDistroBackupRestore(t *testing.T) {
	if os.Getenv("WSLPILOT_LIVE") != "1" {
		t.Skip("set WSLPILOT_LIVE=1")
	}
	ctx := context.Background()
	w := wsl.New()
	if _, err := w.Get(ctx, "wslpilot-test-d1"); err != nil {
		t.Skip("wslpilot-test-d1 is not registered")
	}
	c := wslc.New()
	r := &Runner{WSL: w, WSLc: c, Vol: &volume.Pipeline{E: c}, Host: "test"}
	w.Exec(ctx, "wslpilot-test-d1", "root", "sh", "-c", "echo marker-$$ > /root/marker && chown 1234:4321 /root/marker")
	want, _ := w.Exec(ctx, "wslpilot-test-d1", "root", "cat", "/root/marker")
	root := t.TempDir()
	var repoItem Item
	for _, wr := range []Writer{&ArchiveWriter{Dir: filepath.Join(root, "a"), Compression: CompressZstd}, &FolderWriter{Dir: filepath.Join(root, "f")}, &RepoWriter{Dir: filepath.Join(root, "r")}} {
		it, err := r.BackupDistro(ctx, "wslpilot-test-d1", wr, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := Verify(ctx, it); err != nil {
			t.Fatalf("%s: %v", it.Kind, err)
		}
		if it.Kind == KindRepo {
			repoItem = it
		}
	}
	const restored = "wslpilot-test-restored"
	defer w.Unregister(context.Background(), restored)
	if err := r.RestoreAsDistro(ctx, repoItem, restored, filepath.Join(root, "restored")); err != nil {
		t.Fatal(err)
	}
	got, err := w.Exec(ctx, restored, "root", "stat", "-c", "%u:%g", "/root/marker")
	if err != nil || strings.TrimSpace(got) != "1234:4321" {
		t.Errorf("owner %q %v", got, err)
	}
	content, _ := w.Exec(ctx, restored, "root", "cat", "/root/marker")
	if content != want {
		t.Errorf("content %q, want %q", content, want)
	}
	// Partial restore into the running copy.
	w.Exec(ctx, restored, "root", "rm", "/root/marker")
	if err := r.RestoreIntoDistro(ctx, repoItem, restored, "/", func(n string) bool { return strings.TrimPrefix(n, "./") == "root/marker" }); err != nil {
		t.Fatal(err)
	}
	if content, _ := w.Exec(ctx, restored, "root", "cat", "/root/marker"); !bytes.Equal([]byte(content), []byte(want)) {
		t.Errorf("partial restore %q", content)
	}
}
