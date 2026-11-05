package wpbk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Forget removes snapshots. Their data stays until Prune.
// Inputs: full snapshot ids or unique prefixes.
// Returns: the full ids removed.
// Side effects: holds an exclusive lock; deletes snapshots/<id>.json files.
func (r *Repo) Forget(ids ...string) ([]string, error) {
	l, err := r.lock(true)
	if err != nil {
		return nil, err
	}
	defer l.release()
	return r.forget(ids)
}

// forget removes snapshot files; the caller holds the exclusive lock.
func (r *Repo) forget(ids []string) ([]string, error) {
	var snaps []Snapshot
	for _, id := range ids {
		s, err := r.resolveSnapshot(id)
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, s)
	}
	var done []string
	for _, s := range snaps {
		if err := os.Remove(filepath.Join(r.dir, "snapshots", s.ID+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return done, err
		}
		done = append(done, s.ID)
	}
	return done, nil
}

// Policy is a retention policy. Each rule keeps the newest snapshot of each
// of the last N periods; a snapshot kept by any rule is kept. Policies apply
// per Source (kind, name and host).
type Policy struct {
	KeepLast    int
	KeepDaily   int
	KeepWeekly  int
	KeepMonthly int
}

// empty reports whether the policy has no rule (and therefore keeps all).
func (p Policy) empty() bool { return p == Policy{} }

// PlanPolicy splits snapshots into those a policy keeps and those it
// forgets, without changing anything. Use it to preview ApplyPolicy.
// Notes: periods use the local time zone; weeks are ISO weeks. An empty
// policy keeps everything.
func PlanPolicy(snaps []Snapshot, p Policy) (keep, forget []Snapshot) {
	if p.empty() {
		return slices.Clone(snaps), nil
	}
	groups := map[string][]Snapshot{}
	var order []string
	for _, s := range snaps {
		k := s.Source.key()
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], s)
	}
	for _, k := range order {
		g := groups[k]
		slices.SortStableFunc(g, func(a, b Snapshot) int { return b.Time.Compare(a.Time) })
		kept := make([]bool, len(g))
		for i := 0; i < len(g) && i < p.KeepLast; i++ {
			kept[i] = true
		}
		bucket := func(n int, key func(time.Time) string) {
			last := ""
			for i := 0; i < len(g) && n > 0; i++ {
				b := key(g[i].Time.Local())
				if b != last {
					kept[i] = true
					last = b
					n--
				}
			}
		}
		bucket(p.KeepDaily, func(t time.Time) string { return t.Format("2006-01-02") })
		bucket(p.KeepWeekly, func(t time.Time) string {
			y, w := t.ISOWeek()
			return fmt.Sprintf("%d-%02d", y, w)
		})
		bucket(p.KeepMonthly, func(t time.Time) string { return t.Format("2006-01") })
		for i, s := range g {
			if kept[i] {
				keep = append(keep, s)
			} else {
				forget = append(forget, s)
			}
		}
	}
	return keep, forget
}

// ApplyPolicy forgets every snapshot the policy does not keep.
// Returns: the forgotten snapshot ids.
// Side effects: holds an exclusive lock; deletes snapshot files.
func (r *Repo) ApplyPolicy(p Policy) ([]string, error) {
	l, err := r.lock(true)
	if err != nil {
		return nil, err
	}
	defer l.release()
	snaps, err := r.loadSnapshots()
	if err != nil {
		return nil, fmt.Errorf("wpbk: refusing to apply a policy over unreadable snapshots: %w", err)
	}
	_, forget := PlanPolicy(snaps, p)
	ids := make([]string, len(forget))
	for i, s := range forget {
		ids[i] = s.ID
	}
	return r.forget(ids)
}

// PruneReport summarizes Prune.
type PruneReport struct {
	PacksDeleted  int
	PacksRepacked int
	PacksWritten  int
	BytesFreed    int64
}

