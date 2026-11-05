package compose

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/YuanZhe-99/WSLPilot/internal/store"
)

// ProjectSchema is the newest project.json schema this build reads and writes.
const ProjectSchema = 1

// Volume modes of a named volume.
const (
	// VolumeNative keeps the named volume as a wslc volume (default).
	VolumeNative = "native"
	// VolumeBind replaces the named volume with a bind mount of a Windows folder.
	VolumeBind = "bind"
)

// VolumeChoice is how one named volume of the project is provided.
type VolumeChoice struct {
	Mode string `json:"mode"` // native | bind
	// Path is the Windows folder for bind mode (absolute).
	Path string `json:"path,omitempty"`
}

// Project is WSLPilot's metadata about one Compose project, stored as
// projects/<id>/project.json. The user's Compose files stay where they are
// and are never copied or rewritten.
type Project struct {
	SchemaVersion int `json:"schemaVersion"`
	// ID is a random hex identifier; it names the metadata directory.
	ID string `json:"id"`
	// Name is the Compose project name (labels, resource prefixes).
	Name string `json:"name"`
	// Backend is fixed at creation; only Migrate changes it.
	Backend BackendID `json:"backend"`
	// Files are absolute paths to the Compose files, merged in order.
	Files []string `json:"files"`
	// EnvFiles are absolute paths of project env files; empty uses the
	// working directory's .env when present.
	EnvFiles []string `json:"envFiles,omitempty"`
	// Profiles are the active Compose profiles.
	Profiles []string `json:"profiles,omitempty"`
	// WorkingDir is the project directory; empty means the first file's folder.
	WorkingDir string `json:"workingDir,omitempty"`
	// Autostart asks the WSLPilot agent to bring the project up at sign-in.
	Autostart bool `json:"autostart,omitempty"`
	// Volumes maps a Compose volume key to its provision; absent keys are native.
	Volumes map[string]VolumeChoice `json:"volumes,omitempty"`
}

// projectName is the Compose project name rule.
var projectName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// idPattern restricts IDs so they are safe directory names.
var idPattern = regexp.MustCompile(`^[0-9a-f]{16,64}$`)

// NewProject returns metadata for a new project on the builtin backend.
// Inputs: name must follow the Compose rule (lowercase letters, digits,
// '-' and '_', starting with a letter or digit); files must be absolute.
// Returns: the project with a fresh random ID; it is not saved yet.
func NewProject(name string, files ...string) (*Project, error) {
	p := &Project{SchemaVersion: ProjectSchema, Name: name, Backend: BackendBuiltin, Files: files}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	p.ID = hex.EncodeToString(b[:])
	return p, p.Check()
}

// Check reports metadata that cannot be used: a bad name, ID or backend,
// relative paths, or an invalid volume choice.
func (p *Project) Check() error {
	if !projectName.MatchString(p.Name) {
		return fmt.Errorf("compose: invalid project name %q (lowercase letters, digits, '-' and '_')", p.Name)
	}
	if !idPattern.MatchString(p.ID) {
		return fmt.Errorf("compose: invalid project id %q", p.ID)
	}
	if p.Backend != BackendBuiltin && p.Backend != BackendNative {
		return fmt.Errorf("%w: %q", ErrUnknownBackend, p.Backend)
	}
	if len(p.Files) == 0 {
		return errors.New("compose: a project needs at least one Compose file")
	}
	for _, f := range append(append([]string{}, p.Files...), p.EnvFiles...) {
		if !filepath.IsAbs(f) {
			return fmt.Errorf("compose: path %q is not absolute", f)
		}
	}
	if p.WorkingDir != "" && !filepath.IsAbs(p.WorkingDir) {
		return fmt.Errorf("compose: working directory %q is not absolute", p.WorkingDir)
	}
	for k, v := range p.Volumes {
		switch v.Mode {
		case "", VolumeNative:
		case VolumeBind:
			if !filepath.IsAbs(v.Path) {
				return fmt.Errorf("compose: volume %q: bind mode needs an absolute folder", k)
			}
		default:
			return fmt.Errorf("compose: volume %q: unknown mode %q", k, v.Mode)
		}
	}
	return nil
}

// Projects stores project metadata in a store.Store.
type Projects struct{ Store *store.Store }

// projectDoc is the document name of a project's metadata.
func projectDoc(id string) string { return "projects/" + id + "/project.json" }

// Save writes the project's metadata.
// Side effects: writes projects/<id>/project.json atomically.
func (s Projects) Save(p *Project) error {
	if err := p.Check(); err != nil {
		return err
	}
	p.SchemaVersion = ProjectSchema
	return s.Store.Save(projectDoc(p.ID), ProjectSchema, p)
}

// Load reads one project's metadata.
// Returns: fs.ErrNotExist when missing; store.ErrNewerSchema when written
// by a newer WSLPilot.
func (s Projects) Load(id string) (*Project, error) {
	if !idPattern.MatchString(id) {
		return nil, fmt.Errorf("compose: invalid project id %q", id)
	}
	var p Project
	v, err := s.Store.Load(projectDoc(id), ProjectSchema, &p)
	if err != nil {
		return nil, err
	}
	p.SchemaVersion = v
	return &p, nil
}

// List reads every stored project, sorted by name.
// Notes: directories without a readable project.json are skipped; a
// document from a newer WSLPilot is an error so it is never overwritten.
func (s Projects) List() ([]*Project, error) {
	ids, err := s.Store.List("projects")
	if err != nil {
		return nil, err
	}
	var res []*Project
	for _, id := range ids {
		if !idPattern.MatchString(id) {
			continue
		}
		p, err := s.Load(id)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		res = append(res, p)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Name < res[j].Name })
	return res, nil
}

// Delete removes a project's metadata. It never touches the Compose files
// or the project's containers.
func (s Projects) Delete(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("compose: invalid project id %q", id)
	}
	return s.Store.Delete("projects/" + id)
}
