package backup

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Compose project backups, layout version 2 (doc/en-us/backups.md,
// "Compose project backups"). One tar holds the project's files at its
// root, each bind folder under folders/<id>/ and each named volume under
// volumes/<name>/, after a manifest that is the first entry.
const (
	ProjectManifestName = "wslpilot-project.json"
	ProjectFormat       = "wslpilot-compose"
	ProjectVersion      = 2
	// OutsideFolder is where a restore places bind folders that were not
	// inside the project's folder.
	OutsideFolder = "outside-folders"
)

// ProjectFolder is one bind folder (or bound single file) of a project.
type ProjectFolder struct {
	// ID names the folder inside the backup: folders/<ID>/.
	ID string `json:"id"`
	// Source is the Windows path that was backed up.
	Source string `json:"source"`
	// Relative is Source relative to the project's folder, slash
	// separated; "" when it is outside it.
	Relative string `json:"relative,omitempty"`
	// File is true when the bind source is one file, stored as
	// folders/<ID>/<base name>.
	File     bool     `json:"file,omitempty"`
	Services []string `json:"services,omitempty"`
}

// ProjectVolume is one named volume of a project.
type ProjectVolume struct {
	Key      string   `json:"key"`  // the Compose key
	Name     string   `json:"name"` // the WSLc volume, stored as volumes/<Name>/
	Services []string `json:"services,omitempty"`
}

// ProjectManifest is the first entry of a version 2 project backup.
type ProjectManifest struct {
	Format     string          `json:"format"`
	Version    int             `json:"version"`
	Project    string          `json:"project"`
	WorkingDir string          `json:"workingDir,omitempty"`
	Files      []string        `json:"files"`
	Folders    []ProjectFolder `json:"folders,omitempty"`
	Volumes    []ProjectVolume `json:"volumes,omitempty"`
	// External names volumes the project uses but did not create; they
	// are not in this backup.
	External []string `json:"external,omitempty"`
}

// ProjectSpec is what to put in a project backup.
type ProjectSpec struct {
	Name       string
	WorkingDir string
	// Files maps a name at the tar's root to a Windows file.
	Files    map[string]string
	Folders  []ProjectFolder
	Volumes  []ProjectVolume
	External []string
	// Running reports containers to stop for a consistent copy; Stop and
	// Start stop and start them again.
	Running bool
	Stop    func(ctx context.Context) error
	Start   func(ctx context.Context)
	// ExportVolume writes a volume's tar; nil uses the Runner's pipeline.
	ExportVolume func(ctx context.Context, name string, w io.Writer) error
}

// BackupProject stores a Compose project as one item: files, bind folders
// and named volumes.
// Side effects: stops the project's running containers (unless Live) and
// starts them again afterwards.
func (r *Runner) BackupProject(ctx context.Context, s ProjectSpec, w Writer, o Options) (Item, error) {
	consistent := true
	if s.Running {
		if o.Live || s.Stop == nil {
			consistent = false
		} else {
			r.logf("Stopping %s for a consistent copy", s.Name)
			if err := s.Stop(ctx); err != nil {
				return Item{}, err
			}
			if s.Start != nil {
				defer func() {
					r.logf("Starting %s again", s.Name)
					s.Start(context.Background())
				}()
			}
		}
	}
	export := s.ExportVolume
	if export == nil {
		export = func(ctx context.Context, name string, w io.Writer) error {
			_, err := r.Vol.Export(ctx, name, w)
			return err
		}
	}
	m := Meta{Source: Source{Kind: SourceCompose, Name: s.Name, Host: r.Host}, Time: time.Now(), Consistent: consistent, Plan: o.Plan, Tool: tool()}
	return r.pipe(ctx, w, m, func(pw io.Writer) error { return r.writeProjectTar(ctx, pw, s, export) })
}

// writeProjectTar writes the version 2 layout.
func (r *Runner) writeProjectTar(ctx context.Context, w io.Writer, s ProjectSpec, export func(context.Context, string, io.Writer) error) error {
	man := ProjectManifest{Format: ProjectFormat, Version: ProjectVersion, Project: s.Name, WorkingDir: s.WorkingDir,
		Folders: s.Folders, Volumes: s.Volumes, External: s.External}
	for name := range s.Files {
		man.Files = append(man.Files, name)
	}
	sort.Strings(man.Files)
	tw := tar.NewWriter(w)
	data, _ := json.MarshalIndent(man, "", "  ")
	now := time.Now()
	if err := tw.WriteHeader(&tar.Header{Name: ProjectManifestName, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(data)), ModTime: now}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	for _, name := range man.Files {
		if err := addWindowsFile(tw, name, s.Files[name]); err != nil {
			return err
		}
	}
	for _, f := range s.Folders {
		r.logf("Copying %s", f.Source)
		prefix := "folders/" + f.ID + "/"
		if err := tw.WriteHeader(&tar.Header{Name: prefix, Typeflag: tar.TypeDir, Mode: 0o755, ModTime: now}); err != nil {
			return err
		}
		if f.File {
			if err := addWindowsFile(tw, prefix+filepath.Base(f.Source), f.Source); err != nil {
				return err
			}
			continue
		}
		if err := r.addWindowsTree(ctx, tw, prefix, f.Source); err != nil {
			return err
		}
	}
	for _, v := range s.Volumes {
		r.logf("Copying volume %s", v.Name)
		if err := addVolume(ctx, tw, "volumes/"+v.Name+"/", func(pw io.Writer) error { return export(ctx, v.Name, pw) }); err != nil {
			return fmt.Errorf("volume %s: %w", v.Name, err)
		}
	}
	return tw.Close()
}

// addWindowsFile writes one Windows file (mode 0644).
func addWindowsFile(tw *tar.Writer, name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: st.Size(), ModTime: st.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// addWindowsTree writes a Windows folder's directories and files under
// prefix. Links and other reparse points are not followed; they are
// logged and left out.
func (r *Runner) addWindowsTree(ctx context.Context, tw *tar.Writer, prefix, root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		name := prefix + filepath.ToSlash(rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0 || d.Type()&fs.ModeIrregular != 0:
			r.logf("Left out %s: links are not copied", p)
			return nil
		case d.IsDir():
			st, err := d.Info()
			if err != nil {
				return err
			}
			return tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: st.ModTime()})
		case d.Type().IsRegular():
			return addWindowsFile(tw, name, p)
		}
		r.logf("Left out %s: not a file or folder", p)
		return nil
	})
}