// Prune removes data no snapshot references: unused packs are deleted and
// partly used packs are rewritten with only their live chunks.
// Side effects: holds an exclusive lock. Order: new packs, then a new index
// listing every surviving pack, then removal of the old index files, then
// removal of obsolete packs, so a crash at any point loses nothing that a
// snapshot references.
// Notes: Prune refuses to run when a snapshot is unreadable or its entry
// list cannot be read, because it could not tell which chunks are live.
// Packs whose trailer is unreadable and that no index lists are left alone.
func (r *Repo) Prune(ctx context.Context) (PruneReport, error) {
	var rep PruneReport
	l, err := r.lock(true)
	if err != nil {
		return rep, err
	}
	defer l.release()
	r.removeTemps()

	idx, err := r.loadIndex(true)
	if err != nil {
		return rep, err
	}
	snaps, err := r.loadSnapshots()
	if err != nil {
		return rep, fmt.Errorf("wpbk: prune: %w", err)
	}
	br := r.newBlobReader(idx)
	defer br.close()
	used := map[string]bool{}
	for _, s := range snaps {
		for _, c := range s.Tree {
			used[c] = true
		}
		err := walkTree(ctx, br, s, func(e Entry) error {
			for _, c := range e.Chunks {
				used[c] = true
			}
			return nil
		})
		if err != nil {
			return rep, fmt.Errorf("wpbk: prune: cannot read snapshot %s, run Check: %w", s.ID, err)
		}
	}

	// Visit fully used packs first so they claim shared chunks.
	ids := sortedKeys(idx.packs)
	usedCount := func(id string) int {
		n := 0
		for _, b := range idx.packs[id].Blobs {
			if used[b.ID] {
				n++
			}
		}
		return n
	}
	slices.SortStableFunc(ids, func(a, b string) int {
		fa := float64(usedCount(a)) / float64(max(1, len(idx.packs[a].Blobs)))
		fb := float64(usedCount(b)) / float64(max(1, len(idx.packs[b].Blobs)))
		switch {
		case fa > fb:
			return -1
		case fa < fb:
			return 1
		}
		return 0
	})

	claimed := map[string]bool{}
	var keep []indexPack
	var obsolete []string
	s := &saver{r: r, idx: &memIndex{blobs: map[string][]location{}}, pending: map[string]bool{}}
	defer s.abort()
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		p := idx.packs[id]
		size, present := idx.onDisk[id]
		if !present {
			continue // dropped from the new index
		}
		var live []BlobRef
		for _, b := range p.Blobs {
			if used[b.ID] && !claimed[b.ID] && b.Offset+b.Length <= size {
				live = append(live, b)
			}
		}
		switch {
		case len(live) == len(p.Blobs):
			keep = append(keep, p)
		case len(live) == 0:
			obsolete = append(obsolete, id)
			rep.PacksDeleted++
		default:
			if err := r.repack(s, id, live); err != nil {
				return rep, err
			}
			obsolete = append(obsolete, id)
			rep.PacksRepacked++
		}
		for _, b := range live {
			claimed[b.ID] = true
		}
	}
	if err := s.flush(); err != nil {
		return rep, err
	}
	rep.PacksWritten = len(s.written)
	if len(obsolete) == 0 && len(idx.unindexed) == 0 && len(idx.files) <= 1 && len(idx.badFiles) == 0 {
		return rep, nil
	}
	newIndex, err := r.writeIndex(append(keep, s.written...))
	if err != nil {
		return rep, err
	}
	for _, f := range idx.files {
		if f != newIndex {
			os.Remove(filepath.Join(r.dir, "index", f+".json"))
		}
	}
	for name := range idx.badFiles {
		os.Remove(filepath.Join(r.dir, "index", name))
	}
	for _, id := range obsolete {
		rep.BytesFreed += idx.onDisk[id]
		if err := os.Remove(r.packPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return rep, err
		}
	}
	for _, p := range s.written {
		rep.BytesFreed -= p.Size
	}
	return rep, nil
}

// repack copies the live blobs of pack id into the saver's packs,
// verifying each one first.
func (r *Repo) repack(s *saver, id string, live []BlobRef) error {
	f, err := os.Open(r.packPath(id))
	if err != nil {
		return err
	}
	defer f.Close()
	for _, b := range live {
		if _, err := r.readBlob(f, b); err != nil {
			return fmt.Errorf("wpbk: prune: pack %s: %w", id, err)
		}
		raw := make([]byte, b.Length)
		if _, err := f.ReadAt(raw, b.Offset); err != nil {
			return err
		}
		if s.pw == nil {
			pw, err := r.newPackWriter()
			if err != nil {
				return err
			}
			s.pw = pw
		}
		if err := s.pw.add(b.ID, raw, b.RawLength); err != nil {
			return err
		}
		if s.pw.size() >= r.cfg.PackSize {
			if err := s.flush(); err != nil {
				return err
			}
		}
	}
	return nil
}

