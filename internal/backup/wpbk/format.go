// Package wpbk implements WSLPilot's incremental backup repository: a
// versioned, content-addressed store whose unit of input is a tar stream.
//
// Each tar entry's header is kept field by field and its content is split by
// a content-defined chunker into chunks that are deduplicated across every
// snapshot in the repository, compressed with zstd and stored in pack files.
// A repository may be created encrypted (AES-256-GCM under a master key
// unlocked by an Argon2id-derived password key); see crypto.go. The on-disk format is specified in doc/en-us/backup-format.md; this package
// is one implementation of that specification, and spec_test.go proves that
// the document alone is enough to restore data.
package wpbk

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/zeebo/blake3"
)

// Format identifiers and limits of repository format version 1.
const (
	// FormatName is the value of config.json's "format" field.
	FormatName = "wpbk"
	// FormatVersion is the newest repository format version this package
	// reads and the version it writes.
	FormatVersion = 1
	// CompressionZstd is the only compression v1 defines.
	CompressionZstd = "zstd"
	// HashBlake3 is the only hash v1 defines.
	HashBlake3 = "blake3-256"
	// ChunkerFastCDC names the chunking algorithm of v1.
	ChunkerFastCDC = "fastcdc-gear-v1"
	// DefaultPackSize is the size at which a pack is closed.
	DefaultPackSize = 32 << 20
)

// Pack framing constants (see backup-format.md, "Pack files").
const (
	packHeaderMagic = "WPBKPAK1"
	packFooterMagic = "WPBKEND1"
	packFooterSize  = 16
)

// Errors returned by the repository. Callers match them with errors.Is.
var (
	// ErrNotRepository reports a directory without a valid wpbk config.json.
	ErrNotRepository = errors.New("wpbk: not a wpbk repository")
	// ErrUnsupportedVersion reports a repository written by a newer format
	// version than this package understands.
	ErrUnsupportedVersion = errors.New("wpbk: unsupported repository format version")
	// ErrReadOnly reports a write operation on a repository opened read-only.
	ErrReadOnly = errors.New("wpbk: repository is open read-only")
	// ErrLocked reports a conflicting lock held by another process.
	ErrLocked = errors.New("wpbk: repository is locked by another process")
	// ErrSnapshotNotFound reports an unknown or ambiguous snapshot id.
	ErrSnapshotNotFound = errors.New("wpbk: snapshot not found")
	// ErrChunkMissing reports a chunk that no readable pack contains.
	ErrChunkMissing = errors.New("wpbk: chunk missing from repository")
	// ErrChunkCorrupt reports a chunk whose bytes do not hash to its id.
	ErrChunkCorrupt = errors.New("wpbk: chunk is corrupt")
	// ErrEncrypted reports an encrypted repository opened without a
	// password.
	ErrEncrypted = errors.New("wpbk: repository is encrypted; a password is required")
	// ErrPassword reports a wrong password for an encrypted repository.
	ErrPassword = errors.New("wpbk: wrong repository password")
	// ErrNotEncrypted reports a password operation on an unencrypted
	// repository.
	ErrNotEncrypted = errors.New("wpbk: repository is not encrypted")
)

// Config is the content of config.json, written once by Init.
type Config struct {
	Format      string          `json:"format"`
	Version     int             `json:"version"`
	ID          string          `json:"id"`
	Created     time.Time       `json:"created"`
	Chunker     ChunkerParams   `json:"chunker"`
	Compression string          `json:"compression"`
	Hash        string          `json:"hash"`
	Encryption  json.RawMessage `json:"encryption"`
	PackSize    int64           `json:"packSize"`
}

// validate checks that a config describes a repository this package can use.
// Returns: ErrNotRepository or ErrUnsupportedVersion (wrapped) on failure.
// Notes: unknown JSON fields are ignored, which is how v1 grows additively.
func (c *Config) validate() error {
	if c.Format != FormatName {
		return fmt.Errorf("%w: format %q", ErrNotRepository, c.Format)
	}
	if c.Version < 1 || c.Version > FormatVersion {
		return fmt.Errorf("%w: repository is version %d, this build reads up to %d; update WSLPilot", ErrUnsupportedVersion, c.Version, FormatVersion)
	}
	if c.Compression != CompressionZstd {
		return fmt.Errorf("%w: compression %q", ErrUnsupportedVersion, c.Compression)
	}
	if c.Hash != HashBlake3 {
		return fmt.Errorf("%w: hash %q", ErrUnsupportedVersion, c.Hash)
	}
	if _, err := c.encryption(); err != nil {
		return err
	}
	if _, err := hex.DecodeString(c.ID); err != nil || c.ID == "" {
		return fmt.Errorf("%w: bad repository id", ErrNotRepository)
	}
	return c.Chunker.validate()
}

// encryption returns the parsed "encryption" object, or nil for an
// unencrypted repository (field null or absent).
func (c *Config) encryption() (*Encryption, error) {
	if len(c.Encryption) == 0 || string(c.Encryption) == "null" {
		return nil, nil
	}
	return parseEncryption(c.Encryption)
}

