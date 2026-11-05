// Package volume moves WSLc volume contents as tar streams. Data enters and
// leaves a volume through a short-lived helper container that runs tar
// inside Linux, so owners, modes, symlinks and hard links are kept and no
// Linux tree is ever unpacked into a Windows folder.
package volume

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	"github.com/klauspost/compress/zstd"

	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// DefaultHelperImage is the image helper containers run unless the user
// chose another. It needs a tar that understands -p and --numeric-owner
// (busybox tar 1.36.1 in alpine 3.20, verified on WSLc 3.0.3.0); it is
// pulled on first use when missing.
const DefaultHelperImage = "alpine:3.20"

// HelperLabel marks helper containers so leftovers can be found and removed.
const HelperLabel = "io.wslpilot.helper"

// Engine is the part of the wslc adapter the pipeline uses.
type Engine interface {
	Create(ctx context.Context, s wslc.ContainerSpec) (string, error)
	Start(ctx context.Context, id string) error
	Remove(ctx context.Context, force, anonVolumes bool, ids ...string) error
	ExecStream(ctx context.Context, id, user string, stdin io.Reader, stdout io.Writer, argv ...string) error
	Exec(ctx context.Context, id, user string, argv ...string) (string, error)
	InspectImage(ctx context.Context, ref string) (wslc.ImageDetail, error)
	Pull(ctx context.Context, ref string, log io.Writer) error
	Containers(ctx context.Context, filters ...string) ([]wslc.ContainerSummary, error)
	CreateVolume(ctx context.Context, name, driver string, opts, labels map[string]string) error
}

// Pipeline runs volume transfers through an Engine.
type Pipeline struct {
	E Engine
	// Log receives progress text (pull output); may be nil.
	Log   io.Writer
	image atomic.Value // string; set by SetImage
}

// SetImage chooses the helper image ("" restores DefaultHelperImage). It
// may be called while transfers run; each transfer uses the image it saw
// when it started.
func (p *Pipeline) SetImage(ref string) { p.image.Store(strings.TrimSpace(ref)) }

// Image returns the helper image in use.
func (p *Pipeline) Image() string {
	if s, _ := p.image.Load().(string); s != "" {
		return s
	}
	return DefaultHelperImage
}

// helper starts a helper container with volume mounted at target.
// Returns: its ID and a cleanup that force-removes it.
func (p *Pipeline) helper(ctx context.Context, volume, target string, readOnly bool) (string, func(), error) {
	img := p.Image()
	if _, err := p.E.InspectImage(ctx, img); err != nil {
		if !errors.Is(err, wslc.ErrNotFound) {
			return "", nil, err
		}
		if err := p.E.Pull(ctx, img, p.log()); err != nil {
			return "", nil, fmt.Errorf("pull helper image %s: %w", img, err)
		}
	}
	var rnd [4]byte
	rand.Read(rnd[:])
	spec := wslc.ContainerSpec{
		Name:    "wslpilot-helper-" + hex.EncodeToString(rnd[:]),
		Image:   img,
		Command: []string{"sleep", "86400"},
		Labels:  map[string]string{HelperLabel: "1"},
		Mounts:  []wslc.MountSpec{{Type: "volume", Source: volume, Target: target, ReadOnly: readOnly}},
	}
	id, err := p.E.Create(ctx, spec)
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { p.E.Remove(context.Background(), true, false, id) }
	if err := p.E.Start(ctx, id); err != nil {
		cleanup()
		return "", nil, err
	}
	return id, cleanup, nil
}

func (p *Pipeline) log() io.Writer {
	if p.Log == nil {
		return io.Discard
	}
	return p.Log
}

// Stats summarizes a tar stream that passed through the pipeline.
type Stats struct {
	Entries int
	Bytes   int64 // sum of regular file sizes
}

// ImportOptions control Import.
type ImportOptions struct {
	// Create makes the volume first (Driver "" = guest; vhd needs Size).
	Create bool
	Driver string
	Size   int64
	Labels map[string]string
	// Replace empties an existing volume first. The caller has confirmed
	// and backed it up (behavior contract).
	Replace bool
}

