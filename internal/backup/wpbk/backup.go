package wpbk

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// SnapshotMeta describes a backup being taken.
type SnapshotMeta struct {
	Source Source
	Tags   []string
	// Parent names the previous snapshot of the same source; empty selects
	// the newest snapshot with an equal Source. It is informational:
	// deduplication always uses the whole repository.
	Parent string
	// Time overrides the snapshot time (zero: now).
	Time time.Time
}

// Progress reports a running backup or restore.
type Progress struct {
	// Entries is the number of tar entries processed.
	Entries int64
	// Bytes is the entry content processed.
	Bytes int64
	// NewChunks and NewBytes count chunks stored by this backup and their
	// compressed size (zero during restore).
	NewChunks int64
	NewBytes  int64
	// Current is the entry being processed.
	Current string
}

// paxBasicKeys are PAX keys that Entry already holds as fields; they are not
// stored again in Entry.PAX.
var paxBasicKeys = map[string]bool{
	"path": true, "linkpath": true, "size": true, "uid": true, "gid": true,
	"uname": true, "gname": true, "mtime": true, "atime": true, "ctime": true,
}

// Backup reads a tar stream and stores it as a new snapshot.
// Inputs: src is the tar stream; meta describes the source; progress, when
// not nil, is called after every chunk and entry.
// Returns: the snapshot written.
// Side effects: holds an exclusive lock; writes packs, one index file and
// one snapshot file, in that order, each atomically.
// Notes: on error or cancellation no snapshot becomes visible; packs that
// were already completed stay as unindexed packs, which later backups reuse
// and Prune removes when unused.
func (r *Repo) Backup(ctx context.Context, src io.Reader, meta SnapshotMeta, progress func(Progress)) (Snapshot, error) {
	l, err := r.lock(true)
	if err != nil {
		return Snapshot{}, err
	}
	defer l.release()

	snaps, _ := r.loadSnapshots()
	if meta.Parent == "" {
		for _, s := range snaps {
			if s.Source == meta.Source {
				meta.Parent = s.ID
			}
		}
	}
	idx, err := r.loadIndex(true)
	if err != nil {
		return Snapshot{}, err
	}
	s := &saver{r: r, idx: idx, pending: map[string]bool{}}
	defer s.abort()

	var prog Progress
	report := func() {
		if progress != nil {
			prog.NewChunks, prog.NewBytes = s.newChunks, s.newBytes
			progress(prog)
		}
	}
	snap := Snapshot{
		Version: FormatVersion,
		ID:      randomID(),
		Time:    meta.Time,
		Source:  meta.Source,
		Tags:    meta.Tags,
		Tool:    toolString(),
		Parent:  meta.Parent,
	}
	if snap.Time.IsZero() {
		snap.Time = r.now()
	}
	snap.Time = snap.Time.UTC()

	treeChunker := newChunker(r.cfg.Chunker, func(b []byte) error {
		id, err := s.store(b)
		snap.Tree = append(snap.Tree, id)
		return err
	})
	enc := json.NewEncoder(treeChunker)
	enc.SetEscapeHTML(false)

	tr := tar.NewReader(src)
	in := &ctxReader{ctx: ctx, r: tr}
	for {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Snapshot{}, fmt.Errorf("wpbk: reading tar stream: %w", err)
		}
		e := entryFromHeader(hdr)
		prog.Current = hdr.Name
		cc := newChunker(r.cfg.Chunker, func(b []byte) error {
			id, err := s.store(b)
			e.Chunks = append(e.Chunks, id)
			prog.Bytes += int64(len(b))
			report()
			return err
		})
		n, err := io.Copy(cc, in)
		if err != nil {
			return Snapshot{}, fmt.Errorf("wpbk: reading %s: %w", hdr.Name, err)
		}
		if err := cc.Close(); err != nil {
			return Snapshot{}, err
		}
		if n != e.Size {
			if n != 0 {
				return Snapshot{}, fmt.Errorf("wpbk: %s: read %d bytes, header says %d", hdr.Name, n, e.Size)
			}
			// Header-only types (links, devices, directories) carry no data
			// whatever their size field says; keep the field as recorded.
		}
		if err := enc.Encode(&e); err != nil {
			return Snapshot{}, err
		}
		snap.Totals.Entries++
		snap.Totals.Bytes += n
		prog.Entries++
		report()
	}
	if err := treeChunker.Close(); err != nil {
		return Snapshot{}, err
	}
	if err := s.flush(); err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if _, err := r.writeIndex(s.written); err != nil {
		return Snapshot{}, err
	}
	snap.Totals.NewChunks, snap.Totals.NewBytes = s.newChunks, s.newBytes
	if snap.Tree == nil {
		snap.Tree = []string{}
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return Snapshot{}, err
	}
	if err := writeFileAtomic(filepath.Join(r.dir, "snapshots", snap.ID+".json"), r.seal(aadSnapshot, data)); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// entryFromHeader converts a tar header into an Entry without chunks.
