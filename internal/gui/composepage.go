package gui

import (
	"context"
	"maps"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/YuanZhe-99/WSLPilot/internal/compose"
	"github.com/YuanZhe-99/WSLPilot/internal/configdoc"
	"github.com/YuanZhe-99/WSLPilot/internal/core"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// composePage is the state of the Compose page.
type composePage struct {
	loaded, loading bool
	err             error
	projects        []*compose.Project
	ps              map[string][]compose.ContainerStatus
	reports         map[string]*compose.Report
	// data is what each project mounts and backs up (nil while loading);
	// fsize the measured size of each bind folder (-1 while measuring).
	data     map[string]*core.ProjectData
	fsize    map[string]int64
	pl, svc  ui.ListState
	sel      int // the open project (-1: the list)
	showLogs bool

	fileFor  string
	file     *editor.State
	fileSnap configdoc.Snapshot
	fileErr  string
	diagFor  *compose.Report // the report the editor marks

	logs       *logBuffer
	logsFor    string
	logsCancel context.CancelFunc

	dlg string
	in  composeInput
}

// composeInput holds the add-project dialog fields.
type composeInput struct {
	file, name    string
	removeVolumes bool
	live          bool   // backup: leave running services running
	key, mode     string // storage: the named volume and native | bind
	path          string
}

var composeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func (a *App) loadProjects() {
	p := &a.cmp
	if p.loading || a.k.Compose == nil {
		return
	}
	p.loading = true
	a.async(func(ctx context.Context) func() {
		list, err := a.k.Projects.List()
		ps := map[string][]compose.ContainerStatus{}
		for _, pr := range list {
			if b, berr := a.k.Compose.For(pr); berr == nil {
				ps[pr.ID], _ = b.Ps(ctx, pr)
			}
		}
		return func() { p.loading, p.loaded, p.err, p.projects, p.ps = false, true, err, list, ps }
	})
}

// validate runs the backend's validation for a project.
func (a *App) validate(pr *compose.Project) {
	a.async(func(ctx context.Context) func() {
		b, err := a.k.Compose.For(pr)
		var r compose.Report
		if err == nil {
			r, err = b.Validate(ctx, pr)
		}
		return func() {
			if a.cmp.reports == nil {
				a.cmp.reports = map[string]*compose.Report{}
			}
			if err != nil {
				r = compose.Report{Errors: []compose.Issue{{Message: friendlyError(err)}}}
			}
			a.cmp.reports[pr.ID] = &r
		}
	})
}

// ensureReports marks a project's report as being computed.
func ensureReports(m map[string]*compose.Report, id string) map[string]*compose.Report {
	if m == nil {
		m = map[string]*compose.Report{}
	}
	if _, ok := m[id]; !ok {
		m[id] = nil
	}
	return m
}

// composeJob runs a project command as a job.
func (a *App) composeJob(pr *compose.Project, what string, opts *compose.DownOptions) {
	title := pr.Name + " " + what
	a.startJob(title, func(ctx context.Context, j *jobs.Job) error {
		b, err := a.k.Compose.For(pr)
		if err != nil {
			return err
		}
		progress := func(e compose.Event) {
			s := string(e.Step)
			if e.Service != "" {
				s = e.Service + ": " + s
			}
			if e.Message != "" {
				s += " " + e.Message
			}
			j.SetProgress(-1, s)
			j.Logf(s)
		}
		switch what {
		case "up":
			err = b.Up(ctx, pr, compose.UpOptions{Log: j}, progress)
		case "stop":
			err = b.Stop(ctx, pr, nil, progress)
		case "restart":
			err = b.Restart(ctx, pr, nil, progress)
		case "down":
			o := compose.DownOptions{}
			if opts != nil {
				o = *opts
			}
			err = b.Down(ctx, pr, o, progress)
			a.k.Jobs.Audit("compose.down", pr.Name, "", err)
		}
		return err
	}, func() { a.loadProjects(); a.loadContainers() })
}

// openProjectLogs follows the merged logs of a project.
func (a *App) openProjectLogs(pr *compose.Project) {
	p := &a.cmp
	if p.logsCancel != nil {
		p.logsCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.logsFor, p.logsCancel, p.logs = pr.ID, cancel, &logBuffer{a: a}
	buf := p.logs
	go func() {
		b, err := a.k.Compose.For(pr)
		if err == nil {
			b.Logs(ctx, pr, compose.LogsOptions{Follow: true, Tail: 200, Timestamps: true}, buf)
		}
	}()
}

func (a *App) composeDialogs(c *ui.Context) {
	p := &a.cmp
	if p.dlg == "" {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(dialogWidth).Gap(gap).Children(func() {
			switch p.dlg {
			case "add":
				ui.Text(c, L.CmpAdd()).FontSize(16).Bold()
				muted(c, L.CmpAddHelp())
				pathField(c, a, L.CmpFile(), &p.in.file, false, "yaml", "yml")
				if p.in.name == "" && p.in.file != "" {
					p.in.name = defaultProjectName(p.in.file)
				}
				ui.Field(c, L.ColName(), func() { ui.TextInput(c, &p.in.name).Label(L.ColName()) }).Description(L.CmpNameHelp())
				ok := p.in.file != "" && composeNameRe.MatchString(p.in.name)
				dialogButtons(c, a, L.CmpAdd(), ok, func() {
					in := p.in
					a.async(func(ctx context.Context) func() {
						pr, err := compose.NewProject(in.name, in.file)
						if err == nil {
							err = a.k.Projects.Save(pr)
						}
						return func() {
							if err != nil {
								a.notify(friendlyError(err), true)
							}
							a.loadProjects()
						}
					})
				})
			case "backup":
				pr := p.projects[p.sel]
				running := false
				for _, s := range p.ps[pr.ID] {
					running = running || s.State == "running"
				}
				a.backupDialog(c, pr, running)
			case "store":
				a.storeDialog(c, p.projects[p.sel])
			case "down":
				pr := p.projects[p.sel]
				ui.Text(c, L.CmpDownTitle(pr.Name)).FontSize(16).Bold()
				muted(c, L.CmpDownMessage())
				ui.Checkbox(c, &p.in.removeVolumes, L.CmpRemoveVolumes())
				if p.in.removeVolumes {
					ui.Text(c, L.CmpRemoveVolumesWarning()).TextColor(c.Theme().Danger)
				}
				dialogButtons(c, a, L.CmpDown(), true, func() {
					o := compose.DownOptions{RemoveVolumes: p.in.removeVolumes}
					a.composeJob(pr, "down", &o)
				})
			}
		})
	})
	if !open {
		p.dlg = ""
	}
}

// defaultProjectName derives a Compose name from the file's folder.
func defaultProjectName(file string) string {
	base := strings.ToLower(filepath.Base(filepath.Dir(file)))
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-_")
}

// snapshotProject copies a project for saving on a goroutine while the
// view keeps editing the original.
func snapshotProject(p *compose.Project) *compose.Project {
	cp := *p
	cp.Files = append([]string(nil), p.Files...)
	cp.EnvFiles = append([]string(nil), p.EnvFiles...)
	cp.Profiles = append([]string(nil), p.Profiles...)
	cp.Volumes = maps.Clone(p.Volumes)
	return &cp
}
