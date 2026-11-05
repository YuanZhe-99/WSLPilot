package backup_test

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
)

// specBytes decodes a byte string as doc/en-us/backups.md defines it.
func specBytes(t *testing.T, raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct{ Base64 string }
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	b, _ := base64.StdEncoding.DecodeString(o.Base64)
	return string(b)
}

func specTar() []byte {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	mt := time.Unix(1700000000, 0)
	tw.WriteHeader(&tar.Header{Name: "home/", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: mt})
	tw.WriteHeader(&tar.Header{Name: "home/a\xfe", Typeflag: tar.TypeReg, Mode: 0o600, Uid: 7, Gid: 8, Size: 3, ModTime: mt})
	tw.Write([]byte("abc"))
	tw.WriteHeader(&tar.Header{Name: "home/l", Typeflag: tar.TypeSymlink, Linkname: "a\xfe", ModTime: mt})
	tw.Close()
	return b.Bytes()
}

// TestFolderSpecReader restores a folder backup using only the documented
// layout: folder.json, manifest.jsonl and files/ with SHA-256 checks.
func TestFolderSpecReader(t *testing.T) {
	dir := t.TempDir()
	it, err := (&backup.FolderWriter{Dir: dir}).Write(context.Background(), bytes.NewReader(specTar()),
		backup.Meta{Source: backup.Source{Kind: "wsl-distro", Name: "x"}, Consistent: true})
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Format   string
		Version  int
		Complete bool
	}
	b, _ := os.ReadFile(filepath.Join(it.Path, ".wslpilot", "folder.json"))
	json.Unmarshal(b, &info)
	if info.Format != "wslpilot-folder" || info.Version != 1 || !info.Complete {
		t.Fatalf("folder.json %+v", info)
	}
	f, _ := os.Open(filepath.Join(it.Path, ".wslpilot", "manifest.jsonl"))
	defer f.Close()
	sc := bufio.NewScanner(f)
	var names []string
	for sc.Scan() {
		var e struct {
			Name, Linkname    json.RawMessage
			Type, Stored, SHA string
			SHA256            string
			UID, GID          int
			Mode              int64
		}
		json.Unmarshal(sc.Bytes(), &e)
		name := specBytes(t, e.Name)
		names = append(names, name)
		switch e.Type {
		case "file":
			content, err := os.ReadFile(filepath.Join(it.Path, "files", filepath.FromSlash(e.Stored)))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(content)
			if hex.EncodeToString(sum[:]) != e.SHA256 || string(content) != "abc" || e.UID != 7 || e.Mode != 0o600 {
				t.Errorf("file %q: %+v", name, e)
			}
		case "symlink":
			if specBytes(t, e.Linkname) != "a\xfe" {
				t.Errorf("link target")
			}
		}
	}
	if len(names) != 3 || names[1] != "home/a\xfe" {
		t.Errorf("names %q", names)
	}
}

// TestArchiveSpecReader checks an archive with only the sidecar rules.
func TestArchiveSpecReader(t *testing.T) {
	dir := t.TempDir()
	it, err := (&backup.ArchiveWriter{Dir: dir, Compression: backup.CompressZstd}).Write(context.Background(), bytes.NewReader(specTar()),
		backup.Meta{Source: backup.Source{Kind: "wslc-volume", Name: "v"}, Consistent: true})
	if err != nil {
		t.Fatal(err)
	}
	var sc struct {
		Format, Compression, SHA256 string
		Version                     int
		Entries                     int64
	}
	b, _ := os.ReadFile(it.Path + ".wpmeta.json")
	json.Unmarshal(b, &sc)
	raw, _ := os.ReadFile(it.Path)
	sum := sha256.Sum256(raw)
	if sc.Format != "wslpilot-archive" || sc.Version != 1 || sc.Compression != "zstd" || hex.EncodeToString(sum[:]) != sc.SHA256 {
		t.Fatalf("sidecar %+v", sc)
	}
	d, _ := zstd.NewReader(bytes.NewReader(raw))
	tr := tar.NewReader(d)
	n := int64(0)
	for {
		if _, err := tr.Next(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != sc.Entries {
		t.Errorf("entries %d, sidecar %d", n, sc.Entries)
	}
}
