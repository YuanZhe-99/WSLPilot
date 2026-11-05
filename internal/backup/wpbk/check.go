package wpbk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ProblemKind classifies a Check finding.
type ProblemKind string

// Problem kinds reported by Check.
const (
	// ProblemIndexUnreadable: an index file cannot be read or parsed.
	ProblemIndexUnreadable ProblemKind = "index-unreadable"
	// ProblemPackMissing: an index lists a pack that is not on disk.
	ProblemPackMissing ProblemKind = "pack-missing"
	// ProblemPackDamaged: a pack's framing, trailer or hash is wrong.
	ProblemPackDamaged ProblemKind = "pack-damaged"
	// ProblemPackUnindexed: a pack on disk is in no index file (benign after
	// an interrupted backup; RebuildIndex or Prune resolves it).
	ProblemPackUnindexed ProblemKind = "pack-unindexed"
	// ProblemChunkMissing: a referenced chunk has no readable copy.
	ProblemChunkMissing ProblemKind = "chunk-missing"
	// ProblemChunkCorrupt: a stored copy fails decompression or its hash.
	ProblemChunkCorrupt ProblemKind = "chunk-corrupt"
	// ProblemChunkUnindexed: a referenced chunk is only reachable through
	// the trailer of an unindexed pack; RebuildIndex fixes it.
	ProblemChunkUnindexed ProblemKind = "chunk-unindexed"
	// ProblemSnapshotUnreadable: a snapshot file cannot be read or parsed.
	ProblemSnapshotUnreadable ProblemKind = "snapshot-unreadable"
	// ProblemSnapshotDamaged: a snapshot's entry list cannot be decoded.
	ProblemSnapshotDamaged ProblemKind = "snapshot-damaged"
)

// Problem is one Check finding. Only the fields relevant to Kind are set.
type Problem struct {
	Kind     ProblemKind
	File     string
	Pack     string
	Chunk    string
	Snapshot string
	Detail   string
}

// String renders the problem for logs.
func (p Problem) String() string {
	var parts []string
	for _, kv := range [][2]string{{"file", p.File}, {"pack", p.Pack}, {"chunk", p.Chunk}, {"snapshot", p.Snapshot}} {
		if kv[1] != "" {
			parts = append(parts, kv[0]+"="+kv[1])
		}
	}
	s := string(p.Kind) + " " + strings.Join(parts, " ")
	if p.Detail != "" {
		s += ": " + p.Detail
	}
	return s
}

// CheckReport is the result of Check.
type CheckReport struct {
	Problems []Problem
	// AffectedSnapshots lists snapshots that cannot be fully restored.
	AffectedSnapshots []string
	Packs             int
	Chunks            int
	Snapshots         int
}

// Healthy reports whether Check found nothing at all.
func (c CheckReport) Healthy() bool { return len(c.Problems) == 0 }

// Has reports whether any problem of kind k was found.
func (c CheckReport) Has(k ProblemKind) bool {
	return slices.ContainsFunc(c.Problems, func(p Problem) bool { return p.Kind == k })
}

