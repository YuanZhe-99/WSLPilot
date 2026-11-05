package backup

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Folder backup layout (format "wslpilot-folder", version 1):
//
//	<dir>/.wslpilot/folder.json     Meta of the backup
//	<dir>/.wslpilot/manifest.jsonl  one ManifestEntry per tar entry, in order
//	<dir>/files/…                   regular files and directories, browsable
//
// Linux metadata (owner, mode, times, links, devices, xattrs) lives only in
// the manifest; the files tree is for browsing and holds content.

// FolderInfo is .wslpilot/folder.json.
type FolderInfo struct {
	Format      string    `json:"format"` // "wslpilot-folder"
	Version     int       `json:"version"`
	Source      Source    `json:"source"`
	Created     time.Time `json:"created"`
	Consistent  bool      `json:"consistent"`
	Plan        string    `json:"plan,omitempty"`
	Tool        string    `json:"tool"`
	Entries     int64     `json:"entries"`
	ContentSize int64     `json:"contentBytes"`
	Complete    bool      `json:"complete"`
}

// Bytes is a byte string stored as JSON text when it is valid UTF-8 and as
// {"base64": "..."} otherwise (Linux names are bytes, not text).
type Bytes string

// MarshalJSON implements json.Marshaler.
func (b Bytes) MarshalJSON() ([]byte, error) {
	if utf8.ValidString(string(b)) {
		return json.Marshal(string(b))
	}
	return json.Marshal(map[string]string{"base64": base64.StdEncoding.EncodeToString([]byte(b))})
}

// UnmarshalJSON implements json.Unmarshaler.
func (b *Bytes) UnmarshalJSON(data []byte) error {
	var s string
	if json.Unmarshal(data, &s) == nil {
		*b = Bytes(s)
		return nil
	}
	var o struct{ Base64 string }
	if err := json.Unmarshal(data, &o); err != nil {
		return err
	}
	d, err := base64.StdEncoding.DecodeString(o.Base64)
	*b = Bytes(d)
	return err
}

// ManifestEntry is one line of manifest.jsonl.
type ManifestEntry struct {
	Name     Bytes            `json:"name"`
	Type     string           `json:"type"` // file, dir, symlink, hardlink, char, block, fifo
	Mode     int64            `json:"mode"`
	UID      int              `json:"uid"`
	GID      int              `json:"gid"`
	Uname    Bytes            `json:"uname,omitempty"`
	Gname    Bytes            `json:"gname,omitempty"`
	ModTime  time.Time        `json:"mtime"`
	Linkname Bytes            `json:"linkname,omitempty"`
	Devmajor int64            `json:"devmajor,omitempty"`
	Devminor int64            `json:"devminor,omitempty"`
	Size     int64            `json:"size,omitempty"`
	SHA256   string           `json:"sha256,omitempty"`
	Stored   string           `json:"stored,omitempty"` // path under files/, '/'-separated
	PAX      map[string]Bytes `json:"pax,omitempty"`
}

var typeNames = map[byte]string{
	tar.TypeReg: "file", tar.TypeDir: "dir", tar.TypeSymlink: "symlink", tar.TypeLink: "hardlink",
	tar.TypeChar: "char", tar.TypeBlock: "block", tar.TypeFifo: "fifo",
}

func typeFlag(name string) (byte, bool) {
	for k, v := range typeNames {
		if v == name {
			return k, true
		}
	}
	return 0, false
}

// FolderWriter writes each backup into a new folder below Dir.
type FolderWriter struct{ Dir string }

