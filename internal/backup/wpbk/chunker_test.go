package wpbk

import (
	"encoding/binary"
	"slices"
	"testing"
)

// TestGearTable pins the gear table generation rule of backup-format.md.
func TestGearTable(t *testing.T) {
	var b []byte
	for _, g := range gear {
		b = binary.LittleEndian.AppendUint64(b, g)
	}
	if got := hashID(b); got != "03f5267f6940558e57783e6bfe4ded7b3d75182a2a23f7a64f9c4e83227fe7ee" {
		t.Fatalf("gear table digest %s", got)
	}
	if gear[0] != 0x72d1fab4f91743fc || gear[255] != 0x2dd732f8f06d09b8 {
		t.Fatalf("gear[0]=%#x gear[255]=%#x", gear[0], gear[255])
	}
}

// cutAll returns the cumulative chunk boundaries of data written in pieces
// of step bytes.
func cutAll(p ChunkerParams, data []byte, step int) []int {
	var cuts []int
	off := 0
	c := newChunker(p, func(b []byte) error {
		off += len(b)
		cuts = append(cuts, off)
		return nil
	})
	for i := 0; i < len(data); i += step {
		c.Write(data[i:min(i+step, len(data))])
	}
	c.Close()
	return cuts
}

// TestChunkerDeterministic pins boundaries for fixed data: changing them
// would silently break deduplication against existing repositories.
func TestChunkerDeterministic(t *testing.T) {
	data := randomBytes(42, 24<<20)
	want := []int{1148117, 2212823, 3333526, 3736739, 5156721, 5484428, 6589246, 7658376, 8995450, 9827546, 10930206, 12167913, 13971620, 15158439, 16399183, 17695460, 18666436, 19829162, 21327361, 22420887, 23592862, 24960101, 25165824}
	for _, step := range []int{len(data), 1 << 20, 32 << 10, 4099} {
		if got := cutAll(DefaultChunker(), data, step); !slices.Equal(got, want) {
			t.Fatalf("write size %d: boundaries %v", step, got)
		}
	}
}

// TestChunkerLimits checks the min/max bounds and that a local change only
// moves nearby boundaries.
func TestChunkerLimits(t *testing.T) {
	p := DefaultChunker()
	zeros := make([]byte, 10<<20) // no boundary ever matches: every chunk is Max
	cuts := cutAll(p, zeros, 1<<20)
	if !slices.Equal(cuts, []int{4 << 20, 8 << 20, 10 << 20}) {
		t.Fatalf("zero data cuts %v", cuts)
	}
	if got := cutAll(p, []byte("tiny"), 4); !slices.Equal(got, []int{4}) {
		t.Fatalf("tiny cuts %v", got)
	}
	if got := cutAll(p, nil, 1); got != nil {
		t.Fatalf("empty cuts %v", got)
	}
	data := randomBytes(42, 24<<20)
	orig := cutAll(p, data, len(data))
	prev := 0
	for _, c := range orig {
		if n := c - prev; (n < p.Min && c != len(data)) || n > p.Max {
			t.Fatalf("chunk of %d bytes", n)
		}
		prev = c
	}
	changed := slices.Clone(data)
	changed[12<<20] ^= 1
	same := 0
	for _, c := range cutAll(p, changed, len(changed)) {
		if slices.Contains(orig, c) {
			same++
		}
	}
	if same < len(orig)-2 {
		t.Fatalf("a one-byte change moved %d of %d boundaries", len(orig)-same, len(orig))
	}
}
