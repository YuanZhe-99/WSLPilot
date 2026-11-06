package backup_test

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
)

// filesTar is a tar holding the given name → content files.
func filesTar(files ...string) []byte {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, f := range files {
		body := strings.Repeat(f, 50000) // big enough to fill chunks of their own
		tw.WriteHeader(&tar.Header{Name: f, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body)), ModTime: time.Unix(1700000000, 0)})
		tw.Write([]byte(body))
	}
	tw.Close()
	return b.Bytes()
}

func readAll(t *testing.T, it backup.Item) []byte {
	t.Helper()
	rc, err := backup.Open(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Deleting a repository version keeps every other version whole (the data
// it shared stays with them); deleting the last one removes the repository.
func TestDeleteRepositoryVersion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir() + `\repo`
	w := &backup.RepoWriter{Dir: dir}
	loc := backup.Location{Path: dir}
	var items []backup.Item
	var want [][]byte
	for i, files := range [][]string{{"a"}, {"a", "b"}, {"a", "b", "c"}} {
		data := filesTar(files...)
		it, err := w.Write(ctx, bytes.NewReader(data), backup.Meta{Source: backup.Source{Kind: backup.SourceVolume, Name: "v"}, Time: time.Unix(int64(1700000000+i*100), 0), Consistent: true})
		if err != nil {
			t.Fatal(err)
		}
		items, want = append(items, it), append(want, data)
	}
	if err := backup.DeleteVersion(ctx, loc, items[1]); err != nil {
		t.Fatal(err)
	}
	found, _ := backup.Scan(loc)
	if len(found) != 2 {
		t.Fatalf("versions after delete: %d", len(found))
	}
	for _, i := range []int{0, 2} {
		if !bytes.Equal(readAll(t, items[i]), want[i]) {
			t.Errorf("version %d changed", i)
		}
		if err := backup.Verify(ctx, items[i]); err != nil {
			t.Errorf("version %d: %v", i, err)
		}
	}
	if err := backup.DeleteVersion(ctx, loc, items[0]); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readAll(t, items[2]), want[2]) {
		t.Error("the last version changed")
	}
	if err := backup.DeleteVersion(ctx, loc, items[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("repository still there: %v", err)
	}
}

// A read-only location is never changed.
func TestDeleteVersionReadOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir() + `\repo`
	it, err := (&backup.RepoWriter{Dir: dir}).Write(ctx, bytes.NewReader(filesTar("a")), backup.Meta{Source: backup.Source{Kind: backup.SourceVolume, Name: "v"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := backup.DeleteVersion(ctx, backup.Location{Path: dir, ReadOnly: true}, it); err == nil {
		t.Fatal("deleted from a read-only location")
	}
	if found, _ := backup.Scan(backup.Location{Path: dir}); len(found) != 1 {
		t.Fatalf("versions %d", len(found))
	}
}
