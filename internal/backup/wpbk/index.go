package wpbk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// location is where one chunk can be read.
type location struct {
	pack string
	ref  BlobRef
}

// memIndex is the in-memory union of every index file, optionally completed
// with the trailers of packs no index file mentions.
type memIndex struct {
	// blobs maps a chunk id to every place it is stored, first found first.
	blobs map[string][]location
	// packs maps pack id to its blob list.
	packs map[string]indexPack
	// files lists the index file ids that were read successfully.
	files []string
	// unindexed lists packs found on disk only through their trailers.
	unindexed []string
	// badFiles maps unreadable index file names to the reason.
	badFiles map[string]error
	// badPacks maps unindexed packs whose trailer is unreadable to the reason.
	badPacks map[string]error
	// onDisk is the size of every pack file present.
	onDisk map[string]int64
}

// addPack records p in the index; a pack already known is ignored.
func (m *memIndex) addPack(p indexPack) {
	if _, ok := m.packs[p.ID]; ok {
		return
	}
	m.packs[p.ID] = p
	for _, b := range p.Blobs {
		m.blobs[b.ID] = append(m.blobs[b.ID], location{pack: p.ID, ref: b})
	}
}

// has reports whether chunk id has a copy inside a pack that is on disk
// and long enough to hold it. Backups only deduplicate against such copies,
// so a lost pack is never referenced again.
func (m *memIndex) has(id string) bool {
	for _, l := range m.blobs[id] {
		if size, ok := m.onDisk[l.pack]; ok && l.ref.Offset+l.ref.Length <= size {
			return true
		}
	}
	return false
}

// loadIndex reads every index file and, with scanUnindexed, the trailers of
// packs on disk that no index file lists.
// Returns: the merged index; unreadable files are recorded, not fatal.
// Notes: an index entry for a pack that is no longer on disk is kept, so
// Check can report it; readers fail on such chunks when they need them.
func (r *Repo) loadIndex(scanUnindexed bool) (*memIndex, error) {
	m := &memIndex{blobs: map[string][]location{}, packs: map[string]indexPack{}, badFiles: map[string]error{}, badPacks: map[string]error{}}
	dir := filepath.Join(r.dir, "index")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !validID(id) || e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			m.badFiles[e.Name()] = err
			continue
		}
		if hashID(data) != id {
			m.badFiles[e.Name()] = fmt.Errorf("content does not match its id")
			continue
		}
		if data, err = r.unseal(aadIndex, data); err != nil {
			m.badFiles[e.Name()] = err
			continue
		}
		var f indexFile
		if err := json.Unmarshal(data, &f); err != nil {
			m.badFiles[e.Name()] = err
			continue
		}
		if f.Version < 1 || f.Version > FormatVersion {
			return nil, fmt.Errorf("%w: index %s is version %d", ErrUnsupportedVersion, id, f.Version)
		}
		m.files = append(m.files, id)
		for _, p := range f.Packs {
			if validID(p.ID) {
				m.addPack(p)
			}
		}
	}
	m.onDisk, err = r.listPacks()
	if err != nil {
		return nil, err
	}
	if scanUnindexed {
		ids := make([]string, 0)
		for id := range m.onDisk {
			if _, ok := m.packs[id]; !ok {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			blobs, size, err := r.readTrailer(r.packPath(id))
			if err != nil {
				m.badPacks[id] = err
				continue
			}
			m.addPack(indexPack{ID: id, Size: size, Blobs: blobs})
			m.unindexed = append(m.unindexed, id)
		}
	}
	return m, nil
}

// writeIndex publishes an index file listing packs, sealed in an encrypted
// repository.
// Returns: the new index id ("" when packs is empty), the hash of the
// stored bytes.
// Side effects: creates index/<id>.json atomically.
func (r *Repo) writeIndex(packs []indexPack) (string, error) {
	if len(packs) == 0 {
		return "", nil
	}
	data, err := json.Marshal(indexFile{Version: FormatVersion, Packs: packs})
	if err != nil {
		return "", err
	}
	data = r.seal(aadIndex, data)
	id := hashID(data)
	return id, writeFileAtomic(filepath.Join(r.dir, "index", id+".json"), data)
}