// Check verifies the repository structure and, with readData, every stored
// chunk (decompressed and rehashed) and every pack hash.
// Returns: a report of problems; the error is only for failures to run.
// Side effects: holds a shared lock unless the repository is read-only.
func (r *Repo) Check(ctx context.Context, readData bool) (CheckReport, error) {
	l, err := r.lock(false)
	if err != nil {
		return CheckReport{}, err
	}
	defer l.release()
	var rep CheckReport
	add := func(p Problem) { rep.Problems = append(rep.Problems, p) }

	idx, err := r.loadIndex(true)
	if err != nil {
		return rep, err
	}
	for _, name := range sortedKeys(idx.badFiles) {
		add(Problem{Kind: ProblemIndexUnreadable, File: "index/" + name, Detail: idx.badFiles[name].Error()})
	}
	for _, id := range sortedKeys(idx.badPacks) {
		add(Problem{Kind: ProblemPackDamaged, Pack: id, Detail: "unindexed pack: " + idx.badPacks[id].Error()})
	}
	unindexed := map[string]bool{}
	for _, id := range idx.unindexed {
		unindexed[id] = true
		add(Problem{Kind: ProblemPackUnindexed, Pack: id})
	}

	// good[chunk] is true when some copy is readable (as far as checked);
	// viaIndex[chunk] when such a copy is in an indexed pack.
	good := map[string]bool{}
	viaIndex := map[string]bool{}
	br := r.newBlobReader(idx)
	defer br.close()
	for _, id := range sortedKeys(idx.packs) {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		p := idx.packs[id]
		rep.Packs++
		size, present := idx.onDisk[id]
		if !present {
			add(Problem{Kind: ProblemPackMissing, Pack: id, Detail: fmt.Sprintf("%d chunks lost", len(p.Blobs))})
			continue
		}
		if !unindexed[id] {
			if blobs, _, err := r.readTrailer(r.packPath(id)); err != nil {
				add(Problem{Kind: ProblemPackDamaged, Pack: id, Detail: err.Error()})
			} else if !slices.Equal(blobs, p.Blobs) {
				add(Problem{Kind: ProblemPackDamaged, Pack: id, Detail: "trailer disagrees with index"})
			}
		}
		if readData {
			if h, err := packHash(r.packPath(id)); err != nil {
				add(Problem{Kind: ProblemPackDamaged, Pack: id, Detail: err.Error()})
			} else if h != id {
				add(Problem{Kind: ProblemPackDamaged, Pack: id, Detail: "content hash does not match pack id"})
			}
		}
		for _, b := range p.Blobs {
			ok := b.Offset+b.Length <= size
			if ok && readData {
				if _, err := br.read(location{pack: id, ref: b}); err != nil {
					ok = false
					add(Problem{Kind: ProblemChunkCorrupt, Pack: id, Chunk: b.ID, Detail: err.Error()})
				}
			}
			if ok {
				good[b.ID] = true
				if !unindexed[id] {
					viaIndex[b.ID] = true
				}
			}
		}
	}
	rep.Chunks = len(idx.blobs)

	snapsDir := filepath.Join(r.dir, "snapshots")
	ents, err := os.ReadDir(snapsDir)
	if err != nil {
		return rep, err
	}
	reported := map[string]bool{}
	for _, e := range ents {
		sid, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !validID(sid) || e.IsDir() {
			continue
		}
		rep.Snapshots++
		snap, err := r.readSnapshot(sid)
		if err != nil {
			add(Problem{Kind: ProblemSnapshotUnreadable, File: "snapshots/" + e.Name(), Snapshot: sid, Detail: err.Error()})
			rep.AffectedSnapshots = append(rep.AffectedSnapshots, sid)
			continue
		}
		affected := false
		need := func(chunk string) {
			switch {
			case !good[chunk]:
				affected = true
				if !reported[chunk] {
					reported[chunk] = true
					add(Problem{Kind: ProblemChunkMissing, Chunk: chunk, Snapshot: sid})
				}
			case !viaIndex[chunk]:
				if !reported[chunk] {
					reported[chunk] = true
					add(Problem{Kind: ProblemChunkUnindexed, Chunk: chunk, Snapshot: sid})
				}
			}
		}
		treeOK := true
		for _, c := range snap.Tree {
			need(c)
			if !good[c] {
				treeOK = false
			}
		}
		if treeOK {
			err := walkTree(ctx, br, snap, func(en Entry) error {
				for _, c := range en.Chunks {
					need(c)
				}
				return nil
			})
			if err != nil {
				if ctx.Err() != nil {
					return rep, ctx.Err()
				}
				if !errors.Is(err, ErrChunkMissing) && !errors.Is(err, ErrChunkCorrupt) {
					add(Problem{Kind: ProblemSnapshotDamaged, Snapshot: sid, Detail: err.Error()})
				}
				affected = true
			}
		}
		if affected {
			rep.AffectedSnapshots = append(rep.AffectedSnapshots, sid)
		}
	}
	slices.Sort(rep.AffectedSnapshots)
	return rep, nil
}

// sortedKeys returns the keys of m in order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
