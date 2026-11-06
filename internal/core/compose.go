package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/compose"
)

// WireCompose sets up the Compose registry and the hooks that back up and
// autostart projects.
// Notes: the native backend is registered with the probe's answer; it
// stays unavailable until WSL ships a compose command, and projects never
// move to it on their own.
func (k *Core) WireCompose(nativeCompose bool) {
	k.Compose = compose.Registry{
		compose.BackendBuiltin: &compose.Builtin{Engine: k.WSLc},
		compose.BackendNative:  &compose.Native{HasCompose: nativeCompose},
	}
	k.Projects = compose.Projects{Store: k.Store}
	k.ComposeBackup = k.backupProject
	k.AutostartCompose = k.autostart
}

// FindProject returns the project with the Compose name.
func (k *Core) FindProject(name string) (*compose.Project, error) {
	ps, err := k.Projects.List()
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no Compose project named %s", name)
}

// ProjectData is what a Compose project keeps its data in: the files,
// the bind folders and the named volumes a backup of it holds, and what
// each service mounts (doc/en-us/backups.md, "Compose project backups").
type ProjectData struct {
	WorkingDir string
	Files      map[string]string // name in the backup → Windows path
	Folders    []backup.ProjectFolder
	Volumes    []backup.ProjectVolume
	// External volumes are used but not created by the project; a project
	// backup leaves them out.
	External []string
	Services map[string]ServiceData
}

// ServiceData is what one service publishes and mounts.
type ServiceData struct {
	Ports  []string // "127.0.0.1:59884 → 3000"
	Mounts []Mount
}

// Mount is one mount of a service.
type Mount struct {
	Kind     string // MountFolder, MountVolume or "tmpfs"
	Source   string // Windows path or volume name
	Display  string // "./vc-data" inside the project folder, else Source
	Target   string
	ReadOnly bool
}

// Mount kinds.
const (
	MountFolder = "folder"
	MountVolume = "volume"
)

// ProjectData reads a project's Compose files and lists its data.
func (k *Core) ProjectData(ctx context.Context, p *compose.Project) (ProjectData, error) {
	d := ProjectData{WorkingDir: projectDir(p), Files: map[string]string{}, Services: map[string]ServiceData{}}
	for _, f := range append(append([]string{}, p.Files...), p.EnvFiles...) {
		d.Files[filepath.Base(f)] = f
	}
	if env := filepath.Join(d.WorkingDir, ".env"); len(p.EnvFiles) == 0 && fileExists(env) {
		d.Files[".env"] = env
	}
	loaded, err := compose.Load(ctx, p)
	if err != nil {
		return d, err
	}
	plan, _ := compose.Translate(loaded, p)
	folders := map[string]int{} // lower-case path → index
	ids := map[string]bool{}
	vols := map[string]int{}
	for _, v := range plan.Volumes {
		switch {
		case v.External:
			d.External = append(d.External, v.Name)
		case v.Bind == "":
			vols[v.Name] = len(d.Volumes)
			d.Volumes = append(d.Volumes, backup.ProjectVolume{Key: v.Key, Name: v.Name})
		}
	}
	for _, s := range plan.Services {
		sd := ServiceData{}
		if len(s.Containers) > 0 {
			spec := s.Containers[0].Spec
			for _, pb := range spec.Ports {
				host := pb.HostPort
				if pb.HostIP != "" {
					host = pb.HostIP + ":" + host
				}
				port := host + " → " + pb.ContainerPort
				if pb.Protocol == "udp" {
					port += "/udp"
				}
				sd.Ports = append(sd.Ports, port)
			}
			for _, m := range spec.Mounts {
				mt := Mount{Kind: m.Type, Source: m.Source, Display: m.Source, Target: m.Target, ReadOnly: m.ReadOnly}
				switch m.Type {
				case "bind":
					mt.Kind = MountFolder
					rel := relativeTo(d.WorkingDir, m.Source)
					if rel != "" {
						mt.Display = "./" + rel
					}
					key := strings.ToLower(filepath.Clean(m.Source))
					i, ok := folders[key]
					if !ok {
						st, serr := os.Stat(m.Source)
						i = len(d.Folders)
						folders[key] = i
						d.Folders = append(d.Folders, backup.ProjectFolder{ID: backup.FolderID(m.Source, ids), Source: m.Source,
							Relative: rel, File: serr == nil && !st.IsDir()})
					}
					d.Folders[i].Services = appendOnce(d.Folders[i].Services, s.Name)
				case "volume":
					mt.Kind = MountVolume
					if i, ok := vols[m.Source]; ok {
						d.Volumes[i].Services = appendOnce(d.Volumes[i].Services, s.Name)
					}
				}
				sd.Mounts = append(sd.Mounts, mt)
			}
		}
		d.Services[s.Name] = sd
	}
	return d, nil
}

// relativeTo returns p relative to dir with slashes, or "" when p is not
// inside dir.
func relativeTo(dir, p string) string {
	if dir == "" {
		return ""
	}
	rel, err := filepath.Rel(dir, p)
	if err != nil || !filepath.IsLocal(rel) {
		return ""
	}
	return filepath.ToSlash(rel)
}

func appendOnce(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// backupProject stores a project as one item: its files, bind folders and
// named volumes (layout version 2). Running containers are stopped first
// unless the backup is live, and only those are started again.
func (k *Core) backupProject(ctx context.Context, name string, w backup.Writer, o backup.Options) (backup.Item, error) {
	p, err := k.FindProject(name)
	if err != nil {
		return backup.Item{}, err
	}
	d, err := k.ProjectData(ctx, p)
	if err != nil {
		return backup.Item{}, err
	}
	spec := backup.ProjectSpec{Name: p.Name, WorkingDir: d.WorkingDir, Files: d.Files, Folders: d.Folders, Volumes: d.Volumes, External: d.External}
	if b, berr := k.Compose.For(p); berr == nil {
		var running []string
		if ps, perr := b.Ps(ctx, p); perr == nil {
			for _, c := range ps {
				if c.State == "running" {
					running = appendOnce(running, c.Service)
				}
			}
		}
		spec.Running = len(running) > 0
		spec.Stop = func(ctx context.Context) error { return b.Stop(ctx, p, nil, nil) }
		spec.Start = func(ctx context.Context) { b.Start(ctx, p, running, nil) }
	}
	return k.Backup.BackupProject(ctx, spec, w, o)
}

// autostart brings up every project marked autostart, with its own backend.
func (k *Core) autostart(ctx context.Context, log func(string)) {
	ps, err := k.Projects.List()
	if err != nil {
		log("compose autostart: " + err.Error())
		return
	}
	for _, p := range ps {
		if !p.Autostart {
			continue
		}
		b, err := k.Compose.For(p)
		if err == nil {
			err = b.Up(ctx, p, compose.UpOptions{}, nil)
		}
		if err != nil {
			log("compose autostart " + p.Name + ": " + err.Error())
		} else {
			log("compose autostart " + p.Name + ": up")
		}
	}
}

// projectDir is the project's working directory.
func projectDir(p *compose.Project) string {
	if p.WorkingDir != "" {
		return p.WorkingDir
	}
	if len(p.Files) > 0 {
		return filepath.Dir(p.Files[0])
	}
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// SetNativeCompose records whether WSL has a native compose command. Call
// it on the thread that reads the registry (the UI thread).
func (k *Core) SetNativeCompose(on bool) {
	if n, ok := k.Compose[compose.BackendNative].(*compose.Native); ok {
		n.HasCompose = on
	}
}
