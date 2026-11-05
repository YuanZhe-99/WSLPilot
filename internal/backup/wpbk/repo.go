package wpbk

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/version"
	"github.com/klauspost/compress/zstd"
)

// Repo is an open wpbk repository. Its methods may be called from one
// goroutine at a time; separate processes coordinate through locks/.
type Repo struct {
	dir      string
	cfg      Config
	readOnly bool
	enc      *zstd.Encoder
	dec      *zstd.Decoder
	// now returns the current time; tests replace it.
	now func() time.Time
}

// InitOptions configure a new repository.
type InitOptions struct {
	// Chunker overrides the default chunking parameters (zero: defaults).
	Chunker ChunkerParams
	// PackSize overrides the pack target size in bytes (zero: 32 MiB).
	PackSize int64
}

// OpenOptions configure how an existing repository is opened.
type OpenOptions struct {
	// ReadOnly forbids every write, including lock files. Use it for
	// repositories that belong to another PC or sit on shared storage.
	ReadOnly bool
}

// repoDirs are the subdirectories of every repository.
var repoDirs = []string{"packs", "index", "snapshots", "locks"}

// Init creates a new, empty repository in dir.
// Inputs: dir must not exist or be an empty directory.
// Returns: the repository, open for writing.
// Side effects: creates dir, its subdirectories and config.json.
func Init(dir string, opts InitOptions) (*Repo, error) {
	if ents, err := os.ReadDir(dir); err == nil && len(ents) > 0 {
		return nil, fmt.Errorf("wpbk: %s is not empty", dir)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	cfg := Config{
		Format:      FormatName,
		Version:     FormatVersion,
		ID:          randomID(),
		Created:     time.Now().UTC(),
		Chunker:     opts.Chunker,
		Compression: CompressionZstd,
		Hash:        HashBlake3,
		Encryption:  json.RawMessage("null"),
		PackSize:    opts.PackSize,
	}
	if cfg.Chunker == (ChunkerParams{}) {
		cfg.Chunker = DefaultChunker()
	}
	if cfg.PackSize <= 0 {
		cfg.PackSize = DefaultPackSize
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	for _, d := range repoDirs {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return nil, err
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(dir, "config.json"), data); err != nil {
		return nil, err
	}
	return newRepo(dir, cfg, false)
}

// Open opens an existing repository.
// Returns: ErrNotRepository or ErrUnsupportedVersion (wrapped) when dir is not
// a repository this build can read.
// Notes: with ReadOnly nothing in dir is ever created, changed or removed.
func Open(dir string, opts OpenOptions) (*Repo, error) {
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s has no config.json", ErrNotRepository, dir)
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%w: config.json: %v", ErrNotRepository, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.PackSize <= 0 {
		cfg.PackSize = DefaultPackSize
	}
	for _, d := range repoDirs {
		if d == "locks" && opts.ReadOnly {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, d)); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("%w: missing %s/", ErrNotRepository, d)
		}
	}
	return newRepo(dir, cfg, opts.ReadOnly)
}

// newRepo builds the Repo value and its codecs.
func newRepo(dir string, cfg Config, readOnly bool) (*Repo, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(1<<30), zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	return &Repo{dir: dir, cfg: cfg, readOnly: readOnly, enc: enc, dec: dec, now: time.Now}, nil
}

// Close releases the codecs. The Repo must not be used afterwards.
func (r *Repo) Close() error {
	r.enc.Close()
	r.dec.Close()
	return nil
}

// Config returns the repository configuration.
func (r *Repo) Config() Config { return r.cfg }

// Dir returns the repository directory.
func (r *Repo) Dir() string { return r.dir }

// ReadOnly reports whether the repository was opened read-only.
func (r *Repo) ReadOnly() bool { return r.readOnly }

// toolString identifies the writer in snapshots.
func toolString() string { return "WSLPilot " + version.Version }

// randomID returns 32 random bytes as lowercase hex.
func randomID() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// writeFileAtomic writes data to path through a synced temporary file in
// the same directory and a rename, so readers never see partial content.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "tmp-*.partial")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	syncDir(dir)
	return nil
}

// syncDir flushes a directory entry where the platform supports it.
// Notes: Windows cannot open directories for sync; errors are ignored.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}