// Notes: GNU sparse files are read expanded by archive/tar, so they are
// stored as regular files; their holes become zero bytes.
func entryFromHeader(h *tar.Header) Entry {
	e := Entry{
		Name:     BString(h.Name),
		Type:     string([]byte{h.Typeflag}),
		Mode:     h.Mode,
		UID:      h.Uid,
		GID:      h.Gid,
		Uname:    BString(h.Uname),
		Gname:    BString(h.Gname),
		ModTime:  h.ModTime.UTC(),
		Linkname: BString(h.Linkname),
		Devmajor: h.Devmajor,
		Devminor: h.Devminor,
		Size:     h.Size,
		Format:   int(h.Format),
	}
	if !h.AccessTime.IsZero() {
		e.ATime = h.AccessTime.UTC()
	}
	if !h.ChangeTime.IsZero() {
		e.CTime = h.ChangeTime.UTC()
	}
	if h.Typeflag == tar.TypeGNUSparse {
		e.Type = string([]byte{tar.TypeReg})
	}
	keys := make([]string, 0, len(h.PAXRecords))
	for k := range h.PAXRecords {
		if paxBasicKeys[k] || strings.HasPrefix(k, "GNU.sparse.") {
			continue
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		e.PAX = append(e.PAX, PAXRecord{Key: BString(k), Value: BString(h.PAXRecords[k])})
	}
	return e
}

// saver deduplicates chunks and packs the new ones.
type saver struct {
	r         *Repo
	idx       *memIndex
	pending   map[string]bool
	pw        *packWriter
	written   []indexPack
	newChunks int64
	newBytes  int64
}

// store makes sure chunk b is in the repository.
// Returns: the chunk id.
func (s *saver) store(b []byte) (string, error) {
	id := s.r.chunkID(b)
	if s.pending[id] || s.idx.has(id) {
		return id, nil
	}
	data := s.r.seal(blobAAD(id), s.r.enc.EncodeAll(b, make([]byte, 0, len(b)/2+64)))
	if s.pw == nil {
		pw, err := s.r.newPackWriter()
		if err != nil {
			return id, err
		}
		s.pw = pw
	}
	if err := s.pw.add(id, data, int64(len(b))); err != nil {
		return id, err
	}
	s.pending[id] = true
	s.newChunks++
	s.newBytes += int64(len(data))
	if s.pw.size() >= s.r.cfg.PackSize {
		return id, s.flush()
	}
	return id, nil
}

// flush completes the open pack, if any.
func (s *saver) flush() error {
	if s.pw == nil {
		return nil
	}
	p, err := s.pw.finish()
	s.pw = nil
	if err != nil {
		return err
	}
	s.written = append(s.written, p)
	return nil
}

// abort discards an open pack after a failure.
func (s *saver) abort() {
	if s.pw != nil {
		s.pw.abort()
		s.pw = nil
	}
}

// ctxReader fails reads once ctx is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

// Read reads from the wrapped reader unless the context is done.
func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