// addVolume copies a volume's tar under prefix, keeping every header
// field. Hard link targets are archive paths and get the prefix too.
func addVolume(ctx context.Context, tw *tar.Writer, prefix string, produce func(io.Writer) error) error {
	pr, pw := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		err := produce(pw)
		pw.CloseWithError(err)
		errc <- err
	}()
	tr := tar.NewReader(pr)
	var cerr error
	root := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			cerr = err
			break
		}
		name := cleanTarName(h.Name)
		if name == "" {
			h.Name, root = prefix, true
		} else {
			h.Name = prefix + name
			if h.Typeflag == tar.TypeDir && !strings.HasSuffix(h.Name, "/") {
				h.Name += "/"
			}
		}
		if h.Typeflag == tar.TypeLink {
			h.Linkname = prefix + cleanTarName(h.Linkname)
		}
		if cerr = tw.WriteHeader(h); cerr != nil {
			break
		}
		if _, cerr = io.Copy(tw, tr); cerr != nil {
			break
		}
	}
	if cerr == nil {
		io.Copy(io.Discard, pr)
	}
	pr.CloseWithError(errors.New("backup: volume copy stopped"))
	perr := <-errc
	if perr != nil {
		return perr
	}
	if cerr == nil && !root {
		cerr = tw.WriteHeader(&tar.Header{Name: prefix, Typeflag: tar.TypeDir, Mode: 0o755, ModTime: time.Now()})
	}
	return cerr
}

// cleanTarName drops "./" and "/" at the start; "" is the root.
func cleanTarName(n string) string {
	for {
		switch {
		case strings.HasPrefix(n, "./"):
			n = n[2:]
		case strings.HasPrefix(n, "/"):
			n = n[1:]
		case n == ".":
			return ""
		default:
			return n
		}
	}
}

// ReadProjectManifest returns a project backup's manifest, or nil for a
// version 1 backup (files only).
func ReadProjectManifest(ctx context.Context, it Item) (*ProjectManifest, error) {
	rc, err := Open(ctx, it)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	h, err := tr.Next()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if cleanTarName(h.Name) != ProjectManifestName {
		return nil, nil
	}
	var m ProjectManifest
	if err := json.NewDecoder(io.LimitReader(tr, 1<<20)).Decode(&m); err != nil {
		return nil, err
	}
	if m.Format != ProjectFormat || m.Version > ProjectVersion {
		return nil, fmt.Errorf("backup: unsupported project backup %s version %d", m.Format, m.Version)
	}
	return &m, nil
}

