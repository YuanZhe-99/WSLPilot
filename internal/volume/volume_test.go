package volume

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// sampleTar builds a tar with owners, a symlink and a hard link.
func sampleTar(t *testing.T) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	now := time.Unix(1700000000, 0)
	add := func(h *tar.Header, body []byte) {
		h.ModTime = now
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		tw.Write(body)
	}
	add(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755}, nil)
	add(&tar.Header{Name: "./d/", Typeflag: tar.TypeDir, Mode: 0o700, Uid: 1234, Gid: 4321}, nil)
	body := bytes.Repeat([]byte("wslpilot\n"), 50000)
	add(&tar.Header{Name: "./d/f.bin", Typeflag: tar.TypeReg, Mode: 0o640, Uid: 1234, Gid: 4321, Size: int64(len(body))}, body)
	add(&tar.Header{Name: "./link", Typeflag: tar.TypeSymlink, Linkname: "d/f.bin", Mode: 0o777}, nil)
	add(&tar.Header{Name: "./hard", Typeflag: tar.TypeLink, Linkname: "./d/f.bin", Mode: 0o640, Uid: 1234, Gid: 4321}, nil)
	tw.Close()
	return buf.Bytes()
}

func TestDecompressDetectsFormats(t *testing.T) {
	plain := sampleTar(t)
	var gz, zs bytes.Buffer
	g := gzip.NewWriter(&gz)
	g.Write(plain)
	g.Close()
	z, _ := zstd.NewWriter(&zs)
	z.Write(plain)
	z.Close()
	for name, in := range map[string][]byte{"tar": plain, "gz": gz.Bytes(), "zst": zs.Bytes()} {
		r, err := Decompress(bytes.NewReader(in))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, _ := io.ReadAll(r)
		if !bytes.Equal(got, plain) {
			t.Errorf("%s: content differs", name)
		}
	}
	if _, err := Decompress(bytes.NewReader([]byte{0xfd, '7', 'z', 'X', 'Z', 0})); err == nil {
		t.Error("xz accepted")
	}
	st, _ := count(bytes.NewReader(plain))
	if st.Entries != 5 || st.Bytes != 450000 {
		t.Errorf("count = %+v", st)
	}
}

// TestLiveRoundTrip imports a tar into a new volume, exports it back and
// compares headers. Runs only with WSLPILOT_LIVE=1 and only touches
// wslpilot-test-* objects.
func TestLiveRoundTrip(t *testing.T) {
	if os.Getenv("WSLPILOT_LIVE") != "1" {
		t.Skip("set WSLPILOT_LIVE=1")
	}
	ctx := context.Background()
	c := wslc.New()
	p := &Pipeline{E: c}
	const vol = "wslpilot-test-volume-rt"
	defer c.RemoveVolumes(context.Background(), vol)
	in := sampleTar(t)
	st, found, err := p.Import(ctx, vol, bytes.NewReader(in), ImportOptions{Create: true, Labels: map[string]string{"io.wslpilot.test": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if st.Entries != 5 || found != 4 { // "./" itself is not counted inside
		t.Errorf("import stats %+v, found %d", st, found)
	}
	var out bytes.Buffer
	if _, err := p.Export(ctx, vol, &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]*tar.Header{}
	tr := tar.NewReader(&out)
	var fileSum [32]byte
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		got[h.Name] = h
		if h.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(tr)
			fileSum = sha256.Sum256(b)
		}
	}
	f := got["./d/f.bin"]
	// tar stores whichever of a hard-linked pair it meets first as the file.
	if f != nil && f.Typeflag == tar.TypeLink {
		f, got["./d/f.bin"], got["./hard"] = got["./hard"], got["./hard"], f
	}
	if f == nil || f.Uid != 1234 || f.Gid != 4321 || f.Mode&0o777 != 0o640 {
		t.Errorf("file header %+v", f)
	}
	if fileSum != sha256.Sum256(bytes.Repeat([]byte("wslpilot\n"), 50000)) {
		t.Error("file content differs")
	}
	if l := got["./link"]; l == nil || l.Typeflag != tar.TypeSymlink || l.Linkname != "d/f.bin" {
		t.Errorf("symlink %+v", l)
	}
	if h := got["./hard"]; h == nil || (h.Typeflag != tar.TypeLink && got["./d/f.bin"].Typeflag != tar.TypeLink) {
		t.Errorf("hard link %+v", h)
	}
	if left, _ := c.Containers(ctx, "label="+HelperLabel+"=1"); len(left) != 0 {
		t.Errorf("helpers left behind: %+v", left)
	}
}
