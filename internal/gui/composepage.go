package gui

import (
	"context"
	"maps"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/YuanZhe-99/WSLPilot/internal/compose"
	"github.com/YuanZhe-99/WSLPilot/internal/configdoc"
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
	volKeys         map[string][]string // project ID → named volume keys
	volMode         map[string]*string  // "id NUL key" → native | bind
	volPath         map[string]*string
	pl, svc         ui.ListState
	sel             int // the open project (-1: the list)
	showLogs        bool

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
		var keys []string
		if loaded, lerr := compose.Load(ctx, pr); lerr == nil {
			for k, v := range loaded.Volumes {
				if !bool(v.External) {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
		}
		return func() {
			if a.cmp.reports == nil {
				a.cmp.reports = map[string]*compose.Report{}
			}
			if err != nil {
				r = compose.Report{Errors: []compose.Issue{{Message: friendlyError(err)}}}
			}
			a.cmp.reports[pr.ID] = &r
			if a.cmp.volKeys == nil {
				a.cmp.volKeys = map[string][]string{}
			}
			a.cmp.volKeys[pr.ID] = keys
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

// projectVolumes chooses, per named volume, a WSLc volume or a Windows
// folder (bind mount).
func (a *App) projectVolumes(c *ui.Context, pr *compose.Project) {
	p := &a.cmp
	keys := p.volKeys[pr.ID]
	if len(keys) == 0 {
		return
	}
	if p.volMode == nil {
		p.volMode, p.volPath = map[string]*string{}, map[string]*string{}
	}
	card(c, L.CmpVolumes(), func() {
		muted(c, L.CmpVolumesHelp())
		changed := false
		for _, key := range keys {
			id := pr.ID + "\x00" + key
			if p.volMode[id] == nil {
				ch := pr.Volumes[key]
				mode, path := ch.Mode, ch.Path
				if mode == "" {
					mode = "native"
				}
				p.volMode[id], p.volPath[id] = &mode, &path
			}
			mode, path := p.volMode[id], p.volPath[id]
			cur := pr.Volumes[key]
			if (cur.Mode == "" && *mode != "native") || (cur.Mode != "" && cur.Mode != *mode) || cur.Path != *path {
				changed = true
			}
			ui.Row(c.Key(id)).FillWidth().Gap(smallGap).AlignItems(ui.Center).Children(func() {
				ui.Text(c, key).Width(140)
				ui.Select(c, mode, []string{"native", "bind"}).Label(key)
				if *mode == "bind" {
					ui.TextInput(c, path).Placeholder(`C:\data\` + key).Label(key).Grow(1)
				}
			})
		}
		if changed {
			ui.Text(c, L.CmpVolumesWarning()).TextColor(c.Theme().Warning)
			ui.PrimaryButton(c, L.Save()).OnClick(func() {
				if pr.Volumes == nil {
					pr.Volumes = map[string]compose.VolumeChoice{}
				}
				for _, key := range keys {
					id := pr.ID + "\x00" + key
					if *p.volMode[id] == "native" {
						delete(pr.Volumes, key)
					} else {
						pr.Volumes[key] = compose.VolumeChoice{Mode: "bind", Path: strings.TrimSpace(*p.volPath[id])}
					}
				}
				proj := snapshotProject(pr)
				a.async(func(ctx context.Context) func() {
					err := a.k.Projects.Save(proj)
					return func() {
						if err != nil {
							a.notify(friendlyError(err), true)
							return
						}
						delete(a.cmp.reports, proj.ID)
						a.notify(L.CfgSaved(), false)
					}
				})
			})
		}
	})
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
