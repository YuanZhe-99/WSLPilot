package wpbk

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeebo/blake3"
)

// packWriter streams blobs into a temporary pack file and publishes it under
// its content id on finish.
type packWriter struct {
	r     *Repo
	f     *os.File
	tmp   string
	h     *blake3.Hasher
	w     io.Writer
	off   int64
	blobs []BlobRef
}

// newPackWriter creates a temporary pack in packs/ and writes its header.
// Side effects: creates packs/tmp-<random>.partial.
func (r *Repo) newPackWriter() (*packWriter, error) {
	dir := filepath.Join(r.dir, "packs")
	f, err := os.CreateTemp(dir, "tmp-*.partial")
	if err != nil {
		return nil, err
	}
	pw := &packWriter{r: r, f: f, tmp: f.Name(), h: blake3.New()}
	pw.w = io.MultiWriter(f, pw.h)
	if _, err := io.WriteString(pw.w, packHeaderMagic); err != nil {
		pw.abort()
		return nil, err
	}
	pw.off = int64(len(packHeaderMagic))
	return pw, nil
}

// add appends one stored blob.
// Inputs: id is the chunk id, data the zstd frame (sealed in an encrypted
// repository), raw the plaintext length.
func (pw *packWriter) add(id string, data []byte, raw int64) error {
	if _, err := pw.w.Write(data); err != nil {
		return err
	}
	pw.blobs = append(pw.blobs, BlobRef{ID: id, Offset: pw.off, Length: int64(len(data)), RawLength: raw})
	pw.off += int64(len(data))
	return nil
}

// size returns the bytes written so far.
func (pw *packWriter) size() int64 { return pw.off }

// finish writes the trailer and footer, syncs the file and renames it to
// packs/<id[:2]>/<id>.pack.
// Returns: the pack's index entry.
// Side effects: the pack becomes visible only after it is complete on disk.
func (pw *packWriter) finish() (indexPack, error) {
	trailer, err := json.Marshal(packTrailer{Version: FormatVersion, Blobs: pw.blobs})
	if err != nil {
		pw.abort()
		return indexPack{}, err
	}
	trailer = pw.r.seal(aadTrailer, trailer)
	footer := make([]byte, packFooterSize)
	binary.LittleEndian.PutUint64(footer, uint64(len(trailer)))
	copy(footer[8:], packFooterMagic)
	if _, err := pw.w.Write(trailer); err != nil {
		pw.abort()
		return indexPack{}, err
	}
	if _, err := pw.w.Write(footer); err != nil {
		pw.abort()
		return indexPack{}, err
	}
	size := pw.off + int64(len(trailer)) + packFooterSize
	if err := pw.f.Sync(); err != nil {
		pw.abort()
		return indexPack{}, err
	}
	if err := pw.f.Close(); err != nil {
		os.Remove(pw.tmp)
		return indexPack{}, err
	}
	id := hex.EncodeToString(pw.h.Sum(nil))
	final := pw.r.packPath(id)
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		os.Remove(pw.tmp)
		return indexPack{}, err
	}
	if err := os.Rename(pw.tmp, final); err != nil {
		os.Remove(pw.tmp)
		return indexPack{}, err
	}
	syncDir(filepath.Dir(final))
	return indexPack{ID: id, Size: size, Blobs: pw.blobs}, nil
}

// abort discards the temporary pack.
func (pw *packWriter) abort() {
	pw.f.Close()
	os.Remove(pw.tmp)
}

// packPath returns where pack id lives.
func (r *Repo) packPath(id string) string {
	return filepath.Join(r.dir, "packs", id[:2], id+".pack")
}

// listPacks returns the size of every well-named pack file on disk.
// Notes: temporary files and foreign names are ignored.
func (r *Repo) listPacks() (map[string]int64, error) {
	out := map[string]int64{}
	root := filepath.Join(r.dir, "packs")
	subs, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, s := range subs {
		if !s.IsDir() || len(s.Name()) != 2 {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, s.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			id, ok := strings.CutSuffix(f.Name(), ".pack")
			if !ok || !validID(id) || id[:2] != s.Name() || f.IsDir() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				return nil, err
			}
			out[id] = info.Size()
		}
	}
	return out, nil
}

