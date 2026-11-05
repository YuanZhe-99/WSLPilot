// Package core wires WSLPilot's packages together once, for the GUI, the
// agent and the CLI: adapters, the metadata store, the job manager, the
// backup runner and the plan scheduler.
package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/capability"
	"github.com/YuanZhe-99/WSLPilot/internal/compose"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/scheduler"
	"github.com/YuanZhe-99/WSLPilot/internal/secret"
	"github.com/YuanZhe-99/WSLPilot/internal/store"
	"github.com/YuanZhe-99/WSLPilot/internal/volume"
	"github.com/YuanZhe-99/WSLPilot/internal/wsl"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// Core is the set of services one process uses.
type Core struct {
	WSL       *wsl.Client
	WSLc      *wslc.Client
	Store     *store.Store
	Jobs      *jobs.Manager
	Volumes   *volume.Pipeline
	Backup    *backup.Runner
	Scheduler *scheduler.Service
	Prober    *capability.Prober
	// Compose and Projects are set by WireCompose.
	Compose  compose.Registry
	Projects compose.Projects
	// ComposeBackup backs up a Compose project (its files and volumes);
	// set by the compose wiring.
	passwords     sync.Map // repository dir → password typed this session
	ComposeBackup func(ctx context.Context, project string, w backup.Writer, o backup.Options) (backup.Item, error)
	// AutostartCompose brings up the Compose projects marked autostart;
	// set by the compose wiring.
	AutostartCompose func(ctx context.Context, log func(string))
}

// New builds the services with the real programs and the default store.
func New() (*Core, error) {
	st, err := store.Default()
	if err != nil {
		return nil, err
	}
	w, c := wsl.New(), wslc.New()
	host, _ := os.Hostname()
	vol := &volume.Pipeline{E: c}
	k := &Core{
		WSL: w, WSLc: c, Store: st,
		Jobs:    &jobs.Manager{AuditPath: st.Path("logs/audit.jsonl")},
		Volumes: vol,
		Backup:  &backup.Runner{WSL: w, WSLc: c, Vol: vol, Host: host},
		Prober:  &capability.Prober{WSL: w, WSLc: c},
	}
	k.Scheduler = &scheduler.Service{Store: st, Do: k.BackupTarget}
	k.ApplyPrefs(k.LoadPrefs())
	backup.RepoPassword = k.repoPassword
	return k, nil
}

// BackupTarget backs up one plan target into w.
func (k *Core) BackupTarget(ctx context.Context, t scheduler.Target, w backup.Writer, o backup.Options) (backup.Item, error) {
	switch t.Kind {
	case backup.SourceDistro:
		return k.Backup.BackupDistro(ctx, t.Name, w, o)
	case backup.SourceVolume:
		return k.Backup.BackupVolume(ctx, t.Name, w, o)
	}
	if k.ComposeBackup != nil && t.Kind == backup.SourceCompose {
		return k.ComposeBackup(ctx, t.Name, w, o)
	}
	return backup.Item{}, &UnknownTarget{Kind: t.Kind}
}

// UnknownTarget is a plan target kind this build cannot back up.
type UnknownTarget struct{ Kind string }

// Error names the kind.
func (e *UnknownTarget) Error() string { return "unknown backup target kind " + e.Kind }

// AgentPath returns wslpilot-agent.exe next to the running program.
func AgentPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "wslpilot-agent.exe"
	}
	return filepath.Join(filepath.Dir(exe), "wslpilot-agent.exe")
}

// SettingsDoc is the app preferences document shared by the GUI and the
// agent (see doc/en-us/app-data.md).
const SettingsDoc = "settings.json"

// Prefs are the preferences in SettingsDoc.
type Prefs struct {
	Theme     string `json:"theme,omitempty"` // system, light, dark
	BackupDir string `json:"backupDir,omitempty"`
	// HelperImage is the image for volume transfers ("" = alpine:3.20).
	HelperImage string `json:"helperImage,omitempty"`
}

// LoadPrefs reads the preferences (zero values when missing).
func (k *Core) LoadPrefs() Prefs {
	var p Prefs
	k.Store.Load(SettingsDoc, 1, &p)
	return p
}

// ApplyPrefs puts preferences that affect services into effect.
func (k *Core) ApplyPrefs(p Prefs) { k.Volumes.SetImage(p.HelperImage) }

// RememberPassword keeps an encrypted repository's password in memory for
// this process (the user typed it to open the repository).
func (k *Core) RememberPassword(dir, password string) {
	k.passwords.Store(strings.ToLower(filepath.Clean(dir)), password)
}

// repoPassword finds the password of the repository in dir: one typed in
// this session, else the DPAPI-protected one of a plan writing there.
func (k *Core) repoPassword(dir string) (string, bool) {
	key := strings.ToLower(filepath.Clean(dir))
	if v, ok := k.passwords.Load(key); ok {
		return v.(string), true
	}
	ps, _ := k.Scheduler.Plans()
	for _, p := range ps {
		if p.Destination.ProtectedPassword != "" && strings.ToLower(filepath.Clean(p.Destination.Path)) == key {
			if pw, err := secret.Unprotect(p.Destination.ProtectedPassword); err == nil {
				return pw, true
			}
		}
	}
	return "", false
}
