package wpbk

import (
	"encoding/binary"
	"fmt"
	"math/bits"

	"github.com/zeebo/blake3"
)

// ChunkerParams are the content-defined chunking parameters recorded in
// config.json. Writers must use them so that the same content always cuts
// into the same chunks, which is what makes deduplication work.
type ChunkerParams struct {
	Algorithm string `json:"algorithm"`
	Min       int    `json:"min"`
	Avg       int    `json:"avg"`
	Max       int    `json:"max"`
}

// DefaultChunker returns the v1 defaults: 256 KiB / 1 MiB / 4 MiB.
func DefaultChunker() ChunkerParams {
	return ChunkerParams{Algorithm: ChunkerFastCDC, Min: 256 << 10, Avg: 1 << 20, Max: 4 << 20}
}

// validate checks the parameter constraints of backup-format.md.
// Notes: Avg must be a power of two between 2^8 and 2^30, and
// 64 <= Min < Avg < Max.
func (p ChunkerParams) validate() error {
	if p.Algorithm != ChunkerFastCDC {
		return fmt.Errorf("%w: chunker %q", ErrUnsupportedVersion, p.Algorithm)
	}
	if p.Avg < 1<<8 || p.Avg > 1<<30 || p.Avg&(p.Avg-1) != 0 || p.Min < 64 || p.Min >= p.Avg || p.Max <= p.Avg {
		return fmt.Errorf("%w: invalid chunker parameters %d/%d/%d", ErrNotRepository, p.Min, p.Avg, p.Max)
	}
	return nil
}

// gearSeed is the domain-separation prefix of the gear table rule.
const gearSeed = "wpbk gear v1"

// gear is the 256-entry FastCDC gear table: gear[i] is the little-endian
// uint64 of the first 8 bytes of BLAKE3-256("wpbk gear v1" || byte(i)).
// chunker_test.go pins a digest of the whole table.
var gear = func() (t [256]uint64) {
	for i := range t {
		sum := blake3.Sum256(append([]byte(gearSeed), byte(i)))
		t[i] = binary.LittleEndian.Uint64(sum[:8])
	}
	return t
}()

// cut returns the length of the first chunk of data.
// Inputs: data holds at least p.Max bytes, or the rest of the stream.
// Returns: a length in [1, len(data)], or 0 for empty data.
// Notes: normalized FastCDC (level 2): before Avg a boundary needs the top
// log2(Avg)+2 bits of the gear hash to be zero, after Avg only the top
// log2(Avg)-2 bits. Hashing starts at offset Min with a zero hash.
func (p ChunkerParams) cut(data []byte) int {
	n := len(data)
	if n <= p.Min {
		return n
	}
	if n > p.Max {
		n = p.Max
	}
	normal := p.Avg
	if n < normal {
		normal = n
	}
	b := bits.TrailingZeros(uint(p.Avg))
	maskS := ^uint64(0) << (64 - (b + 2))
	maskL := ^uint64(0) << (64 - (b - 2))
	var fp uint64
	i := p.Min
	for ; i < normal; i++ {
		fp = fp<<1 + gear[data[i]]
		if fp&maskS == 0 {
			return i + 1
		}
	}
	for ; i < n; i++ {
		fp = fp<<1 + gear[data[i]]
		if fp&maskL == 0 {
			return i + 1
		}
	}
	return n
}

// chunker is a push-style splitter: content is written into it and every
// complete chunk is passed to emit. Boundaries depend only on the bytes, not
// on how writes are sized.
type chunker struct {
	p    ChunkerParams
	buf  []byte
	emit func([]byte) error
}

// newChunker returns a chunker that calls emit for each chunk.
// Notes: emit must not keep the slice it receives.
func newChunker(p ChunkerParams, emit func([]byte) error) *chunker {
	return &chunker{p: p, emit: emit, buf: make([]byte, 0, 2*p.Max)}
}

// Write buffers b and emits every chunk that can already be cut.
func (c *chunker) Write(b []byte) (int, error) {
	c.buf = append(c.buf, b...)
	start := 0
	for len(c.buf)-start >= c.p.Max {
		n := c.p.cut(c.buf[start:])
		if err := c.emit(c.buf[start : start+n]); err != nil {
			return 0, err
		}
		start += n
	}
	if start > 0 {
		c.buf = append(c.buf[:0], c.buf[start:]...)
	}
	return len(b), nil
}

// Close emits the remaining buffered bytes as the final chunks.
func (c *chunker) Close() error {
	start := 0
	for start < len(c.buf) {
		n := c.p.cut(c.buf[start:])
		if err := c.emit(c.buf[start : start+n]); err != nil {
			return err
		}
		start += n
	}
	c.buf = c.buf[:0]
	return nil
}
