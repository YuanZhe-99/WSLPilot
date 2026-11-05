package wpbk

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// loadSnapshots reads every snapshot file, oldest first.
// Returns: the readable snapshots and, when some files are unreadable, an
// error naming them alongside the partial list.
func (r *Repo) loadSnapshots() ([]Snapshot, error) {
	dir := filepath.Join(r.dir, "snapshots")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Snapshot
	var errs []error
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !validID(id) || e.IsDir() {
			continue
		}
		s, err := r.readSnapshot(id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, s)
	}
	slices.SortStableFunc(out, func(a, b Snapshot) int {
		if c := a.Time.Compare(b.Time); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, errors.Join(errs...)
}

// readSnapshot reads and validates snapshots/<id>.json.
func (r *Repo) readSnapshot(id string) (Snapshot, error) {
	data, err := os.ReadFile(filepath.Join(r.dir, "snapshots", id+".json"))
	if err != nil {
		return Snapshot{}, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, fmt.Errorf("wpbk: snapshot %s: %w", id, err)
	}
	if s.Version < 1 || s.Version > FormatVersion {
		return Snapshot{}, fmt.Errorf("%w: snapshot %s is version %d", ErrUnsupportedVersion, id, s.Version)
	}
	if s.ID != id {
		return Snapshot{}, fmt.Errorf("wpbk: snapshot file %s holds id %s", id, s.ID)
	}
	return s, nil
}

// Snapshots lists the repository's snapshots, oldest first.
// Returns: the readable snapshots; a non-nil error alongside a partial list
// names snapshot files that could not be read.
func (r *Repo) Snapshots() ([]Snapshot, error) {
	return r.loadSnapshots()
}

// resolveSnapshot finds a snapshot by full id or unique prefix.
func (r *Repo) resolveSnapshot(idOrPrefix string) (Snapshot, error) {
	if validID(idOrPrefix) {
		s, err := r.readSnapshot(idOrPrefix)
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, fmt.Errorf("%w: %s", ErrSnapshotNotFound, idOrPrefix)
		}
		return s, err
	}
	snaps, _ := r.loadSnapshots()
	var found []Snapshot
	for _, s := range snaps {
		if idOrPrefix != "" && strings.HasPrefix(s.ID, idOrPrefix) {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		return Snapshot{}, fmt.Errorf("%w: %q matches %d snapshots", ErrSnapshotNotFound, idOrPrefix, len(found))
	}
	return found[0], nil
}

// blobReader fetches chunks through an index, keeping packs open.
type blobReader struct {
	r     *Repo
	idx   *memIndex
	files map[string]*os.File
}

// newBlobReader returns a reader over idx.
func (r *Repo) newBlobReader(idx *memIndex) *blobReader {
	return &blobReader{r: r, idx: idx, files: map[string]*os.File{}}
}

// get returns the verified plaintext of chunk id, trying every stored copy.
func (b *blobReader) get(id string) ([]byte, error) {
	locs := b.idx.blobs[id]
	if len(locs) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrChunkMissing, id)
	}
	var first error
	for _, loc := range locs {
		data, err := b.read(loc)
		if err == nil {
			return data, nil
		}
		if first == nil {
			first = err
		}
	}
	return nil, first
}

// read returns the verified plaintext of one stored copy.
func (b *blobReader) read(loc location) ([]byte, error) {
	f, ok := b.files[loc.pack]
	if !ok {
		var err error
		f, err = os.Open(b.r.packPath(loc.pack))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("%w: %s (pack %s is gone)", ErrChunkMissing, loc.ref.ID, loc.pack)
			}
			return nil, err
		}
		if len(b.files) >= 64 {
			b.close()
		}
		b.files[loc.pack] = f
	}
	return b.r.readBlob(f, loc.ref)
}

// close closes every open pack.
func (b *blobReader) close() {
	for k, f := range b.files {
		f.Close()
		delete(b.files, k)
	}
}

// treeReader streams the concatenated tree chunks of a snapshot.
type treeReader struct {
	ctx  context.Context
	br   *blobReader
	ids  []string
	cur  []byte
	next int
}

// Read returns tree bytes, fetching chunks as needed.
func (t *treeReader) Read(p []byte) (int, error) {
	for len(t.cur) == 0 {
		if err := t.ctx.Err(); err != nil {
			return 0, err
		}
		if t.next >= len(t.ids) {
			return 0, io.EOF
		}
		b, err := t.br.get(t.ids[t.next])
		if err != nil {
			return 0, err
		}
		t.cur, t.next = b, t.next+1
	}
	n := copy(p, t.cur)
	t.cur = t.cur[n:]
	return n, nil
}

// walkTree calls fn for every entry of snap in order.
func walkTree(ctx context.Context, br *blobReader, snap Snapshot, fn func(Entry) error) error {
	dec := json.NewDecoder(&treeReader{ctx: ctx, br: br, ids: snap.Tree})
	for {
		var e Entry
		err := dec.Decode(&e)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("wpbk: snapshot %s tree: %w", snap.ID, err)
		}
		if err := fn(e); err != nil {
			return err
		}
	}
}

