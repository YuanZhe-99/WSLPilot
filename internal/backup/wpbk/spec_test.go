package wpbk_test

// This file is an independent wpbk v1 reader written only from
// doc/en-us/backup-format.md. It deliberately uses nothing from package
// wpbk except to create the repository under test; if the document stops
// being sufficient to restore data, this test fails.

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/backup/wpbk"
	"github.com/klauspost/compress/zstd"
	"github.com/zeebo/blake3"
)

// specBlob is a blob reference as specified for trailers and index files.
type specBlob struct {
	ID        string `json:"id"`
	Offset    int64  `json:"offset"`
	Length    int64  `json:"length"`
	RawLength int64  `json:"rawLength"`
}

// specLoc is where the spec reader finds a chunk.
type specLoc struct {
	pack string
	blob specBlob
}

// specBytes decodes a byte string: a JSON string or {"base64": ...}.
func specBytes(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		Base64 string `json:"base64"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatalf("bad byte string %s", raw)
	}
	b, err := base64.StdEncoding.DecodeString(o.Base64)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// specReader restores snapshots following the specification.
type specReader struct {
	t   *testing.T
	dir string
	dec *zstd.Decoder
	m   map[string]specLoc
}

// readTrailers builds the chunk map from pack trailers alone.
func (s *specReader) readTrailers() map[string]specLoc {
	m := map[string]specLoc{}
	files, _ := filepath.Glob(filepath.Join(s.dir, "packs", "*", "*.pack"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			s.t.Fatal(err)
		}
		n := len(data)
		if string(data[:8]) != "WPBKPAK1" || string(data[n-8:]) != "WPBKEND1" {
			s.t.Fatalf("%s: bad magic", f)
		}
		tl := int(binary.LittleEndian.Uint64(data[n-16 : n-8]))
		var tr struct {
			Version int        `json:"version"`
			Blobs   []specBlob `json:"blobs"`
		}
		if err := json.Unmarshal(data[n-16-tl:n-16], &tr); err != nil || tr.Version != 1 {
			s.t.Fatalf("%s: trailer %v", f, err)
		}
		id := strings.TrimSuffix(filepath.Base(f), ".pack")
		sum := blake3.Sum256(data)
		if hex.EncodeToString(sum[:]) != id || filepath.Base(filepath.Dir(f)) != id[:2] {
			s.t.Fatalf("%s: pack id is not the hash of the file", f)
		}
		for _, b := range tr.Blobs {
			m[b.ID] = specLoc{pack: id, blob: b}
		}
	}
	return m
}

// readIndex builds the chunk map from index files.
func (s *specReader) readIndex() map[string]specLoc {
	m := map[string]specLoc{}
	files, _ := filepath.Glob(filepath.Join(s.dir, "index", "*.json"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		sum := blake3.Sum256(data)
		if hex.EncodeToString(sum[:])+".json" != filepath.Base(f) {
			s.t.Fatalf("%s: index name is not its hash", f)
		}
		var idx struct {
			Version int `json:"version"`
			Packs   []struct {
				ID    string     `json:"id"`
				Blobs []specBlob `json:"blobs"`
			} `json:"packs"`
		}
		if err := json.Unmarshal(data, &idx); err != nil || idx.Version != 1 {
			s.t.Fatalf("%s: %v", f, err)
		}
		for _, p := range idx.Packs {
			for _, b := range p.Blobs {
				m[b.ID] = specLoc{pack: p.ID, blob: b}
			}
		}
	}
	return m
}

// chunk returns the verified plaintext of a chunk.
func (s *specReader) chunk(id string) []byte {
	loc, ok := s.m[id]
	if !ok {
		s.t.Fatalf("chunk %s not found", id)
	}
	f, err := os.Open(filepath.Join(s.dir, "packs", loc.pack[:2], loc.pack+".pack"))
	if err != nil {
		s.t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, loc.blob.Length)
	if _, err := f.ReadAt(buf, loc.blob.Offset); err != nil {
		s.t.Fatal(err)
	}
	plain, err := s.dec.DecodeAll(buf, nil)
	if err != nil {
		s.t.Fatal(err)
	}
	sum := blake3.Sum256(plain)
	if hex.EncodeToString(sum[:]) != id || int64(len(plain)) != loc.blob.RawLength {
		s.t.Fatalf("chunk %s fails verification", id)
	}
	return plain
}

// restore writes snapshot id as a tar stream.
func (s *specReader) restore(id string) []byte {
	t := s.t
	data, err := os.ReadFile(filepath.Join(s.dir, "snapshots", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Version int      `json:"version"`
		ID      string   `json:"id"`
		Tree    []string `json:"tree"`
	}
	if err := json.Unmarshal(data, &snap); err != nil || snap.Version != 1 || snap.ID != id {
		t.Fatalf("snapshot: %v", err)
	}
	var list []byte
	for _, c := range snap.Tree {
		list = append(list, s.chunk(c)...)
	}
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	for _, line := range strings.Split(strings.TrimSuffix(string(list), "\n"), "\n") {
		var e struct {
			Name     json.RawMessage `json:"name"`
			Type     string          `json:"type"`
			Mode     int64           `json:"mode"`
			UID      int             `json:"uid"`
			GID      int             `json:"gid"`
			Uname    json.RawMessage `json:"uname"`
			Gname    json.RawMessage `json:"gname"`
			MTime    time.Time       `json:"mtime"`
			ATime    time.Time       `json:"atime"`
			CTime    time.Time       `json:"ctime"`
			Linkname json.RawMessage `json:"linkname"`
			Devmajor int64           `json:"devmajor"`
			Devminor int64           `json:"devminor"`
			Size     int64           `json:"size"`
			Format   int             `json:"format"`
			PAX      []struct {
				Key   json.RawMessage `json:"key"`
				Value json.RawMessage `json:"value"`
			} `json:"pax"`
			Chunks []string `json:"chunks"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("entry %q: %v", line, err)
		}
		h := &tar.Header{
			Typeflag: e.Type[0], Name: specBytes(t, e.Name), Mode: e.Mode, Uid: e.UID, Gid: e.GID,
			Uname: specBytes(t, e.Uname), Gname: specBytes(t, e.Gname), ModTime: e.MTime,
			AccessTime: e.ATime, ChangeTime: e.CTime, Linkname: specBytes(t, e.Linkname),
			Devmajor: e.Devmajor, Devminor: e.Devminor, Size: e.Size, Format: tar.FormatPAX,
		}
		if len(e.Chunks) == 0 && h.Typeflag != tar.TypeReg {
			h.Size = 0
		}
		if e.Format == int(tar.FormatUSTAR) {
			h.Format = tar.FormatUSTAR
		}
		for _, p := range e.PAX {
			if h.PAXRecords == nil {
				h.PAXRecords = map[string]string{}
			}
			h.PAXRecords[specBytes(t, p.Key)] = specBytes(t, p.Value)
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("%q: %v", h.Name, err)
		}
		for _, c := range e.Chunks {
			tw.Write(s.chunk(c))
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// TestSpecReaderRestores restores a snapshot with the independent reader,
// once through the index and once through pack trailers only.
func TestSpecReaderRestores(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	r, err := wpbk.Init(dir, wpbk.InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	src := wpbk.SampleTar(t)
	snap, err := r.Backup(context.Background(), bytes.NewReader(src), wpbk.SnapshotMeta{Source: wpbk.Source{Kind: "wsl-distro", Name: "spec"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()

	cfgData, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Format      string          `json:"format"`
		Version     int             `json:"version"`
		Compression string          `json:"compression"`
		Hash        string          `json:"hash"`
		Encryption  json.RawMessage `json:"encryption"`
	}
	json.Unmarshal(cfgData, &cfg)
	if cfg.Format != "wpbk" || cfg.Version != 1 || cfg.Compression != "zstd" || cfg.Hash != "blake3-256" || string(cfg.Encryption) != "null" {
		t.Fatalf("config %+v", cfg)
	}
	for _, d := range []string{"packs", "index", "snapshots", "locks"} {
		if info, err := os.Stat(filepath.Join(dir, d)); err != nil || !info.IsDir() {
			t.Fatalf("missing %s/", d)
		}
	}

	dec, _ := zstd.NewReader(nil)
	defer dec.Close()
	s := &specReader{t: t, dir: dir, dec: dec}
	fromIndex, fromTrailers := s.readIndex(), s.readTrailers()
	if len(fromIndex) == 0 || len(fromIndex) != len(fromTrailers) {
		t.Fatalf("index has %d chunks, trailers %d", len(fromIndex), len(fromTrailers))
	}
	for _, m := range []map[string]specLoc{fromIndex, fromTrailers} {
		s.m = m
		wpbk.CompareTarBytes(t, src, s.restore(snap.ID))
	}
}
