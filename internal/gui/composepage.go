package gui

import (
	"context"
	"path/filepath"
	"regexp"
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
	pl, svc         ui.ListState
	sel             int
	tab             int // 0 services, 1 file, 2 logs

	fileFor  string
	file     *editor.State
	fileSnap configdoc.Snapshot
	fileErr  string

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

// composeView draws the Compose page.
func (a *App) composeView(c *ui.Context) {
	p := &a.cmp
	if a.k.Compose == nil {
		muted(c, L.DashChecking())
		return
	}
	if !p.loaded && !p.loading {
		p.pl.Selected = &p.sel
		a.loadProjects()
	}
	a.header(c, L.NavCompose(), func() {
		ui.Button(c, L.Refresh()).Disabled(p.loading).OnClick(a.loadProjects)
	})
	ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
		ui.PrimaryButton(c, L.CmpAdd()).OnClick(func() { p.in = composeInput{}; p.dlg = "add" })
	})
	muted(c, L.CmpHelp())
	if p.err != nil {
		errorText(c, friendlyError(p.err))
	}
	ui.Row(c).Grow(1).FillWidth().Gap(gap).Children(func() {
		cols := []ui.TableColumn{{Title: L.ColName()}, {Title: L.CmpBackend(), Width: 90}, {Title: L.ColState(), Width: 120}}
		ui.Table(c, &p.pl, cols, len(p.projects), func(row, col int) {
			pr := p.projects[row]
			switch col {
			case 0:
				ui.Text(c, pr.Name)
			case 1:
				ui.Text(c, string(pr.Backend))
			case 2:
				running, total := 0, len(p.ps[pr.ID])
				for _, s := range p.ps[pr.ID] {
					if s.State == "running" {
						running++
					}
				}
				ui.Text(c, L.CmpRunning(running, total))
			}
		}).Width(360).FillHeight()
		if p.sel >= 0 && p.sel < len(p.projects) {
			pr := p.projects[p.sel]
			ui.Column(c).Grow(1).FillHeight().Gap(gap).Children(func() { a.projectDetail(c, pr) })
		}
	})
	a.composeDialogs(c)
}