// Entries calls fn for every entry of a snapshot, in tar order, without
// reading file content. Use it to browse a snapshot.
// Inputs: snapshotID is a full id or a unique prefix.
// Side effects: holds a shared lock unless the repository is read-only.
func (r *Repo) Entries(ctx context.Context, snapshotID string, fn func(Entry) error) error {
	l, err := r.lock(false)
	if err != nil {
		return err
	}
	defer l.release()
	snap, err := r.resolveSnapshot(snapshotID)
	if err != nil {
		return err
	}
	idx, err := r.loadIndex(true)
	if err != nil {
		return err
	}
	br := r.newBlobReader(idx)
	defer br.close()
	return walkTree(ctx, br, snap, fn)
}

// Restore writes a snapshot as a tar stream.
// Inputs: snapshotID is a full id or unique prefix; filter, when not nil,
// selects entries by name; progress may be nil.
// Side effects: holds a shared lock unless the repository is read-only.
// Notes: every header field is reproduced. A hard link whose target was
// filtered out is written as a regular file carrying the target's content,
// so a partial restore never contains a dangling hard link. Every chunk is
// verified against its id before it is written.
func (r *Repo) Restore(ctx context.Context, snapshotID string, w io.Writer, filter func(name string) bool, progress func(Progress)) error {
	l, err := r.lock(false)
	if err != nil {
		return err
	}
	defer l.release()
	snap, err := r.resolveSnapshot(snapshotID)
	if err != nil {
		return err
	}
	idx, err := r.loadIndex(true)
	if err != nil {
		return err
	}
	br := r.newBlobReader(idx)
	defer br.close()

	tw := tar.NewWriter(w)
	var prog Progress
	emitted := map[string]bool{}
	skipped := map[string]Entry{}
	err = walkTree(ctx, br, snap, func(e Entry) error {
		name := string(e.Name)
		if filter != nil && !filter(name) {
			if len(e.Chunks) > 0 || e.Type == string([]byte{tar.TypeReg}) {
				skipped[name] = e
			}
			return nil
		}
		if filter != nil && e.Type == string([]byte{tar.TypeLink}) && !emitted[string(e.Linkname)] {
			if target, ok := skipped[string(e.Linkname)]; ok {
				e.Type = string([]byte{tar.TypeReg})
				e.Linkname = ""
				e.Size = target.Size
				e.Chunks = target.Chunks
			}
		}
		if filter != nil {
			emitted[name] = true
		}
		prog.Current = name
		if err := writeHeader(tw, e); err != nil {
			return fmt.Errorf("wpbk: writing header of %s: %w", name, err)
		}
		for _, id := range e.Chunks {
			b, err := br.get(id)
			if err != nil {
				return fmt.Errorf("wpbk: %s: %w", name, err)
			}
			if _, err := tw.Write(b); err != nil {
				return err
			}
			prog.Bytes += int64(len(b))
			if progress != nil {
				progress(prog)
			}
		}
		prog.Entries++
		if progress != nil {
			progress(prog)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

// headerFromEntry converts an Entry back into a tar header.
func headerFromEntry(e Entry) *tar.Header {
	h := &tar.Header{
		Name:       string(e.Name),
		Mode:       e.Mode,
		Uid:        e.UID,
		Gid:        e.GID,
		Uname:      string(e.Uname),
		Gname:      string(e.Gname),
		ModTime:    e.ModTime,
		AccessTime: e.ATime,
		ChangeTime: e.CTime,
		Linkname:   string(e.Linkname),
		Devmajor:   e.Devmajor,
		Devminor:   e.Devminor,
		Size:       e.Size,
		Format:     tar.Format(e.Format),
	}
	if len(e.Type) > 0 {
		h.Typeflag = e.Type[0]
	}
	if len(e.PAX) > 0 {
		h.PAXRecords = make(map[string]string, len(e.PAX))
		for _, p := range e.PAX {
			h.PAXRecords[string(p.Key)] = string(p.Value)
		}
	}
	if len(e.Chunks) == 0 && e.Size > 0 && h.Typeflag != tar.TypeReg {
		// Header-only entries recorded with a size: write them without data.
		h.Size = 0
	}
	return h
}

// writeHeader writes e's header, preferring the recorded tar format.
// Notes: if the recorded format cannot express the header (or is unknown,
// which would make archive/tar round times), PAX is used.
func writeHeader(tw *tar.Writer, e Entry) error {
	h := headerFromEntry(e)
	if h.Format != tar.FormatUnknown {
		if err := tw.WriteHeader(h); err == nil {
			return nil
		}
	}
	h.Format = tar.FormatPAX
	return tw.WriteHeader(h)
}