// Write unpacks the tar stream into "<Dir>/<kind>-<name>-<time>/".
// Notes: folder.json is written with Complete=false first and rewritten
// with Complete=true at the end, so an interrupted backup is recognized.
func (w *FolderWriter) Write(ctx context.Context, r io.Reader, m Meta) (Item, error) {
	if m.Time.IsZero() {
		m.Time = time.Now()
	}
	dir := filepath.Join(w.Dir, baseName(m))
	meta := filepath.Join(dir, ".wslpilot")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		return Item{}, err
	}
	info := FolderInfo{Format: "wslpilot-folder", Version: 1, Source: m.Source, Created: m.Time.UTC(),
		Consistent: m.Consistent, Plan: m.Plan, Tool: m.Tool}
	if err := writeJSON(filepath.Join(meta, "folder.json"), info); err != nil {
		return Item{}, err
	}
	mf, err := os.Create(filepath.Join(meta, "manifest.jsonl"))
	if err != nil {
		return Item{}, err
	}
	defer mf.Close()
	bw := bufio.NewWriter(mf)
	enc := json.NewEncoder(bw)
	names := newNamer()
	tr := tar.NewReader(ctxReader{ctx, r})
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Item{}, err
		}
		e := ManifestEntry{Name: Bytes(h.Name), Type: typeNames[h.Typeflag], Mode: h.Mode, UID: h.Uid, GID: h.Gid,
			Uname: Bytes(h.Uname), Gname: Bytes(h.Gname), ModTime: h.ModTime.UTC(), Linkname: Bytes(h.Linkname),
			Devmajor: h.Devmajor, Devminor: h.Devminor}
		if e.Type == "" {
			return Item{}, fmt.Errorf("backup: unsupported tar entry type %q for %s", h.Typeflag, h.Name)
		}
		if len(h.PAXRecords) > 0 {
			e.PAX = map[string]Bytes{}
			for k, v := range h.PAXRecords {
				e.PAX[k] = Bytes(v)
			}
		}
		switch h.Typeflag {
		case tar.TypeReg:
			e.Stored = names.stored(h.Name)
			e.Size = h.Size
			p := filepath.Join(dir, "files", filepath.FromSlash(e.Stored))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return Item{}, err
			}
			f, err := os.Create(p)
			if err != nil {
				return Item{}, err
			}
			hs := sha256.New()
			_, err = io.Copy(io.MultiWriter(f, hs), tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return Item{}, err
			}
			os.Chtimes(p, h.ModTime, h.ModTime)
			e.SHA256 = hex.EncodeToString(hs.Sum(nil))
			info.ContentSize += h.Size
		case tar.TypeDir:
			e.Stored = names.stored(h.Name)
			os.MkdirAll(filepath.Join(dir, "files", filepath.FromSlash(e.Stored)), 0o755)
		}
		info.Entries++
		if err := enc.Encode(e); err != nil {
			return Item{}, err
		}
	}
	if err := bw.Flush(); err != nil {
		return Item{}, err
	}
	if err := mf.Sync(); err != nil {
		return Item{}, err
	}
	info.Complete = true
	if err := writeJSON(filepath.Join(meta, "folder.json"), info); err != nil {
		return Item{}, err
	}
	return folderItem(dir, info), nil
}

func folderItem(dir string, info FolderInfo) Item {
	it := Item{Kind: KindFolder, Path: dir, Source: info.Source, Time: info.Created, Size: info.ContentSize,
		Consistent: info.Consistent, Plan: info.Plan, Tool: info.Tool}
	if !info.Complete {
		it.Note = "incomplete"
	}
	return it
}

// readFolderInfo loads .wslpilot/folder.json of dir.
func readFolderInfo(dir string) (FolderInfo, error) {
	var info FolderInfo
	b, err := os.ReadFile(filepath.Join(dir, ".wslpilot", "folder.json"))
	if err != nil {
		return info, err
	}
	if err := json.Unmarshal(b, &info); err != nil {
		return info, err
	}
	if info.Format != "wslpilot-folder" {
		return info, fmt.Errorf("%s: not a WSLPilot folder backup", dir)
	}
	if info.Version > 1 {
		return info, fmt.Errorf("%s: folder format %d is newer than this WSLPilot", dir, info.Version)
	}
	return info, nil
}

// openFolder streams a folder backup as tar, rebuilt from the manifest.
func openFolder(ctx context.Context, dir string) (io.ReadCloser, error) {
	info, err := readFolderInfo(dir)
	if err != nil {
		return nil, err
	}
	if !info.Complete {
		return nil, fmt.Errorf("%s: the backup did not finish", dir)
	}
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(writeFolderTar(ctx, dir, pw, true)) }()
	return pr, nil
}

// verifyFolder rebuilds the tar stream, checking every file's hash.
func verifyFolder(ctx context.Context, dir string) error {
	return writeFolderTar(ctx, dir, io.Discard, true)
}

