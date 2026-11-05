package wpbk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var bg = context.Background()

// newTestRepo initializes a repository in a temporary directory.
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	r, err := Init(filepath.Join(t.TempDir(), "repo"), InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

// backup stores data as a snapshot of source name.
func backup(t *testing.T, r *Repo, name string, data []byte) Snapshot {
	t.Helper()
	s, err := r.Backup(bg, bytes.NewReader(data), SnapshotMeta{Source: Source{Kind: "wsl-distro", Name: name, Host: "test"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// restore returns a snapshot as tar bytes.
func restore(t *testing.T, r *Repo, id string, filter func(string) bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := r.Restore(bg, id, &buf, filter, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// packsOnDisk lists pack ids.
func packsOnDisk(t *testing.T, r *Repo) []string {
	t.Helper()
	m, err := r.listPacks()
	if err != nil {
		t.Fatal(err)
	}
	return sortedKeys(m)
}

func TestRoundTrip(t *testing.T) {
	r := newTestRepo(t)
	src := buildTar(t, sampleEntries(1))
	var calls int
	snap, err := r.Backup(bg, bytes.NewReader(src), SnapshotMeta{Source: Source{Kind: "wsl-distro", Name: "Ubuntu"}, Tags: []string{"manual"}}, func(Progress) { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	if calls == 0 || snap.Totals.Entries != 11 || snap.Totals.Bytes < 10<<20 {
		t.Fatalf("unexpected snapshot totals %+v (progress calls %d)", snap.Totals, calls)
	}
	compareTars(t, parseTar(t, src), parseTar(t, restore(t, r, snap.ID, nil)))

	// Restore by prefix, through a fresh Open.
	r2, err := Open(r.Dir(), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	compareTars(t, parseTar(t, src), parseTar(t, restore(t, r2, snap.ID[:10], nil)))

	rep, err := r.Check(bg, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Healthy() {
		t.Fatalf("check: %v", rep.Problems)
	}
	if entries, err := os.ReadDir(filepath.Join(r.Dir(), "locks")); err != nil || len(entries) != 0 {
		t.Fatalf("locks left behind: %v %v", entries, err)
	}
}

func TestPartialRestoreHardlinkTarget(t *testing.T) {
	r := newTestRepo(t)
	src := buildTar(t, sampleEntries(1))
	snap := backup(t, r, "x", src)
	got := parseTar(t, restore(t, r, snap.ID, func(n string) bool { return n == "root/hard" }))
	if len(got) != 1 {
		t.Fatalf("got %d entries", len(got))
	}
	h := got[0].hdr
	if h.Typeflag != '0' || h.Linkname != "" || string(got[0].data) != "hello, world\n" || h.Mode != 0o4755 {
		t.Fatalf("hard link with filtered target not materialized: %+v %q", h, got[0].data)
	}
	// With its target included it stays a hard link.
	got = parseTar(t, restore(t, r, snap.ID, func(n string) bool { return n == "root/hard" || n == "root/small.txt" }))
	if len(got) != 2 || got[1].hdr.Typeflag != '1' {
		t.Fatalf("hard link not kept: %+v", got)
	}
}

func TestEntries(t *testing.T) {
	r := newTestRepo(t)
	snap := backup(t, r, "x", buildTar(t, sampleEntries(1)))
	var names []string
	if err := r.Entries(bg, snap.ID, func(e Entry) error { names = append(names, string(e.Name)); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(names) != 11 || names[4] != "root/caf\xe9 \"q\" \\x" {
		t.Fatalf("entries: %q", names)
	}
}

func TestDedup(t *testing.T) {
	r := newTestRepo(t)
	entries := sampleEntries(1)
	first := backup(t, r, "x", buildTar(t, entries))
	// Change a few bytes in the middle of the big file and the small file.
	big := slices.Clone(entries[1].data)
	copy(big[5<<20:], "changed!")
	entries[1].data = big
	entries[2].data = []byte("hello, again\n")
	second := backup(t, r, "x", buildTar(t, entries))
	if first.Totals.NewChunks < 10 {
		t.Fatalf("first backup stored only %d chunks", first.Totals.NewChunks)
	}
	if second.Totals.NewChunks > 4 {
		t.Fatalf("second backup stored %d new chunks, want at most 4", second.Totals.NewChunks)
	}
	if second.Parent != first.ID {
		t.Fatalf("parent %q, want %q", second.Parent, first.ID)
	}
	// An identical third backup stores nothing, not even a new tree chunk.
	third := backup(t, r, "x", buildTar(t, entries))
	if third.Totals.NewChunks != 0 {
		t.Fatalf("identical backup stored %d chunks", third.Totals.NewChunks)
	}
	compareTars(t, parseTar(t, buildTar(t, entries)), parseTar(t, restore(t, r, second.ID, nil)))
	st, err := r.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Snapshots != 3 || st.DedupRatio < 2.5 {
		t.Fatalf("stats %+v", st)
	}
}

func TestForgetPrune(t *testing.T) {
	r := newTestRepo(t)
	shared := tarEntry{hdr: sampleEntries(0)[2].hdr, data: randomBytes(7, 3<<20)}
	shared.hdr.Name, shared.hdr.PAXRecords = "shared.bin", nil
	onlyA := tarEntry{hdr: shared.hdr, data: randomBytes(8, 6<<20)}
	onlyA.hdr.Name = "a.bin"
	onlyB := tarEntry{hdr: shared.hdr, data: randomBytes(9, 2<<20)}
	onlyB.hdr.Name = "b.bin"
	tarA := buildTar(t, []tarEntry{shared, onlyA})
	tarB := buildTar(t, []tarEntry{shared, onlyB})
	a := backup(t, r, "x", tarA)
	b := backup(t, r, "x", tarB)
	before, _ := r.Stats()

	if _, err := r.Forget(a.ID); err != nil {
		t.Fatal(err)
	}
	rep, err := r.Prune(bg)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := r.Stats()
	if rep.PacksRepacked != 1 || rep.BytesFreed < 5<<20 || after.StoredBytes >= before.StoredBytes-5<<20 {
		t.Fatalf("prune report %+v, stored %d -> %d", rep, before.StoredBytes, after.StoredBytes)
	}
	compareTars(t, parseTar(t, tarB), parseTar(t, restore(t, r, b.ID, nil)))
	if c, err := r.Check(bg, true); err != nil || !c.Healthy() {
		t.Fatalf("check after prune: %v %v", c.Problems, err)
	}
	idx, _ := os.ReadDir(filepath.Join(r.Dir(), "index"))
	if len(idx) != 1 {
		t.Fatalf("prune left %d index files", len(idx))
	}
	// Pruning again changes nothing.
	if rep, err := r.Prune(bg); err != nil || rep != (PruneReport{}) {
		t.Fatalf("second prune %+v %v", rep, err)
	}
}

func TestApplyPolicy(t *testing.T) {
	r := newTestRepo(t)
	data := buildTar(t, []tarEntry{{hdr: sampleEntries(0)[10].hdr, data: []byte("x")}})
	day := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	for i := range 10 {
		for j := range 2 {
			when := day.AddDate(0, 0, i).Add(time.Duration(j) * time.Hour)
			if _, err := r.Backup(bg, bytes.NewReader(data), SnapshotMeta{Source: Source{Kind: "k", Name: "n"}, Time: when}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	other := backup(t, r, "other", data)
	gone, err := r.ApplyPolicy(Policy{KeepLast: 3, KeepDaily: 4})
	if err != nil {
		t.Fatal(err)
	}
	snaps, _ := r.Snapshots()
	// KeepLast 3 covers days 10 (2) and 9 (1); KeepDaily 4 adds days 8 and 7.
	if len(gone) != 15 || len(snaps) != 6 || !slices.ContainsFunc(snaps, func(s Snapshot) bool { return s.ID == other.ID }) {
		t.Fatalf("forgot %d, kept %d", len(gone), len(snaps))
	}
	if keep, forget := PlanPolicy(snaps, Policy{}); len(keep) != 6 || forget != nil {
		t.Fatal("empty policy must keep everything")
	}
}

// twoSnapshotRepo returns a repository holding two snapshots whose data
// lives in different packs, and the pack each one wrote.
func twoSnapshotRepo(t *testing.T) (r *Repo, a, b Snapshot, packA, packB string, tarA, tarB []byte) {
	r = newTestRepo(t)
	tarA = buildTar(t, sampleEntries(1))
	a = backup(t, r, "a", tarA)
	packs := packsOnDisk(t, r)
	if len(packs) != 1 {
		t.Fatalf("%d packs", len(packs))
	}
	packA = packs[0]
	tarB = buildTar(t, distinct(sampleEntries(2), "-b"))
	b = backup(t, r, "b", tarB)
	for _, p := range packsOnDisk(t, r) {
		if p != packA {
			packB = p
		}
	}
	return
}

func TestCheckTruncatedPack(t *testing.T) {
	r, a, b, packA, _, _, tarB := twoSnapshotRepo(t)
	path := r.packPath(packA)
	info, _ := os.Stat(path)
	if err := os.Truncate(path, info.Size()/2); err != nil {
		t.Fatal(err)
	}
	rep, err := r.Check(bg, false)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Has(ProblemPackDamaged) || !rep.Has(ProblemChunkMissing) || !slices.Equal(rep.AffectedSnapshots, []string{a.ID}) {
		t.Fatalf("report: %v affected %v", rep.Problems, rep.AffectedSnapshots)
	}
	compareTars(t, parseTar(t, tarB), parseTar(t, restore(t, r, b.ID, nil)))
	if err := r.Restore(bg, a.ID, &bytes.Buffer{}, nil, nil); !errors.Is(err, ErrChunkMissing) {
		t.Fatalf("restore of damaged snapshot: %v", err)
	}
	// A new backup of the same data must not reuse the lost chunks.
	c := backup(t, r, "a", buildTar(t, sampleEntries(1)))
	if c.Totals.NewChunks == 0 {
		t.Fatal("backup deduplicated against a truncated pack")
	}
	restore(t, r, c.ID, nil)
}

func TestCheckCorruptPack(t *testing.T) {
	r, a, b, _, packB, tarA, _ := twoSnapshotRepo(t)
	path := r.packPath(packB)
	blobs, _, err := r.readTrailer(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	pos := blobs[0].Offset + blobs[0].Length/2
	one := []byte{0}
	f.ReadAt(one, pos)
	one[0] ^= 0xff
	f.WriteAt(one, pos)
	f.Close()

	if rep, err := r.Check(bg, false); err != nil || rep.Has(ProblemChunkCorrupt) {
		t.Fatalf("structure check should not read data: %v %v", rep.Problems, err)
	}
	rep, err := r.Check(bg, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Has(ProblemChunkCorrupt) || !rep.Has(ProblemPackDamaged) || !slices.Equal(rep.AffectedSnapshots, []string{b.ID}) {
		t.Fatalf("report: %v affected %v", rep.Problems, rep.AffectedSnapshots)
	}
	compareTars(t, parseTar(t, tarA), parseTar(t, restore(t, r, a.ID, nil)))
	if err := r.Restore(bg, b.ID, &bytes.Buffer{}, nil, nil); !errors.Is(err, ErrChunkCorrupt) {
		t.Fatalf("restore of corrupt snapshot: %v", err)
	}
}

func TestLostIndexRebuild(t *testing.T) {
	r, a, b, _, _, tarA, tarB := twoSnapshotRepo(t)
	ents, _ := os.ReadDir(filepath.Join(r.Dir(), "index"))
	for _, e := range ents {
		os.Remove(filepath.Join(r.Dir(), "index", e.Name()))
	}
	rep, err := r.Check(bg, false)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Has(ProblemPackUnindexed) || !rep.Has(ProblemChunkUnindexed) || rep.Has(ProblemChunkMissing) || len(rep.AffectedSnapshots) != 0 {
		t.Fatalf("report: %v affected %v", rep.Problems, rep.AffectedSnapshots)
	}
	rb, err := r.RebuildIndex(bg)
	if err != nil || rb.Packs != 2 || len(rb.Damaged) != 0 {
		t.Fatalf("rebuild %+v %v", rb, err)
	}
	if rep, err := r.Check(bg, true); err != nil || !rep.Healthy() {
		t.Fatalf("check after rebuild: %v %v", rep.Problems, err)
	}
	compareTars(t, parseTar(t, tarA), parseTar(t, restore(t, r, a.ID, nil)))
	compareTars(t, parseTar(t, tarB), parseTar(t, restore(t, r, b.ID, nil)))
}

func TestCorruptIndexAndMissingPack(t *testing.T) {
	r, a, b, packA, _, _, tarB := twoSnapshotRepo(t)
	ents, _ := os.ReadDir(filepath.Join(r.Dir(), "index"))
	os.WriteFile(filepath.Join(r.Dir(), "index", ents[0].Name()), []byte("{garbage"), 0o644)
	os.Remove(r.packPath(packA))
	rep, err := r.Check(bg, false)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Has(ProblemIndexUnreadable) || !rep.Has(ProblemChunkMissing) || !slices.Contains(rep.AffectedSnapshots, a.ID) || slices.Contains(rep.AffectedSnapshots, b.ID) {
		t.Fatalf("report: %v affected %v", rep.Problems, rep.AffectedSnapshots)
	}
	compareTars(t, parseTar(t, tarB), parseTar(t, restore(t, r, b.ID, nil)))
}

// tree records every file's path, size and mtime under dir.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(p) // not d.Info(): NTFS directory listings cache stale times
		if err != nil {
			t.Fatal(err)
		}
		out[p] = fmt.Sprint(info.ModTime().UnixNano(), " ", info.Mode(), " ", info.Size())
		return nil
	})
	return out
}

func TestReadOnlyWritesNothing(t *testing.T) {
	w := newTestRepo(t)
	src := buildTar(t, sampleEntries(1))
	snap := backup(t, w, "x", src)
	before := tree(t, w.Dir())
	time.Sleep(20 * time.Millisecond)

	r, err := Open(w.Dir(), OpenOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Snapshots(); err != nil {
		t.Fatal(err)
	}
	compareTars(t, parseTar(t, src), parseTar(t, restore(t, r, snap.ID, nil)))
	if rep, err := r.Check(bg, true); err != nil || !rep.Healthy() {
		t.Fatal(rep.Problems, err)
	}
	r.Entries(bg, snap.ID, func(Entry) error { return nil })
	r.Stats()
	if _, err := r.Backup(bg, bytes.NewReader(src), SnapshotMeta{}, nil); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("backup: %v", err)
	}
	if _, err := r.Prune(bg); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("prune: %v", err)
	}
	if _, err := r.Forget(snap.ID); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("forget: %v", err)
	}
	if _, err := r.RebuildIndex(bg); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("rebuild: %v", err)
	}
	after := tree(t, w.Dir())
	if len(before) != len(after) {
		t.Fatalf("file set changed: %d -> %d", len(before), len(after))
	}
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("%s changed: %s -> %s", k, v, after[k])
		}
	}
}

func TestLocks(t *testing.T) {
	r := newTestRepo(t)
	data := buildTar(t, sampleEntries(0)[10:])
	host, _ := os.Hostname()
	put := func(info LockInfo) string {
		p := filepath.Join(r.Dir(), "locks", randomID()+".json")
		b, _ := json.Marshal(info)
		os.WriteFile(p, b, 0o644)
		return p
	}
	now := time.Now().UTC()
	live := put(LockInfo{Version: 1, Exclusive: true, Host: "other-pc", PID: 1, Created: now, Refreshed: now})
	_, err := r.Backup(bg, bytes.NewReader(data), SnapshotMeta{}, nil)
	var le *LockError
	if !errors.Is(err, ErrLocked) || !errors.As(err, &le) || le.Holder.Host != "other-pc" {
		t.Fatalf("backup under foreign lock: %v", err)
	}
	// A shared lock of another process blocks exclusive operations only.
	os.Remove(live)
	put(LockInfo{Version: 1, Host: "other-pc", PID: 1, Created: now, Refreshed: now})
	if _, err := r.Check(bg, false); err != nil {
		t.Fatalf("check beside shared lock: %v", err)
	}
	if _, err := r.Prune(bg); !errors.Is(err, ErrLocked) {
		t.Fatalf("prune beside shared lock: %v", err)
	}
	ents, _ := os.ReadDir(filepath.Join(r.Dir(), "locks"))
	for _, e := range ents {
		os.Remove(filepath.Join(r.Dir(), "locks", e.Name()))
	}
	// Stale: old refresh, or a dead process on this host.
	put(LockInfo{Version: 1, Exclusive: true, Host: "other-pc", PID: 1, Created: now.Add(-time.Hour), Refreshed: now.Add(-time.Hour)})
	put(LockInfo{Version: 1, Exclusive: true, Host: host, PID: 0x7ffffff0, Created: now, Refreshed: now})
	if _, err := r.Backup(bg, bytes.NewReader(data), SnapshotMeta{}, nil); err != nil {
		t.Fatalf("stale locks not broken: %v", err)
	}
	if ents, _ := os.ReadDir(filepath.Join(r.Dir(), "locks")); len(ents) != 0 {
		t.Fatalf("%d lock files left", len(ents))
	}
}

func TestVersionAndConfig(t *testing.T) {
	r := newTestRepo(t)
	cfgPath := filepath.Join(r.Dir(), "config.json")
	data, _ := os.ReadFile(cfgPath)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	raw["version"] = 2
	raw["futureField"] = true
	b, _ := json.Marshal(raw)
	os.WriteFile(cfgPath, b, 0o644)
	if _, err := Open(r.Dir(), OpenOptions{ReadOnly: true}); !errors.Is(err, ErrUnsupportedVersion) || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("open v2: %v", err)
	}
	raw["version"] = 1
	b, _ = json.Marshal(raw)
	os.WriteFile(cfgPath, b, 0o644)
	if _, err := Open(r.Dir(), OpenOptions{ReadOnly: true}); err != nil {
		t.Fatalf("unknown fields must be ignored: %v", err)
	}
	if _, err := Open(t.TempDir(), OpenOptions{}); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("open empty dir: %v", err)
	}
	if _, err := Init(r.Dir(), InitOptions{}); err == nil {
		t.Fatal("init over an existing repository must fail")
	}
}

func TestCancelLeavesNothing(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithCancel(bg)
	_, err := r.Backup(ctx, bytes.NewReader(buildTar(t, sampleEntries(1))), SnapshotMeta{}, func(p Progress) {
		if p.Bytes > 2<<20 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("backup: %v", err)
	}
	for _, d := range []string{"snapshots", "index", "locks"} {
		if ents, _ := os.ReadDir(filepath.Join(r.Dir(), d)); len(ents) != 0 {
			t.Fatalf("%s not empty after cancel: %v", d, ents)
		}
	}
	ents, _ := os.ReadDir(filepath.Join(r.Dir(), "packs"))
	for _, e := range ents {
		if !e.IsDir() {
			t.Fatalf("temporary pack left: %s", e.Name())
		}
	}
}

func TestBString(t *testing.T) {
	for _, s := range []BString{"", "plain", "caf\xe9", "\x00\xff", "日本"} {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		var got BString
		if err := json.Unmarshal(b, &got); err != nil || got != s {
			t.Fatalf("%q -> %s -> %q %v", s, b, got, err)
		}
	}
}
