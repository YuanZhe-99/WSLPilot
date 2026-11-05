package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

// backupProject stores a project's files as one item and each of its
// native named volumes as another, all with the same plan tag.
// Returns: the files item; volume failures are joined into the error.
func (k *Core) backupProject(ctx context.Context, name string, w backup.Writer, o backup.Options) (backup.Item, error) {
	p, err := k.FindProject(name)
	if err != nil {
		return backup.Item{}, err
	}
	files := map[string]string{}
	for _, f := range append(append([]string{}, p.Files...), p.EnvFiles...) {
		files[filepath.Base(f)] = f
	}
	if env := filepath.Join(projectDir(p), ".env"); len(p.EnvFiles) == 0 && fileExists(env) {
		files[".env"] = env
	}
	it, err := k.Backup.BackupFiles(ctx, backup.Source{Kind: backup.SourceCompose, Name: p.Name}, files, w, o)
	if err != nil {
		return it, err
	}
	loaded, err := compose.Load(ctx, p)
	if err != nil {
		return it, err
	}
	plan, _ := compose.Translate(loaded, p)
	var errs []error
	for _, v := range plan.Volumes {
		if v.Bind != "" || v.External {
			continue
		}
		if _, err := k.Backup.BackupVolume(ctx, v.Name, w, o); err != nil {
			errs = append(errs, fmt.Errorf("volume %s: %w", v.Name, err))
		}
	}
	return it, errors.Join(errs...)
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
