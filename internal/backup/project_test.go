package backup_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
)

// volumeTar is what a volume export looks like: "./" root, owners, a hard
// link (whose target is an archive path).
func volumeTar() []byte {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	mt := time.Unix(1700000000, 0)
	tw.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o750, Uid: 999, Gid: 999, ModTime: mt})
	tw.WriteHeader(&tar.Header{Name: "./db/", Typeflag: tar.TypeDir, Mode: 0o700, Uid: 999, Gid: 999, ModTime: mt})
	tw.WriteHeader(&tar.Header{Name: "./db/data", Typeflag: tar.TypeReg, Mode: 0o600, Uid: 999, Gid: 998, Size: 4, ModTime: mt})
	tw.Write([]byte("rows"))
	tw.WriteHeader(&tar.Header{Name: "./db/link", Typeflag: tar.TypeLink, Linkname: "./db/data", ModTime: mt})
	tw.WriteHeader(&tar.Header{Name: "./db/sym", Typeflag: tar.TypeSymlink, Linkname: "data", ModTime: mt})
	tw.Close()
	return b.Bytes()
}

type tarEntry struct {
	Name, Link string
	Type       byte
	Mode       int64
	UID, GID   int
	Body       string
}

func entries(t *testing.T, r io.Reader) []tarEntry {
	t.Helper()
	var out []tarEntry
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out = append(out, tarEntry{h.Name, h.Linkname, h.Typeflag, h.Mode, h.Uid, h.Gid, string(b)})
	}
}

// projectFixture lays out a project folder with a bind folder inside it,
// one outside it, and a bound single file.
func projectFixture(t *testing.T) (backup.ProjectSpec, string) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	os.MkdirAll(filepath.Join(proj, "vc-data", "sub"), 0o755)
	os.WriteFile(filepath.Join(proj, "compose.yaml"), []byte("services: {}\n"), 0o644)
	os.WriteFile(filepath.Join(proj, ".env"), []byte("A=1\n"), 0o644)
	os.WriteFile(filepath.Join(proj, "vc-data", "sub", "x.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(proj, "nginx.conf"), []byte("conf"), 0o644)
	other := filepath.Join(root, "elsewhere")
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "y.txt"), []byte("far"), 0o644)
	vol := volumeTar()
	return backup.ProjectSpec{
		Name: "demo", WorkingDir: proj,
		Files: map[string]string{"compose.yaml": filepath.Join(proj, "compose.yaml"), ".env": filepath.Join(proj, ".env")},
		Folders: []backup.ProjectFolder{
			{ID: "vc-data", Source: filepath.Join(proj, "vc-data"), Relative: "vc-data"},
			{ID: "elsewhere", Source: other},
			{ID: "nginx.conf", Source: filepath.Join(proj, "nginx.conf"), Relative: "nginx.conf", File: true},
		},
		Volumes: []backup.ProjectVolume{{Key: "db", Name: "demo_db"}},
		ExportVolume: func(_ context.Context, name string, w io.Writer) error {
			_, err := w.Write(vol)
			return err
		},
	}, root
}

