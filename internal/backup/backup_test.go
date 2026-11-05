package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sample(t *testing.T) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	mt := time.Unix(1700000000, 0).UTC()
	add := func(h *tar.Header, body string) {
		h.ModTime = mt
		h.Size = int64(len(body))
		if h.Typeflag != tar.TypeReg {
			h.Size = 0
		}
		tw.WriteHeader(h)
		io.WriteString(tw, body)
	}
	add(&tar.Header{Name: "etc/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	add(&tar.Header{Name: "etc/a.conf", Typeflag: tar.TypeReg, Mode: 0o600, Uid: 1000, Gid: 1000, Uname: "u", Gname: "g"}, "hello")
	add(&tar.Header{Name: "etc/A.conf", Typeflag: tar.TypeReg, Mode: 0o644}, "case twin")
	add(&tar.Header{Name: "etc/what?:*.txt", Typeflag: tar.TypeReg, Mode: 0o644}, "odd name")
	add(&tar.Header{Name: "etc/con", Typeflag: tar.TypeReg, Mode: 0o644}, "reserved")
	add(&tar.Header{Name: "etc/bad\xff", Typeflag: tar.TypeReg, Mode: 0o644}, "bytes")
	add(&tar.Header{Name: "etc/link", Typeflag: tar.TypeSymlink, Linkname: "a.conf"}, "")
	add(&tar.Header{Name: "etc/hard", Typeflag: tar.TypeLink, Linkname: "etc/a.conf"}, "")
	add(&tar.Header{Name: "dev/null", Typeflag: tar.TypeChar, Devmajor: 1, Devminor: 3, Mode: 0o666}, "")
	add(&tar.Header{Name: "fifo", Typeflag: tar.TypeFifo, Mode: 0o600, PAXRecords: map[string]string{"SCHILY.xattr.user.k": "v\x00\x01"}}, "")
	tw.Close()
	return buf.Bytes()
}

type hdr struct {
	name, link   string
	typ          byte
	mode         int64
	uid, gid     int
	major, minor int64
	body, xattr  string
	modTime      int64
}

func headers(t *testing.T, r io.Reader) []hdr {
	var hs []hdr
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return hs
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		hs = append(hs, hdr{h.Name, h.Linkname, h.Typeflag, h.Mode, h.Uid, h.Gid, h.Devmajor, h.Devminor, string(b), h.PAXRecords["SCHILY.xattr.user.k"], h.ModTime.Unix()})
	}
}

func roundTrip(t *testing.T, w Writer) Item {
	in := sample(t)
	m := Meta{Source: Source{Kind: SourceDistro, Name: "Ubuntu"}, Time: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC), Consistent: true, Tool: "test"}
	it, err := w.Write(context.Background(), bytes.NewReader(in), m)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(context.Background(), it); err != nil {
		t.Fatalf("verify: %v", err)
	}
	rc, err := Open(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, want := headers(t, rc), headers(t, bytes.NewReader(in))
	if len(got) != len(want) {
		t.Fatalf("%d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
	return it
}

func TestArchiveRoundTrip(t *testing.T) {
	for _, c := range []Compression{CompressNone, CompressGzip, CompressZstd} {
		t.Run(string(c), func(t *testing.T) {
			dir := t.TempDir()
			it := roundTrip(t, &ArchiveWriter{Dir: dir, Compression: c})
			if filepath.Base(it.Path) != "distro-Ubuntu-20261105-100000"+c.Ext() {
				t.Errorf("name %s", it.Path)
			}
			// Damage is detected.
			b, _ := os.ReadFile(it.Path)
			b[len(b)/2] ^= 0xff
			os.WriteFile(it.Path, b, 0o644)
			if Verify(context.Background(), it) == nil {
				t.Error("damaged archive verified")
			}
		})
	}
}

func TestFolderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	it := roundTrip(t, &FolderWriter{Dir: dir})
	files := filepath.Join(it.Path, "files", "etc")
	for _, n := range []string{"a.conf", "A.conf%~1", "what%3F%3A%2A.txt", "%con", "bad%FF"} {
		if _, err := os.Stat(filepath.Join(files, n)); err != nil {
			t.Errorf("stored file %s: %v", n, err)
		}
	}
	os.WriteFile(filepath.Join(files, "a.conf"), []byte("HELLO"), 0o644)
	if Verify(context.Background(), it) == nil {
		t.Error("changed file verified")
	}
}

func TestRepoAndCatalog(t *testing.T) {
	root := t.TempDir()
	roundTrip(t, &RepoWriter{Dir: filepath.Join(root, "repo")})
	roundTrip(t, &ArchiveWriter{Dir: filepath.Join(root, "archives"), Compression: CompressZstd})
	roundTrip(t, &FolderWriter{Dir: filepath.Join(root, "folders")})
	os.WriteFile(filepath.Join(root, "archives", "outside.tar"), sample(t), 0o644)
	os.WriteFile(filepath.Join(root, "archives", "half.tar.partial"), []byte("x"), 0o644)
	items, errs := Scan(Location{Path: root, ReadOnly: true})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	kinds := map[Kind]int{}
	for _, it := range items {
		kinds[it.Kind]++
	}
	if kinds[KindRepo] != 1 || kinds[KindArchive] != 1 || kinds[KindFolder] != 1 || kinds[KindPlainTar] != 1 || len(items) != 4 {
		t.Fatalf("kinds %v", kinds)
	}
	gs := GroupBySource(items)
	if len(gs) != 2 || len(gs[1].Items) != 3 {
		t.Errorf("groups %+v", gs)
	}
	for _, it := range items {
		if it.Kind == KindPlainTar {
			rc, err := Open(context.Background(), it)
			if err != nil {
				t.Fatal(err)
			}
			if n := len(headers(t, rc)); n != 10 {
				t.Errorf("plain tar entries %d", n)
			}
			rc.Close()
		}
	}
	if err := Delete(Location{Path: root, ReadOnly: true}, items[0]); err == nil {
		t.Error("delete in read-only location")
	}
}

func TestRetention(t *testing.T) {
	var items []Item
	for i := 0; i < 5; i++ {
		items = append(items, Item{Kind: KindArchive, Path: string(rune('a' + i)), Source: Source{Kind: SourceDistro, Name: "x"}, Time: time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.Local)})
	}
	keep, expire := PlanRetention(items, Policy{KeepLast: 2})
	if len(keep) != 2 || len(expire) != 3 || keep[0].Path != "e" && keep[1].Path != "e" {
		t.Errorf("keep %v expire %v", keep, expire)
	}
}