// BlobRef locates one stored chunk inside a pack. Length is the stored size
// (compressed and, in an encrypted repository, sealed); RawLength is the
// plaintext size.
type BlobRef struct {
	ID        string `json:"id"`
	Offset    int64  `json:"offset"`
	Length    int64  `json:"length"`
	RawLength int64  `json:"rawLength"`
}

// packTrailer is the JSON document at the end of every pack.
type packTrailer struct {
	Version int       `json:"version"`
	Blobs   []BlobRef `json:"blobs"`
}

// indexFile is the content of one index/<id>.json file.
type indexFile struct {
	Version int         `json:"version"`
	Packs   []indexPack `json:"packs"`
}

// indexPack lists the blobs of one pack inside an index file.
type indexPack struct {
	ID    string    `json:"id"`
	Size  int64     `json:"size"`
	Blobs []BlobRef `json:"blobs"`
}

// Source describes what a snapshot was taken of. All fields are free-form;
// WSLPilot uses Kind "wsl-distro", "wslc-volume" or "compose-project".
type Source struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Host string `json:"host,omitempty"`
}

// key returns the grouping key retention policies use.
func (s Source) key() string { return s.Kind + "\x00" + s.Name + "\x00" + s.Host }

// Totals summarizes a snapshot.
type Totals struct {
	// Entries is the number of tar entries.
	Entries int64 `json:"entries"`
	// Bytes is the sum of entry content sizes.
	Bytes int64 `json:"bytes"`
	// NewChunks is the number of chunks this backup stored (not deduplicated).
	NewChunks int64 `json:"newChunks"`
	// NewBytes is the compressed size of the chunks this backup stored.
	NewBytes int64 `json:"newBytes"`
}

// Snapshot is the content of one snapshots/<id>.json file.
type Snapshot struct {
	Version int       `json:"version"`
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	Source  Source    `json:"source"`
	Tags    []string  `json:"tags,omitempty"`
	Tool    string    `json:"tool"`
	Parent  string    `json:"parent,omitempty"`
	// Tree lists the chunks whose concatenation is the JSON Lines entry list.
	Tree   []string `json:"tree"`
	Totals Totals   `json:"totals"`
}

// Entry is one tar entry of a snapshot: the header and its content chunks.
type Entry struct {
	Name     BString     `json:"name"`
	Type     string      `json:"type"`
	Mode     int64       `json:"mode"`
	UID      int         `json:"uid"`
	GID      int         `json:"gid"`
	Uname    BString     `json:"uname,omitempty"`
	Gname    BString     `json:"gname,omitempty"`
	ModTime  time.Time   `json:"mtime"`
	ATime    time.Time   `json:"atime,omitzero"`
	CTime    time.Time   `json:"ctime,omitzero"`
	Linkname BString     `json:"linkname,omitempty"`
	Devmajor int64       `json:"devmajor,omitempty"`
	Devminor int64       `json:"devminor,omitempty"`
	Size     int64       `json:"size"`
	Format   int         `json:"format,omitempty"`
	PAX      []PAXRecord `json:"pax,omitempty"`
	Chunks   []string    `json:"chunks,omitempty"`
}

// PAXRecord is one PAX extended header record (for example an xattr
// "SCHILY.xattr.user.test").
type PAXRecord struct {
	Key   BString `json:"key"`
	Value BString `json:"value"`
}

// BString is a byte string that survives JSON: valid UTF-8 is encoded as a
// JSON string, anything else as {"base64": "..."}. Linux file names, link
// targets and xattr values are byte strings, not text.
type BString string

// MarshalJSON encodes b as a string or a base64 object.
func (b BString) MarshalJSON() ([]byte, error) {
	if utf8.ValidString(string(b)) {
		return json.Marshal(string(b))
	}
	return json.Marshal(struct {
		Base64 string `json:"base64"`
	}{base64.StdEncoding.EncodeToString([]byte(b))})
}

// UnmarshalJSON decodes either encoding produced by MarshalJSON.
func (b *BString) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*b = BString(s)
		return nil
	}
	var o struct {
		Base64 *string `json:"base64"`
	}
	if err := json.Unmarshal(data, &o); err != nil || o.Base64 == nil {
		return fmt.Errorf("wpbk: byte string is neither a string nor {\"base64\":...}")
	}
	raw, err := base64.StdEncoding.DecodeString(*o.Base64)
	if err != nil {
		return fmt.Errorf("wpbk: byte string: %w", err)
	}
	*b = BString(raw)
	return nil
}

// hashID returns the lowercase hex BLAKE3-256 of data: the id of packs and
// index files, and of chunks in an unencrypted repository (see
// Repo.chunkID).
func hashID(data []byte) string {
	sum := blake3.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// validID reports whether s is a 64-character lowercase hex id.
func validID(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
