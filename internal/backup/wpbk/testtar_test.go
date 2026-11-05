package wpbk

import (
	"archive/tar"
	"bytes"
	"io"
	"maps"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

// tarEntry is one entry of a test tar stream.
type tarEntry struct {
	hdr  tar.Header
	data []byte
}

// randomBytes returns n deterministic pseudo-random bytes.
func randomBytes(seed uint64, n int) []byte {
	var s [32]byte
	s[0], s[1] = byte(seed), byte(seed>>8)
	b := make([]byte, n)
	rand.NewChaCha8(s).Read(b)
	return b
}

// sampleEntries returns a tar covering every metadata case the repository
// must preserve. bigSeed selects the 10 MiB file's content.
func sampleEntries(bigSeed uint64) []tarEntry {
	mt := time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.UTC)
	base := func(name string, typ byte, mode int64) tar.Header {
		return tar.Header{Name: name, Typeflag: typ, Mode: mode, Uid: 1234, Gid: 4321, Uname: "alice", Gname: "staff", ModTime: mt, Format: tar.FormatPAX}
	}
	dir := base("root/", tar.TypeDir, 0o2775)
	big := base("root/big.bin", tar.TypeReg, 0o640)
	small := base("root/small.txt", tar.TypeReg, 0o4755)
	small.PAXRecords = map[string]string{
		"SCHILY.xattr.user.test":       "hello xattr",
		"SCHILY.xattr.security.binary": "\x01\x00\xff\xfe",
	}
	small.AccessTime = mt.Add(time.Hour)
	small.ChangeTime = mt.Add(2 * time.Hour)
	empty := base("root/empty", tar.TypeReg, 0o600)
	odd := base("root/caf\xe9 \"q\" \\x", tar.TypeReg, 0o1777)
	link := base("root/link", tar.TypeSymlink, 0o777)
	link.Linkname = "small.txt"
	hard := base("root/hard", tar.TypeLink, 0o4755)
	hard.Linkname = "root/small.txt"
	dev := base("dev/null", tar.TypeChar, 0o666)
	dev.Devmajor, dev.Devminor = 1, 3
	blk := base("dev/sda", tar.TypeBlock, 0o660)
	blk.Devmajor, blk.Devminor = 8, 0
	fifo := base("root/fifo", tar.TypeFifo, 0o644)
	ustar := tar.Header{Name: "plain.txt", Typeflag: tar.TypeReg, Mode: 0o644, ModTime: mt.Truncate(time.Second), Format: tar.FormatUSTAR}
	return []tarEntry{
		{hdr: dir},
		{hdr: big, data: randomBytes(bigSeed, 10<<20)},
		{hdr: small, data: []byte("hello, world\n")},
		{hdr: empty},
		{hdr: odd, data: []byte("non-UTF-8 name")},
		{hdr: link},
		{hdr: hard},
		{hdr: dev},
		{hdr: blk},
		{hdr: fifo},
		{hdr: ustar, data: bytes.Repeat([]byte("ustar "), 1000)},
	}
}

// buildTar serializes entries.
func buildTar(t testing.TB, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		h := e.hdr
		h.Size = int64(len(e.data))
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatalf("header %q: %v", h.Name, err)
		}
		if _, err := tw.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// parseTar reads a tar stream back into entries.
func parseTar(t testing.TB, data []byte) []tarEntry {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(data))
	var out []tarEntry
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, tarEntry{hdr: *h, data: b})
	}
}

// compareTars fails unless got reproduces every header field and byte of want.
func compareTars(t testing.TB, want, got []tarEntry) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		w, g := want[i].hdr, got[i].hdr
		type field struct {
			name      string
			want, got any
		}
		fields := []field{
			{"Name", w.Name, g.Name}, {"Typeflag", w.Typeflag, g.Typeflag}, {"Mode", w.Mode, g.Mode},
			{"Uid", w.Uid, g.Uid}, {"Gid", w.Gid, g.Gid}, {"Uname", w.Uname, g.Uname}, {"Gname", w.Gname, g.Gname},
			{"Linkname", w.Linkname, g.Linkname}, {"Devmajor", w.Devmajor, g.Devmajor}, {"Devminor", w.Devminor, g.Devminor},
			{"Size", w.Size, g.Size}, {"Format", w.Format, g.Format},
		}
		for _, f := range fields {
			if f.want != f.got {
				t.Errorf("entry %d (%q) %s: got %v, want %v", i, w.Name, f.name, f.got, f.want)
			}
		}
		if !w.ModTime.Equal(g.ModTime) || !w.AccessTime.Equal(g.AccessTime) || !w.ChangeTime.Equal(g.ChangeTime) {
			t.Errorf("entry %q times: got %v/%v/%v, want %v/%v/%v", w.Name, g.ModTime, g.AccessTime, g.ChangeTime, w.ModTime, w.AccessTime, w.ChangeTime)
		}
		if !maps.Equal(w.PAXRecords, g.PAXRecords) {
			t.Errorf("entry %q PAX: got %q, want %q", w.Name, g.PAXRecords, w.PAXRecords)
		}
		if !bytes.Equal(want[i].data, got[i].data) {
			t.Errorf("entry %q content differs (%d vs %d bytes)", w.Name, len(got[i].data), len(want[i].data))
		}
	}
}

// distinct appends tag to every non-empty content so that the result shares
// no data chunk with the original.
func distinct(entries []tarEntry, tag string) []tarEntry {
	out := slices.Clone(entries)
	for i := range out {
		if len(out[i].data) > 0 {
			out[i].data = append(slices.Clone(out[i].data), tag...)
		}
	}
	return out
}