func (a *App) projectDetail(c *ui.Context, pr *compose.Project) {
	p := &a.cmp
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(smallGap).AlignItems(ui.Center).Children(func() {
		ui.Text(c, pr.Name).FontSize(sectionSize).Bold().Grow(1)
		ui.PrimaryButton(c, L.CmpUp()).OnClick(func() { a.composeJob(pr, "up", nil) })
		ui.Button(c, L.Stop()).OnClick(func() { a.composeJob(pr, "stop", nil) })
		ui.Button(c, L.Restart()).OnClick(func() { a.composeJob(pr, "restart", nil) })
		ui.Button(c, L.CmpDown()).OnClick(func() {
			p.in = composeInput{}
			p.dlg = "down"
		})
	})
	muted(c, strings.Join(pr.Files, "\n"))
	ui.Row(c).Gap(smallGap).AlignItems(ui.Center).Children(func() {
		kv(c, L.CmpBackend(), string(pr.Backend))
		muted(c, L.CmpBackendFixed())
	})
	ui.Row(c).Gap(smallGap).AlignItems(ui.Center).Children(func() {
		proj := pr
		ui.Switch(c, &pr.Autostart).Label(L.CmpAutostart()).OnChange(func() {
			a.async(func(ctx context.Context) func() {
				err := a.k.Projects.Save(proj)
				return func() {
					if err != nil {
						a.notify(friendlyError(err), true)
					}
				}
			})
		})
		ui.Text(c, L.CmpAutostart())
		muted(c, L.CmpAutostartHelp())
	})
	if rep, ok := p.reports[pr.ID]; !ok {
		a.validate(pr)
		p.reports = ensureReports(p.reports, pr.ID)
	} else if rep != nil {
		if rep.OK() && len(rep.Warnings) == 0 {
			ui.Text(c, L.CmpValid()).TextColor(t.Success)
		}
		for i, is := range rep.Errors {
			ui.Text(c.Key("e"+itoa(i)), "✕ "+is.String()).TextColor(t.Danger).Selectable()
		}
		for i, is := range rep.Warnings {
			ui.Text(c.Key("w"+itoa(i)), "! "+is.String()).TextColor(t.Warning).Selectable()
		}
	}
	ui.Tabs(c, &p.tab, L.CmpServices(), L.CmpFile(), L.CtrLogs())
	switch p.tab {
	case 0:
		ps := p.ps[pr.ID]
		cols := []ui.TableColumn{{Title: L.CmpService()}, {Title: L.ColName()}, {Title: L.ColState(), Width: 90}, {Title: L.CmpHealth(), Width: 90}, {Title: L.CtrImage()}}
		ui.Table(c, &p.svc, cols, len(ps), func(row, col int) {
			s := ps[row]
			switch col {
			case 0:
				ui.Text(c, s.Service)
			case 1:
				ui.Text(c, s.Name)
			case 2:
				ui.Text(c, s.State)
			case 3:
				ui.Text(c, s.Health)
			case 4:
				ui.Text(c, s.Image).SingleLine()
			}
		}).Grow(1).FillWidth()
		if len(ps) == 0 {
			muted(c, L.CmpNotUp())
		}
	case 1:
		a.composeFile(c, pr)
	case 2:
		if p.logsFor != pr.ID {
			a.openProjectLogs(pr)
		}
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Text(c, lastLines(p.logs.String(), 400)).Font("monospace").FontSize(11).Selectable()
		})
	}
	card(c, L.DangerZone(), func() {
		dangerButton(c, L.CmpRemove()).OnClick(func() {
			a.ask(L.CmpRemoveTitle(pr.Name), L.CmpRemoveMessage(), L.CmpRemove(), func(bool) {
				id := pr.ID
				a.async(func(ctx context.Context) func() {
					err := a.k.Projects.Delete(id)
					a.k.Jobs.Audit("compose.project.remove", pr.Name, "", err)
					return func() {
						if err != nil {
							a.notify(friendlyError(err), true)
						}
						a.cmp.sel = -1
						a.loadProjects()
					}
				})
			})
		})
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

// composeFile edits the project's first Compose file as text.
func (a *App) composeFile(c *ui.Context, pr *compose.Project) {
	p := &a.cmp
	if len(pr.Files) == 0 {
		return
	}
	path := pr.Files[0]
	if p.fileFor != path {
		p.fileFor, p.file, p.fileErr = path, nil, ""
		a.async(func(ctx context.Context) func() {
			snap, err := configdoc.ReadFile(path)
			return func() {
				if err != nil {
					p.fileErr = friendlyError(err)
					return
				}
				p.fileSnap = snap
				buf := editor.NewBuffer(snap.Text)
				buf.SetLanguage(editor.LanguageFor(path))
				p.file = &editor.State{Buffer: buf}
			}
		})
	}
	if p.fileErr != "" {
		errorText(c, p.fileErr)
	}
	if p.file == nil {
		return
	}
	editor.View(c, p.file).Grow(1).FillWidth()
	ui.Row(c).FillWidth().Gap(smallGap).Justify(ui.End).Children(func() {
		ui.Button(c, L.ShowInExplorer()).OnClick(func() { a.host.OpenPath(filepath.Dir(path)) })
		ui.PrimaryButton(c, L.CfgSave()).Disabled(!p.file.Buffer.Modified()).OnClick(func() {
			text, snap := p.file.Buffer.String(), p.fileSnap
			if err := configdoc.ParseYAML(text).Validate(); err != nil {
				p.fileErr = friendlyError(err)
				return
			}
			a.async(func(ctx context.Context) func() {
				_, err := snap.Save(text)
				return func() {
					if err == configdoc.ErrConflict {
						p.fileErr = L.CfgConflict()
						return
					}
					if err != nil {
						p.fileErr = friendlyError(err)
						return
					}
					p.fileErr, p.fileFor = "", ""
					delete(p.reports, pr.ID)
					a.notify(L.CfgSaved(), false)
				}
			})
		})
	})
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
				ui.Text(c, L.CmpAdd()).FontSize(sectionSize).Bold()
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
				ui.Text(c, L.CmpDownTitle(pr.Name)).FontSize(sectionSize).Bold()
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
