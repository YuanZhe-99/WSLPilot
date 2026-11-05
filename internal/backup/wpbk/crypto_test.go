package wpbk

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/zeebo/blake3"
)

const testPassword = "correct horse battery staple"

// newEncryptedRepo initializes an encrypted repository in a temporary
// directory.
func newEncryptedRepo(t *testing.T) *Repo {
	t.Helper()
	r, err := Init(filepath.Join(t.TempDir(), "repo"), InitOptions{Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestEncryptedRoundTripDedupPrune(t *testing.T) {
	r := newEncryptedRepo(t)
	if !r.Encrypted() || !r.Config().Encrypted() {
		t.Fatal("repository should be encrypted")
	}
	var enc Encryption
	if err := json.Unmarshal(r.Config().Encryption, &enc); err != nil || enc.Algorithm != EncryptionAESGCM ||
		enc.KDF != (KDFParams{Name: KDFArgon2id, Salt: enc.KDF.Salt, Time: 3, Memory: 65536, Threads: 4}) {
		t.Fatalf("encryption %+v %v", enc, err)
	}
	entries := sampleEntries(1)
	src := buildTar(t, entries)
	first := backup(t, r, "x", src)
	compareTars(t, parseTar(t, src), parseTar(t, restore(t, r, first.ID, nil)))

	big := slices.Clone(entries[1].data)
	copy(big[5<<20:], "changed!")
	entries[1].data = big
	entries[2].data = []byte("hello, again\n")
	tar2 := buildTar(t, entries)
	second := backup(t, r, "x", tar2)
	if second.Totals.NewChunks > 4 {
		t.Fatalf("second backup stored %d new chunks, want at most 4", second.Totals.NewChunks)
	}
	if third := backup(t, r, "x", tar2); third.Totals.NewChunks != 0 {
		t.Fatalf("identical backup stored %d chunks", third.Totals.NewChunks)
	}
	if c, err := r.Check(bg, true); err != nil || !c.Healthy() {
		t.Fatalf("check: %v %v", c.Problems, err)
	}

	// Chunk ids are keyed: the plain BLAKE3 of a chunk is not its id.
	sum := blake3.Sum256([]byte("hello, world\n"))
	plainID := hex.EncodeToString(sum[:])
	if r.chunkID([]byte("hello, world\n")) == plainID {
		t.Fatal("encrypted repository uses unkeyed chunk ids")
	}

	if _, err := r.Forget(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Prune(bg); err != nil {
		t.Fatal(err)
	}
	if c, err := r.Check(bg, true); err != nil || !c.Healthy() {
		t.Fatalf("check after prune: %v %v", c.Problems, err)
	}

	// A fresh read-only open with the password restores the survivor.
	ro, err := Open(r.Dir(), OpenOptions{ReadOnly: true, Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	compareTars(t, parseTar(t, tar2), parseTar(t, restore(t, ro, second.ID, nil)))

	// Lost index: rebuilt from the encrypted trailers.
	ents, _ := os.ReadDir(filepath.Join(r.Dir(), "index"))
	for _, e := range ents {
		os.Remove(filepath.Join(r.Dir(), "index", e.Name()))
	}
	if rb, err := r.RebuildIndex(bg); err != nil || rb.Packs == 0 || len(rb.Damaged) != 0 {
		t.Fatalf("rebuild %+v %v", rb, err)
	}
	if c, err := r.Check(bg, true); err != nil || !c.Healthy() {
		t.Fatalf("check after rebuild: %v %v", c.Problems, err)
	}
	compareTars(t, parseTar(t, tar2), parseTar(t, restore(t, r, second.ID, nil)))
}

func TestEncryptedPasswords(t *testing.T) {
	r := newEncryptedRepo(t)
	src := buildTar(t, sampleEntries(3))
	snap := backup(t, r, "x", src)
	dir := r.Dir()

	if _, err := Open(dir, OpenOptions{}); !errors.Is(err, ErrEncrypted) {
		t.Fatalf("open without password: %v", err)
	}
	if _, err := Open(dir, OpenOptions{ReadOnly: true}); !errors.Is(err, ErrEncrypted) {
		t.Fatalf("read-only open without password: %v", err)
	}
	if _, err := Open(dir, OpenOptions{Password: "wrong"}); !errors.Is(err, ErrPassword) {
		t.Fatalf("open with wrong password: %v", err)
	}
	if err := r.ChangePassword("wrong", "new password"); !errors.Is(err, ErrPassword) {
		t.Fatalf("change with wrong old password: %v", err)
	}
	if err := r.ChangePassword(testPassword, ""); err == nil {
		t.Fatal("empty new password must be refused")
	}
	if err := r.ChangePassword(testPassword, "new password"); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, OpenOptions{Password: testPassword}); !errors.Is(err, ErrPassword) {
		t.Fatalf("old password after change: %v", err)
	}
	r2, err := Open(dir, OpenOptions{ReadOnly: true, Password: "new password"})
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	compareTars(t, parseTar(t, src), parseTar(t, restore(t, r2, snap.ID, nil)))
	// The open handle keeps working after the change.
	compareTars(t, parseTar(t, src), parseTar(t, restore(t, r, snap.ID, nil)))

	// Unencrypted repositories ignore a password and refuse ChangePassword.
	plain := newTestRepo(t)
	p2, err := Open(plain.Dir(), OpenOptions{Password: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Close()
	if p2.Encrypted() || string(p2.Config().Encryption) != "null" {
		t.Fatal("plain repository reported as encrypted")
	}
	if err := p2.ChangePassword("", "x"); !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("change password on plain repository: %v", err)
	}
}

// distinctive strings of the sample tar and backup metadata that must not
// appear in plaintext anywhere in an encrypted repository.
var leakNeedles = []string{"small.txt", "hello, world", "hello xattr", "alice", "staff", "ustar ustar ", "secret-source-name", "manual-tag-x"}

// repoFiles returns the raw bytes of every file under dir.
func repoFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			out[p] = b
		}
		return nil
	})
	return out
}

func TestEncryptedNoPlaintext(t *testing.T) {
	// Use a highly compressible variant too, so literals would survive zstd.
	entries := sampleEntries(4)
	entries[1].data = bytes.Repeat([]byte("hello, world "), 1<<16)
	src := buildTar(t, entries)
	meta := SnapshotMeta{Source: Source{Kind: "wsl-distro", Name: "secret-source-name"}, Tags: []string{"manual-tag-x"}}

	// Control: an unencrypted repository does show the source name.
	plain := newTestRepo(t)
	if _, err := plain.Backup(bg, bytes.NewReader(src), meta, nil); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range repoFiles(t, plain.Dir()) {
		found = found || bytes.Contains(b, []byte("secret-source-name"))
	}
	if !found {
		t.Fatal("control: unencrypted repository should contain the source name")
	}

	r := newEncryptedRepo(t)
	if _, err := r.Backup(bg, bytes.NewReader(src), meta, nil); err != nil {
		t.Fatal(err)
	}
	files := repoFiles(t, r.Dir())
	if len(files) < 4 {
		t.Fatalf("only %d files", len(files))
	}
	for p, b := range files {
		for _, n := range leakNeedles {
			if bytes.Contains(b, []byte(n)) {
				t.Errorf("%s contains %q in plaintext", p, n)
			}
		}
	}
}

func TestEncryptedTamperedPack(t *testing.T) {
	r := newEncryptedRepo(t)
	snap := backup(t, r, "x", buildTar(t, sampleEntries(5)))
	packs := packsOnDisk(t, r)
	if len(packs) != 1 {
		t.Fatalf("%d packs", len(packs))
	}
	path := r.packPath(packs[0])
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
	one[0] ^= 0x01
	f.WriteAt(one, pos)
	f.Close()

	rep, err := r.Check(bg, true)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := slices.ContainsFunc(rep.Problems, func(p Problem) bool { return p.Kind == ProblemChunkCorrupt && p.Chunk == blobs[0].ID })
	if !corrupt || !slices.Equal(rep.AffectedSnapshots, []string{snap.ID}) {
		t.Fatalf("report: %v affected %v", rep.Problems, rep.AffectedSnapshots)
	}
	if err := r.Restore(bg, snap.ID, &bytes.Buffer{}, nil, nil); !errors.Is(err, ErrChunkCorrupt) {
		t.Fatalf("restore of tampered snapshot: %v", err)
	}
}