// TestProjectBackupEveryKind stores a project in a folder, an archive and
// a repository, then reads the manifest, extracts files and folders, and
// gets the volume back exactly as it was exported.
func TestProjectBackupEveryKind(t *testing.T) {
	ctx := context.Background()
	writers := map[string]func(dir string) backup.Writer{
		"folder":  func(d string) backup.Writer { return &backup.FolderWriter{Dir: d} },
		"archive": func(d string) backup.Writer { return &backup.ArchiveWriter{Dir: d, Compression: backup.CompressZstd} },
		"repo":    func(d string) backup.Writer { return &backup.RepoWriter{Dir: d} },
	}
	for kind, mk := range writers {
		t.Run(kind, func(t *testing.T) {
			spec, root := projectFixture(t)
			stopped, started := false, false
			spec.Running = true
			spec.Stop = func(context.Context) error { stopped = true; return nil }
			spec.Start = func(context.Context) { started = true }
			r := &backup.Runner{}
			it, err := r.BackupProject(ctx, spec, mk(filepath.Join(root, "bk")), backup.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !stopped || !started || !it.Consistent || it.Source.Kind != backup.SourceCompose {
				t.Fatalf("stopped %v started %v item %+v", stopped, started, it)
			}
			if err := backup.Verify(ctx, it); err != nil {
				t.Fatal(err)
			}
			man, err := backup.ReadProjectManifest(ctx, it)
			if err != nil || man == nil {
				t.Fatalf("manifest %v %v", man, err)
			}
			if man.Version != 2 || len(man.Folders) != 3 || len(man.Volumes) != 1 || len(man.Files) != 2 {
				t.Fatalf("manifest %+v", man)
			}

			out := filepath.Join(root, "restored")
			placed, err := backup.ExtractProject(ctx, it, out)
			if err != nil {
				t.Fatal(err)
			}
			for p, want := range map[string]string{
				"compose.yaml":                    "services: {}\n",
				".env":                            "A=1\n",
				"vc-data/sub/x.txt":               "hello",
				"nginx.conf":                      "conf",
				"outside-folders/elsewhere/y.txt": "far",
			} {
				b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(p)))
				if err != nil || string(b) != want {
					t.Errorf("%s: %q %v", p, b, err)
				}
			}
			if len(placed) != 3 {
				t.Errorf("placed %v", placed)
			}
			if _, err := backup.ExtractProject(ctx, it, out); err == nil {
				t.Error("extracting over existing files succeeded")
			}

			rc, err := backup.ProjectVolumeStream(ctx, it, "demo_db")
			if err != nil {
				t.Fatal(err)
			}
			got := entries(t, rc)
			rc.Close()
			want := entries(t, bytes.NewReader(volumeTar()))
			if len(got) != len(want) {
				t.Fatalf("volume entries %+v", got)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("entry %d: %+v, want %+v", i, got[i], want[i])
				}
			}
			if rc, err := backup.ProjectVolumeStream(ctx, it, "nope"); err != nil {
				t.Fatal(err)
			} else {
				_, err := io.ReadAll(rc)
				rc.Close()
				if err == nil {
					t.Error("a missing volume read as empty")
				}
			}
		})
	}
}

// TestProjectBackupLive leaves a running project running and labels the
// copy.
func TestProjectBackupLive(t *testing.T) {
	spec, root := projectFixture(t)
	spec.Running = true
	spec.Stop = func(context.Context) error { t.Fatal("stopped a live backup"); return nil }
	it, err := (&backup.Runner{}).BackupProject(context.Background(), spec, &backup.ArchiveWriter{Dir: root}, backup.Options{Live: true})
	if err != nil || it.Consistent {
		t.Fatalf("%+v %v", it, err)
	}
}

// TestProjectSpecReader reads a version 2 project archive using only the
// documented layout: the manifest first, files at the root, folders/<id>/
// and volumes/<name>/ with the volume's own headers.
func TestProjectSpecReader(t *testing.T) {
	spec, root := projectFixture(t)
	it, err := (&backup.Runner{}).BackupProject(context.Background(), spec, &backup.ArchiveWriter{Dir: root, Compression: backup.CompressNone}, backup.Options{})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(it.Path)
	defer f.Close()
	es := entries(t, f)
	var man struct {
		Format  string
		Version int
		Folders []struct{ ID, Source, Relative string }
		Volumes []struct{ Key, Name string }
	}
	if es[0].Name != "wslpilot-project.json" || json.Unmarshal([]byte(es[0].Body), &man) != nil {
		t.Fatalf("first entry %+v", es[0])
	}
	if man.Format != "wslpilot-compose" || man.Version != 2 || man.Volumes[0].Name != "demo_db" {
		t.Fatalf("manifest %+v", man)
	}
	names := map[string]tarEntry{}
	for _, e := range es {
		names[e.Name] = e
	}
	for _, n := range []string{"compose.yaml", ".env", "folders/vc-data/sub/x.txt", "folders/elsewhere/y.txt", "folders/nginx.conf/nginx.conf", "volumes/demo_db/"} {
		if _, ok := names[n]; !ok {
			t.Errorf("missing %s", n)
		}
	}
	if e := names["volumes/demo_db/db/data"]; e.UID != 999 || e.GID != 998 || e.Mode != 0o600 || e.Body != "rows" {
		t.Errorf("volume file %+v", e)
	}
	if e := names["volumes/demo_db/db/link"]; e.Link != "volumes/demo_db/db/data" {
		t.Errorf("hard link %+v", e)
	}
	if e := names["volumes/demo_db/db/sym"]; e.Link != "data" {
		t.Errorf("symlink %+v", e)
	}
	if e := names["volumes/demo_db/"]; e.Mode != 0o750 || e.UID != 999 {
		t.Errorf("volume root %+v", e)
	}
}
