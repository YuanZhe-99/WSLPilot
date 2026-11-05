package backup

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"

	"github.com/YuanZhe-99/WSLPilot/internal/volume"
)

// SidecarSuffix is appended to an archive's name for its metadata file.
const SidecarSuffix = ".wpmeta.json"

// Sidecar is the content of "<archive>.wpmeta.json", format version 1.
type Sidecar struct {
	Format      string    `json:"format"` // "wslpilot-archive"
	Version     int       `json:"version"`
	Source      Source    `json:"source"`
	Created     time.Time `json:"created"`
	Consistent  bool      `json:"consistent"`
	Plan        string    `json:"plan,omitempty"`
	Tool        string    `json:"tool"`
	Compression string    `json:"compression"` // none | gzip | zstd
	Size        int64     `json:"size"`        // archive bytes
	SHA256      string    `json:"sha256"`      // of the archive file
	Entries     int64     `json:"entries"`
	ContentSize int64     `json:"contentBytes"`
}

// Compression of an archive.
type Compression string

// Compressions.
const (
	CompressNone Compression = "none"
	CompressGzip Compression = "gzip"
	CompressZstd Compression = "zstd"
)

// Ext is the file extension for the compression.
func (c Compression) Ext() string {
	switch c {
	case CompressGzip:
		return ".tar.gz"
	case CompressZstd:
		return ".tar.zst"
	}
	return ".tar"
}

// ArchiveWriter writes each backup as one archive file plus a sidecar into
// Dir.
type ArchiveWriter struct {
	Dir         string
	Compression Compression
}

// Write stores the tar stream as "<kind>-<name>-<time><ext>".
// Side effects: writes to a ".partial" file, renamed when complete; a
// failure removes it.
func (w *ArchiveWriter) Write(ctx context.Context, r io.Reader, m Meta) (Item, error) {
	if m.Time.IsZero() {
		m.Time = time.Now()
	}
	if err := os.MkdirAll(w.Dir, 0o755); err != nil {
		return Item{}, err
	}
	name := baseName(m) + w.Compression.Ext()
	final := filepath.Join(w.Dir, name)
	tmp := final + ".partial"
	f, err := os.Create(tmp)
	if err != nil {
		return Item{}, err
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	h := sha256.New()
	out := io.MultiWriter(f, h)
	var comp io.WriteCloser
	switch w.Compression {
	case CompressGzip:
		comp = gzip.NewWriter(out)
	case CompressZstd:
		zw, err := zstd.NewWriter(out)
		if err != nil {
			return Item{}, err
		}
		comp = zw
	default:
		comp = nopCloser{out}
	}
	// Count entries as the stream passes, to record and later verify them.
	pr, pw := io.Pipe()
	counted := make(chan tarCount, 1)
	go func() {
		c, _ := countTar(pr)
		io.Copy(io.Discard, pr)
		counted <- c
	}()
	_, err = io.Copy(comp, io.TeeReader(ctxReader{ctx, r}, pw))
	pw.CloseWithError(err)
	cnt := <-counted
	if err == nil {
		err = comp.Close()
	}
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		return Item{}, err
	}
	st, _ := f.Stat()
	if err := f.Close(); err != nil {
		return Item{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return Item{}, err
	}
	ok = true
	sc := Sidecar{Format: "wslpilot-archive", Version: 1, Source: m.Source, Created: m.Time.UTC(), Consistent: m.Consistent,
		Plan: m.Plan, Tool: m.Tool, Compression: string(w.Compression), Size: st.Size(), SHA256: hex.EncodeToString(h.Sum(nil)),
		Entries: cnt.entries, ContentSize: cnt.bytes}
	if sc.Compression == "" {
		sc.Compression = string(CompressNone)
	}
	b, _ := json.MarshalIndent(sc, "", "  ")
	if err := os.WriteFile(final+SidecarSuffix, append(b, '\n'), 0o644); err != nil {
		return Item{}, err
	}
	return archiveItem(final, sc), nil
}

func archiveItem(path string, sc Sidecar) Item {
	return Item{Kind: KindArchive, Path: path, Source: sc.Source, Time: sc.Created, Size: sc.Size,
		Consistent: sc.Consistent, Plan: sc.Plan, Tool: sc.Tool}
}

// readSidecar loads "<path>.wpmeta.json".
func readSidecar(path string) (Sidecar, error) {
	var sc Sidecar
	b, err := os.ReadFile(path + SidecarSuffix)
	if err != nil {
		return sc, err
	}
	if err := json.Unmarshal(b, &sc); err != nil {
		return sc, err
	}
	if sc.Format != "wslpilot-archive" {
		return sc, fmt.Errorf("%s: not a WSLPilot sidecar", path+SidecarSuffix)
	}
	if sc.Version > 1 {
		return sc, fmt.Errorf("%s: sidecar version %d is newer than this WSLPilot", path, sc.Version)
	}
	return sc, nil
}

// openArchive returns the plain tar stream of an archive of any supported
// compression (detected from content, not the name).
func openArchive(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r, err := volume.Decompress(bufio.NewReaderSize(f, 1<<20))
	if err != nil {
		f.Close()
		return nil, err
	}
	return readCloser{r, f}, nil
}

// verifyArchive checks the SHA-256 and the entry count of the sidecar.
func verifyArchive(path string) error {
	sc, err := readSidecar(path)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sc.SHA256 {
		return fmt.Errorf("%s: checksum mismatch (the archive is damaged or was changed)", filepath.Base(path))
	}
	rc, err := openArchive(path)
	if err != nil {
		return err
	}
	defer rc.Close()
	c, err := countTar(rc)
	if err != nil {
		return err
	}
	if c.entries != sc.Entries {
		return fmt.Errorf("%s: %d entries, sidecar says %d", filepath.Base(path), c.entries, sc.Entries)
	}
	return nil
}

type tarCount struct{ entries, bytes int64 }

// countTar reads a whole tar stream.
func countTar(r io.Reader) (tarCount, error) {
	var c tarCount
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return c, nil
		}
		if err != nil {
			return c, err
		}
		c.entries++
		if h.Typeflag == tar.TypeReg {
			c.bytes += h.Size
		}
	}
}

// baseName is "<kind>-<name>-<YYYYMMDD-HHMMSS>", safe for Windows names.
func baseName(m Meta) string {
	kind := strings.TrimPrefix(strings.TrimPrefix(m.Source.Kind, "wsl-"), "wslc-")
	if kind == "" {
		kind = "backup"
	}
	return SafeName(kind+"-"+m.Source.Name) + "-" + m.Time.Format("20060102-150405")
}

// SafeName replaces characters Windows forbids in file names.
func SafeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	return strings.TrimRight(b.String(), ". ")
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

type readCloser struct {
	io.Reader
	c io.Closer
}

func (r readCloser) Close() error {
	if rc, ok := r.Reader.(io.Closer); ok {
		rc.Close()
	}
	return r.c.Close()
}

// ctxReader stops reading when ctx ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func errUnsupported(k Kind) error {
	return errors.New("backup: cannot restore a " + string(k) + " item this way")
}

func itoa(n int) string { return strconv.Itoa(n) }