// writeFolderTar writes the tar stream of a folder backup to w.
func writeFolderTar(ctx context.Context, dir string, w io.Writer, check bool) error {
	mf, err := os.Open(filepath.Join(dir, ".wslpilot", "manifest.jsonl"))
	if err != nil {
		return err
	}
	defer mf.Close()
	tw := tar.NewWriter(w)
	sc := bufio.NewScanner(mf)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	line := 0
	for sc.Scan() {
		line++
		if err := ctx.Err(); err != nil {
			return err
		}
		var e ManifestEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return fmt.Errorf("manifest line %d: %w", line, err)
		}
		tf, ok := typeFlag(e.Type)
		if !ok {
			return fmt.Errorf("manifest line %d: unknown type %q", line, e.Type)
		}
		h := &tar.Header{Typeflag: tf, Name: string(e.Name), Mode: e.Mode, Uid: e.UID, Gid: e.GID, Uname: string(e.Uname),
			Gname: string(e.Gname), ModTime: e.ModTime, Linkname: string(e.Linkname), Devmajor: e.Devmajor, Devminor: e.Devminor,
			Format: tar.FormatPAX}
		if len(e.PAX) > 0 {
			h.PAXRecords = map[string]string{}
			for k, v := range e.PAX {
				h.PAXRecords[k] = string(v)
			}
		}
		if tf != tar.TypeReg {
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
			continue
		}
		h.Size = e.Size
		f, err := os.Open(filepath.Join(dir, "files", filepath.FromSlash(e.Stored)))
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		if err := tw.WriteHeader(h); err != nil {
			f.Close()
			return err
		}
		hs := sha256.New()
		n, err := io.Copy(io.MultiWriter(tw, hs), io.LimitReader(f, e.Size))
		f.Close()
		if err != nil {
			return err
		}
		if n != e.Size {
			return fmt.Errorf("%s: file is shorter than recorded", e.Name)
		}
		if check && hex.EncodeToString(hs.Sum(nil)) != e.SHA256 {
			return fmt.Errorf("%s: content changed since the backup", e.Name)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return tw.Close()
}

// namer maps Linux paths to Windows-safe, collision-free stored paths.
type namer struct{ used map[string]bool }

func newNamer() *namer { return &namer{used: map[string]bool{}} }

// stored returns the path under files/ for a tar name.
// Notes: each component is encoded (EncodeComponent); NTFS is case-
// insensitive, so a name that differs from an earlier one only in case
// gets a "%~N" suffix.
func (n *namer) stored(name string) string {
	clean := strings.Trim(path.Clean("/"+name), "/")
	if clean == "" || clean == "." {
		return "."
	}
	parts := strings.Split(clean, "/")
	for i, p := range parts {
		parts[i] = EncodeComponent(p)
	}
	s := strings.Join(parts, "/")
	key := strings.ToLower(s)
	if n.used[key] {
		for i := 1; ; i++ {
			alt := fmt.Sprintf("%s%%~%d", s, i)
			if !n.used[strings.ToLower(alt)] {
				s, key = alt, strings.ToLower(alt)
				break
			}
		}
	}
	n.used[key] = true
	return s
}

// reserved are names Windows refuses for files in any folder.
var reserved = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}

func init() {
	for i := 1; i <= 9; i++ {
		reserved[fmt.Sprintf("com%d", i)] = true
		reserved[fmt.Sprintf("lpt%d", i)] = true
	}
}

// EncodeComponent percent-encodes the bytes of one Linux path component
// that Windows cannot store: control characters, <>:"\|?*, %, invalid
// UTF-8, a trailing dot or space; and prefixes reserved device names.
func EncodeComponent(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); {
		r, size := utf8.DecodeRuneInString(p[i:])
		c := p[i]
		bad := r == utf8.RuneError && size == 1 || c < 32 || strings.ContainsRune(`<>:"\|?*%`, rune(c)) && size == 1
		last := i+size == len(p)
		if last && (c == '.' || c == ' ') {
			bad = true
		}
		if bad {
			for j := 0; j < size; j++ {
				fmt.Fprintf(&b, "%%%02X", p[i+j])
			}
		} else {
			b.WriteString(p[i : i+size])
		}
		i += size
	}
	s := b.String()
	base := strings.ToLower(strings.SplitN(s, ".", 2)[0])
	if reserved[base] {
		s = "%" + s
	}
	return s
}

func writeJSON(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