// removeTemps deletes leftover temporary files of interrupted writers.
// Notes: only called under the exclusive lock, so no writer is active.
func (r *Repo) removeTemps() {
	for _, d := range []string{"packs", "index", "snapshots"} {
		ents, _ := os.ReadDir(filepath.Join(r.dir, d))
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), "tmp-") && strings.HasSuffix(e.Name(), ".partial") {
				os.Remove(filepath.Join(r.dir, d, e.Name()))
			}
		}
	}
}

// RebuildReport summarizes RebuildIndex.
type RebuildReport struct {
	Packs  int
	Chunks int
	// Damaged lists packs whose trailer is unreadable; their old index
	// entries are kept when one exists.
	Damaged []string
}

// RebuildIndex replaces every index file with one built from the pack
// trailers, recovering from lost or corrupt index files.
// Side effects: holds an exclusive lock; writes a new index, then removes
// the old ones.
func (r *Repo) RebuildIndex(ctx context.Context) (RebuildReport, error) {
	var rep RebuildReport
	l, err := r.lock(true)
	if err != nil {
		return rep, err
	}
	defer l.release()
	r.removeTemps()
	old, err := r.loadIndex(false)
	if err != nil {
		return rep, err
	}
	var packs []indexPack
	for _, id := range sortedKeys(old.onDisk) {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		blobs, size, err := readTrailer(r.packPath(id))
		if err != nil {
			rep.Damaged = append(rep.Damaged, id)
			if p, ok := old.packs[id]; ok {
				packs = append(packs, p)
			}
			continue
		}
		packs = append(packs, indexPack{ID: id, Size: size, Blobs: blobs})
	}
	seen := map[string]bool{}
	for _, p := range packs {
		rep.Packs++
		for _, b := range p.Blobs {
			seen[b.ID] = true
		}
	}
	rep.Chunks = len(seen)
	newIndex, err := r.writeIndex(packs)
	if err != nil {
		return rep, err
	}
	for _, f := range old.files {
		if f != newIndex {
			os.Remove(filepath.Join(r.dir, "index", f+".json"))
		}
	}
	for name := range old.badFiles {
		os.Remove(filepath.Join(r.dir, "index", name))
	}
	return rep, nil
}

// Stats summarizes repository space use.
type Stats struct {
	Snapshots int
	// LogicalBytes is the sum of every snapshot's content size.
	LogicalBytes int64
	// UniqueChunks and UniqueBytes count distinct chunks and their
	// uncompressed size.
	UniqueChunks int
	UniqueBytes  int64
	// Packs and StoredBytes count pack files and their size on disk.
	Packs       int
	StoredBytes int64
	// DedupRatio is LogicalBytes / StoredBytes (0 when nothing is stored):
	// how many bytes of backed-up content each stored byte represents.
	DedupRatio float64
}

// Stats computes space statistics from snapshots, index and packs.
// Side effects: none; no lock is taken.
func (r *Repo) Stats() (Stats, error) {
	var st Stats
	snaps, err := r.loadSnapshots()
	if err != nil && snaps == nil {
		return st, err
	}
	st.Snapshots = len(snaps)
	for _, s := range snaps {
		st.LogicalBytes += s.Totals.Bytes
	}
	idx, err := r.loadIndex(true)
	if err != nil {
		return st, err
	}
	for _, locs := range idx.blobs {
		st.UniqueChunks++
		st.UniqueBytes += locs[0].ref.RawLength
	}
	for _, size := range idx.onDisk {
		st.Packs++
		st.StoredBytes += size
	}
	if st.StoredBytes > 0 {
		st.DedupRatio = float64(st.LogicalBytes) / float64(st.StoredBytes)
	}
	return st, nil
}