// Import unpacks a tar stream into a volume.
// Inputs: src is a .tar, .tar.gz or .tar.zst stream; Decompress detects
// which.
// Returns: the entries and bytes read from the stream, and the number of
// entries found in the volume afterwards (a check that nothing was
// dropped).
// Side effects: may create the volume; starts and removes a helper.
func (p *Pipeline) Import(ctx context.Context, volume string, src io.Reader, o ImportOptions) (Stats, int, error) {
	if o.Create {
		opts := map[string]string{}
		if o.Driver == "vhd" {
			if o.Size <= 0 {
				return Stats{}, 0, errors.New("volume: a vhd volume needs a size")
			}
			opts["SizeBytes"] = fmt.Sprint(o.Size)
		}
		if err := p.E.CreateVolume(ctx, volume, o.Driver, opts, o.Labels); err != nil {
			return Stats{}, 0, err
		}
	}
	plain, err := Decompress(src)
	if err != nil {
		return Stats{}, 0, err
	}
	id, cleanup, err := p.helper(ctx, volume, "/dst", false)
	if err != nil {
		return Stats{}, 0, err
	}
	defer cleanup()
	if o.Replace {
		if _, err := p.E.Exec(ctx, id, "root", "find", "/dst", "-mindepth", "1", "-delete"); err != nil {
			return Stats{}, 0, fmt.Errorf("empty volume: %w", err)
		}
	}
	// The stream is read once: tar in the helper consumes it while a Go
	// tar reader counts what passes.
	pr, pw := io.Pipe()
	counted := make(chan Stats, 1)
	go func() {
		st, _ := count(pr)
		io.Copy(io.Discard, pr)
		counted <- st
	}()
	tee := io.TeeReader(plain, pw)
	err = p.E.ExecStream(ctx, id, "root", tee, nil, "tar", "-xpf", "-", "--numeric-owner", "-C", "/dst")
	pw.CloseWithError(err)
	st := <-counted
	if err != nil {
		return st, 0, err
	}
	found, err := p.countVolume(ctx, id, "/dst")
	return st, found, err
}

// countVolume returns the number of entries below dir in the helper.
func (p *Pipeline) countVolume(ctx context.Context, id, dir string) (int, error) {
	out, err := p.E.Exec(ctx, id, "root", "sh", "-c", `find "$1" -mindepth 1 | wc -l`, "sh", dir)
	if err != nil {
		return 0, err
	}
	var n int
	_, err = fmt.Sscan(strings.TrimSpace(out), &n)
	return n, err
}

// Export writes the volume's contents to w as a plain tar stream.
// Notes: the volume is mounted read-only in the helper; containers using
// it should be stopped first for a consistent copy (the caller decides).
// busybox tar writes no xattrs: they are not exported (doc/en-us/volumes.md).
func (p *Pipeline) Export(ctx context.Context, volume string, w io.Writer) (Stats, error) {
	id, cleanup, err := p.helper(ctx, volume, "/src", true)
	if err != nil {
		return Stats{}, err
	}
	defer cleanup()
	pr, pw := io.Pipe()
	counted := make(chan Stats, 1)
	go func() {
		st, _ := count(pr)
		io.Copy(io.Discard, pr)
		counted <- st
	}()
	err = p.E.ExecStream(ctx, id, "root", nil, io.MultiWriter(w, pw), "tar", "-cpf", "-", "--numeric-owner", "-C", "/src", ".")
	pw.CloseWithError(err)
	st := <-counted
	return st, err
}

// UsingContainers lists containers (any state) that mount the volume.
func (p *Pipeline) UsingContainers(ctx context.Context, volume string) ([]wslc.ContainerSummary, error) {
	return p.E.Containers(ctx, "volume="+volume)
}

// CleanupHelpers removes helper containers left by an interrupted run.
func (p *Pipeline) CleanupHelpers(ctx context.Context) error {
	cs, err := p.E.Containers(ctx, "label="+HelperLabel+"=1")
	if err != nil {
		return err
	}
	for _, c := range cs {
		p.E.Remove(ctx, true, false, c.ID)
	}
	return nil
}

// count reads a tar stream and totals its entries.
func count(r io.Reader) (Stats, error) {
	var st Stats
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return st, nil
		}
		if err != nil {
			return st, err
		}
		st.Entries++
		if h.Typeflag == tar.TypeReg {
			st.Bytes += h.Size
		}
	}
}

// Decompress returns a plain tar stream for a .tar, gzip or zstd stream,
// detected by magic bytes.
func Decompress(r io.Reader) (io.Reader, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	magic, err := br.Peek(4)
	if err != nil && len(magic) == 0 {
		return nil, fmt.Errorf("volume: empty archive: %w", err)
	}
	switch {
	case bytes.HasPrefix(magic, []byte{0x1f, 0x8b}):
		return gzip.NewReader(br)
	case bytes.HasPrefix(magic, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		d, err := zstd.NewReader(br)
		if err != nil {
			return nil, err
		}
		return d.IOReadCloser(), nil
	case bytes.HasPrefix(magic, []byte{0xfd, '7', 'z', 'X'}):
		return nil, errors.New("volume: .tar.xz is not supported; use .tar, .tar.gz or .tar.zst")
	}
	return br, nil
}