// errPackDamaged marks a pack whose framing or trailer cannot be read.
var errPackDamaged = errors.New("pack damaged")

// readTrailer parses the trailer of the pack at path, decrypting it in an
// encrypted repository.
// Returns: the blob list and file size, or an error wrapping errPackDamaged
// (also when a sealed trailer fails authentication).
func (r *Repo) readTrailer(path string) ([]BlobRef, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	size := info.Size()
	if size < int64(len(packHeaderMagic))+packFooterSize {
		return nil, size, fmt.Errorf("%w: %d bytes is too short", errPackDamaged, size)
	}
	head := make([]byte, len(packHeaderMagic))
	if _, err := f.ReadAt(head, 0); err != nil {
		return nil, size, fmt.Errorf("%w: %v", errPackDamaged, err)
	}
	if string(head) != packHeaderMagic {
		return nil, size, fmt.Errorf("%w: bad header magic", errPackDamaged)
	}
	footer := make([]byte, packFooterSize)
	if _, err := f.ReadAt(footer, size-packFooterSize); err != nil {
		return nil, size, fmt.Errorf("%w: %v", errPackDamaged, err)
	}
	if string(footer[8:]) != packFooterMagic {
		return nil, size, fmt.Errorf("%w: bad footer magic (truncated?)", errPackDamaged)
	}
	tlen := binary.LittleEndian.Uint64(footer)
	body := size - packFooterSize - int64(len(packHeaderMagic))
	if tlen > uint64(body) {
		return nil, size, fmt.Errorf("%w: trailer length %d exceeds pack", errPackDamaged, tlen)
	}
	tstart := size - packFooterSize - int64(tlen)
	raw := make([]byte, tlen)
	if _, err := f.ReadAt(raw, tstart); err != nil {
		return nil, size, fmt.Errorf("%w: %v", errPackDamaged, err)
	}
	if raw, err = r.unseal(aadTrailer, raw); err != nil {
		return nil, size, fmt.Errorf("%w: trailer: %v", errPackDamaged, err)
	}
	var t packTrailer
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, size, fmt.Errorf("%w: trailer: %v", errPackDamaged, err)
	}
	if t.Version < 1 || t.Version > FormatVersion {
		return nil, size, fmt.Errorf("%w: trailer version %d", ErrUnsupportedVersion, t.Version)
	}
	for _, b := range t.Blobs {
		if !validID(b.ID) || b.Offset < int64(len(packHeaderMagic)) || b.Length <= 0 || b.Offset+b.Length > tstart {
			return nil, size, fmt.Errorf("%w: blob %s out of range", errPackDamaged, b.ID)
		}
	}
	return t.Blobs, size, nil
}

// packHash returns the BLAKE3-256 of the whole file at path.
func packHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := blake3.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// readBlob reads, decrypts (in an encrypted repository), decompresses and
// verifies one blob.
// Returns: the plaintext, or an error wrapping ErrChunkCorrupt when the bytes
// are present but wrong (including a failed GCM tag).
func (r *Repo) readBlob(f io.ReaderAt, ref BlobRef) ([]byte, error) {
	buf := make([]byte, ref.Length)
	n, err := f.ReadAt(buf, ref.Offset)
	if n < len(buf) {
		if err == nil || err == io.EOF {
			return nil, fmt.Errorf("%w: %s: short read", ErrChunkMissing, ref.ID)
		}
		return nil, err
	}
	buf, err = r.unseal(blobAAD(ref.ID), buf)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrChunkCorrupt, ref.ID, err)
	}
	plain, err := r.dec.DecodeAll(buf, make([]byte, 0, min(max(ref.RawLength, 0), 8<<20)))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrChunkCorrupt, ref.ID, err)
	}
	if int64(len(plain)) != ref.RawLength || r.chunkID(plain) != ref.ID {
		return nil, fmt.Errorf("%w: %s: hash mismatch", ErrChunkCorrupt, ref.ID)
	}
	return plain, nil
}