// ProjectVolumeStream returns the tar of one volume of a project backup,
// as the volume's own export would have written it ("./…").
// Notes: the caller closes the reader.
func ProjectVolumeStream(ctx context.Context, it Item, name string) (io.ReadCloser, error) {
	rc, err := Open(ctx, it)
	if err != nil {
		return nil, err
	}
	prefix := "volumes/" + name + "/"
	pr, pw := io.Pipe()
	go func() {
		tr := tar.NewReader(rc)
		tw := tar.NewWriter(pw)
		found := false
		for {
			h, err := tr.Next()
			if err == io.EOF {
				if !found {
					pw.CloseWithError(fmt.Errorf("backup: volume %s is not in this backup", name))
					return
				}
				pw.CloseWithError(tw.Close())
				return
			}
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			n := cleanTarName(h.Name)
			if n+"/" == prefix {
				n = prefix
			}
			if !strings.HasPrefix(n, prefix) {
				continue
			}
			found = true
			h.Name = "./" + strings.TrimPrefix(n, prefix)
			if h.Typeflag == tar.TypeLink {
				h.Linkname = "./" + strings.TrimPrefix(cleanTarName(h.Linkname), prefix)
			}
			if err := tw.WriteHeader(h); err != nil {
				pw.CloseWithError(err)
				return
			}
			if _, err := io.Copy(tw, tr); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
	}()
	return readCloser{Reader: pr, c: rc}, nil
}

// ExtractProject writes a project backup's files and bind folders into
// dir: files at its root, folders that were inside the project's folder
// at their relative path, others under OutsideFolder\<id>. A version 1
// backup writes its files only. Existing files are never overwritten.
// Returns: the folders placed, as paths under dir.
func ExtractProject(ctx context.Context, it Item, dir string) ([]string, error) {
	rc, err := Open(ctx, it)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	tr := tar.NewReader(rc)
	var man *ProjectManifest
	place := map[string]ProjectFolder{}
	var placed []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return placed, nil
		}
		if err != nil {
			return placed, err
		}
		name := cleanTarName(h.Name)
		if man == nil && name == ProjectManifestName {
			man = &ProjectManifest{}
			if err := json.NewDecoder(io.LimitReader(tr, 1<<20)).Decode(man); err != nil {
				return nil, err
			}
			for _, f := range man.Folders {
				place[f.ID] = f
			}
			continue
		}
		var target string
		switch {
		case !strings.Contains(strings.TrimSuffix(name, "/"), "/"):
			if h.Typeflag != tar.TypeReg {
				continue
			}
			target = filepath.Join(dir, SafeName(name))
		case strings.HasPrefix(name, "folders/"):
			rest := strings.TrimPrefix(name, "folders/")
			id, sub, _ := strings.Cut(rest, "/")
			f, ok := place[id]
			if !ok {
				continue
			}
			base := filepath.Join(dir, OutsideFolder, SafeName(id))
			if f.Relative != "" && filepath.IsLocal(filepath.FromSlash(f.Relative)) {
				base = filepath.Join(dir, filepath.FromSlash(f.Relative))
				if f.File {
					base = filepath.Dir(base)
				}
			}
			if sub == "" {
				if !f.File {
					placed = append(placed, base)
				}
				if err := os.MkdirAll(base, 0o755); err != nil {
					return placed, err
				}
				continue
			}
			sub = path.Clean(strings.TrimSuffix(sub, "/"))
			if f.File {
				placed = append(placed, filepath.Join(base, filepath.FromSlash(sub)))
			}
			if !filepath.IsLocal(filepath.FromSlash(sub)) {
				return placed, fmt.Errorf("backup: unsafe name %q", h.Name)
			}
			target = filepath.Join(base, filepath.FromSlash(sub))
		default:
			continue // volumes are restored into volumes
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return placed, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return placed, err
			}
			if err := writeNewFile(target, tr, h.ModTime); err != nil {
				return placed, err
			}
		}
	}
}

// writeNewFile creates path (never overwriting) with r's content.
func writeNewFile(p string, r io.Reader, mt time.Time) error {
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && !mt.IsZero() {
		os.Chtimes(p, mt, mt)
	}
	return err
}

// FolderID derives a readable, unique folder ID from a path's base name.
func FolderID(src string, used map[string]bool) string {
	base := strings.ToLower(SafeName(filepath.Base(src)))
	if base == "" || base == "." || base == ".." {
		base = "folder"
	}
	id := base
	for n := 2; used[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	used[id] = true
	return id
}
