package gui

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/compose"
	"github.com/YuanZhe-99/WSLPilot/internal/configdoc"
	"github.com/YuanZhe-99/WSLPilot/internal/core"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// composeView draws the Compose page: the projects, or one project.
func (a *App) composeView(c *ui.Context) {
	p := &a.cmp
	if a.k.Compose == nil {
		a.head(c, paneHead{icon: iconLayers, title: L.NavCompose()})
		body(c, func() { ui.Spinner(c) })
		return
	}
	if !p.loaded && !p.loading {
		a.loadProjects()
	}
	if p.sel >= 0 && p.sel < len(p.projects) {
		a.projectPage(c, p.projects[p.sel])
		a.composeDialogs(c)
		return
	}
	a.head(c, paneHead{icon: iconLayers, title: L.NavCompose(), sub: L.CmpHelp(), actions: func() {
		iconButtonAction(c, "refresh", iconRefresh, L.Refresh(), a.loadProjects)
		if ui.PrimaryButton(c.Key("add"), L.CmpAdd()).Clicked() {
			p.in, p.dlg = composeInput{}, "add"
		}
	}})
	scrollBody(c, func() {
		if p.err != nil {
			callout(c, "err", toneBad, iconAlert, friendlyError(p.err), nil)
		}
		if p.loaded && len(p.projects) == 0 {
			emptyState(c, iconLayers, L.CmpNone(), L.CmpAddHelp(), func() {
				if ui.PrimaryButton(c.Key("add-empty"), L.CmpAdd()).Clicked() {
					p.in, p.dlg = composeInput{}, "add"
				}
			})
		}
		ui.Grid(c).FillWidth().Columns(2).Gap(14).Children(func() {
			for i, pr := range p.projects {
				a.projectCard(c, i, pr)
			}
		})
	})
	a.composeDialogs(c)
}

// projectCard is one project in the list.
func (a *App) projectCard(c *ui.Context, i int, pr *compose.Project) {
	t := c.Theme()
	ps := a.cmp.ps[pr.ID]
	running := 0
	for _, s := range ps {
		if s.State == "running" {
			running++
		}
	}
	b := ui.ButtonBase(c.Key("proj-" + pr.ID)).FillWidth().Label(pr.Name)
	face := t.Background
	if b.Hovered() {
		face = t.SurfaceHover
	}
	b.Children(func() {
		ui.Column(c).FillWidth().Gap(8).Padding(16).Radius(cardRadius).Border(1, t.Border).Background(face).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconLayers).Size(18, 18).TextColor(t.TextMuted)
				ui.Text(c, pr.Name).FontSize(sizeHeading).Bold().Grow(1).SingleLine()
				k := toneNeutral
				if running > 0 && running == len(ps) {
					k = toneGood
				} else if running > 0 {
					k = toneWarn
				}
				pill(c, L.CmpRunning(running, len(ps)), k)
			})
			small(c, strings.Join(pr.Files, " · ")).SingleLine()
			ui.Row(c).Gap(8).Children(func() {
				pill(c, L.CmpBackendIs(string(pr.Backend)), toneNeutral)
				if pr.Autostart {
					pill(c, L.CmpAutostart(), toneAccent)
				}
			})
		})
	})
	if b.Clicked() {
		a.cmp.sel = i
	}
}

// projectPage is one project as the design: its services and data on the
// left, its Compose file and the validation on the right.
func (a *App) projectPage(c *ui.Context, pr *compose.Project) {
	p := &a.cmp
	ps := p.ps[pr.ID]
	running := 0
	for _, s := range ps {
		if s.State == "running" {
			running++
		}
	}
	rep, validated := p.reports[pr.ID]
	if !validated {
		a.validate(pr)
		p.reports = ensureReports(p.reports, pr.ID)
	}
	if _, ok := p.data[pr.ID]; !ok {
		if p.data == nil {
			p.data = map[string]*core.ProjectData{}
		}
		p.data[pr.ID] = nil
		a.loadProjectData(pr)
		if !a.vol.loaded {
			a.loadVolumes()
		}
	}
	blocked := rep != nil && !rep.OK()
	a.head(c, paneHead{crumb: L.NavCompose(), onCrumb: func() { p.sel = -1 }, title: pr.Name, sub: strings.Join(pr.Files, " · "), badge: func() {
		k := toneNeutral
		if running > 0 {
			k = toneGood
		}
		pill(c, L.CmpRunningOf(running, len(ps)), k)
		pill(c, L.CmpBackendIs(string(pr.Backend)), toneNeutral)
	}, actions: func() {
		if ui.Button(c.Key("stop"), L.Stop()).Disabled(running == 0).Clicked() {
			a.composeJob(pr, "stop", nil)
		}
		if dangerButton(c, L.CmpDown()).Disabled(len(ps) == 0).Tooltip(L.CmpDownMessage()).Clicked() {
			p.in, p.dlg = composeInput{}, "down"
		}
		up := L.CmpUpStart()
		if len(ps) > 0 {
			up = L.CmpApply()
		}
		if ui.PrimaryButton(c.Key("up"), up).Disabled(blocked).Tooltip(L.CmpApplyHelp()).Clicked() {
			a.composeJob(pr, "up", nil)
		}
	}})
	ui.Row(c.Key("proj-main")).Grow(1).FillWidth().MinHeight(0).Padding(4, panePadX, panePadX, panePadX).Gap(20).AlignItems(ui.Stretch).Children(func() {
		ui.Scroll(c.Key("proj-left")).Width(420).Shrink(0).Gap(12).Children(func() {
			cardHead(c, L.CmpServices(), func() {
				if ui.Button(c.Key("logs"), L.CmpCombinedLogs()).Clicked() {
					p.showLogs = !p.showLogs
					if p.showLogs {
						a.openProjectLogs(pr)
					}
				}
				if ui.Button(c.Key("restart"), L.Restart()).Disabled(running == 0).Clicked() {
					a.composeJob(pr, "restart", nil)
				}
			})
			if len(ps) == 0 {
				small(c, L.CmpNotUp())
			}
			var sds map[string]core.ServiceData
			if d := p.data[pr.ID]; d != nil {
				sds = d.Services
			}
			for _, s := range ps {
				a.serviceCard(c, s, sds[s.Service])
			}
			a.dataCard(c, pr)
			card(c, L.CmpProject(), func() {
				ui.Row(c.Key("autostart-row")).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
						ui.Text(c, L.CmpAutostart()).FontWeight(600)
						small(c, L.CmpAutostartHelp())
					})
					ui.Switch(c.Key("autostart"), &pr.Autostart).Label(L.CmpAutostart()).OnChange(func() {
						proj := snapshotProject(pr)
						a.async(func(ctx context.Context) func() {
							err := a.k.Projects.Save(proj)
							return func() {
								if err != nil {
									a.notify(friendlyError(err), true)
								}
							}
						})
					})
				})
				ui.Row(c.Key("remove-row")).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
					small(c, L.CmpRemoveMessage()).Grow(1).MinWidth(0)
					if dangerButton(c, L.CmpRemove()).Clicked() {
						a.ask(L.CmpRemoveTitle(pr.Name), L.CmpRemoveMessage(), L.CmpRemove(), func(bool) {
							id := pr.ID
							a.async(func(ctx context.Context) func() {
								err := a.k.Projects.Delete(id)
								a.k.Jobs.Audit("compose.project.remove", pr.Name, "", err)
								return func() {
									if err != nil {
										a.notify(friendlyError(err), true)
									}
									p.sel = -1
									a.loadProjects()
								}
							})
						})
					}
				})
			})
		})
		ui.Column(c.Key("proj-right")).Grow(1).MinWidth(0).Gap(12).Children(func() {
			if p.showLogs {
				text := ""
				if p.logs != nil {
					text = lastLines(p.logs.String(), 300)
				}
				if text == "" {
					text = L.CtrNoLogs()
				}
				console(c, "proj-logs", text, 2000).Grow(1)
				return
			}
			a.composeFileCard(c, pr, rep)
		})
	})
}

// composeFileCard is compose.yaml in the editor with Validate and its
// problems below, the lines of unsupported keys marked.
func (a *App) composeFileCard(c *ui.Context, pr *compose.Project, rep *compose.Report) {
	p := &a.cmp
	t := c.Theme()
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
				p.diagFor = nil
			}
		})
	}
	ui.Column(c.Key("file-card")).Grow(1).MinHeight(0).FillWidth().Radius(cardRadius).Border(1, t.Border).Background(t.Background).Clip().Children(func() {
		ui.Row(c).FillWidth().Height(48).Shrink(0).Padding(0, 12, 0, 16).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, filepath.Base(path)).Bold().Grow(1).MinWidth(0).SingleLine().Tooltip(path)
			if p.file != nil && p.file.Buffer.Modified() {
				small(c, L.CmpEdited())
			}
			if ui.Button(c.Key("explorer"), L.ShowInExplorer()).Clicked() {
				a.host.OpenPath(filepath.Dir(path))
			}
			if ui.Button(c.Key("validate"), L.CmpValidate()).Clicked() {
				delete(p.reports, pr.ID)
			}
			if p.file != nil && ui.PrimaryButton(c.Key("save"), L.CfgSave()).Disabled(!p.file.Buffer.Modified()).Clicked() {
				a.saveComposeFile(pr)
			}
		})
		ui.Box(c).Height(1).FillWidth().Background(t.Border)
		if p.fileErr != "" {
			ui.Box(c).Padding(10, 16).Children(func() { errorText(c, p.fileErr) })
		}
		if p.file == nil {
			ui.Box(c).Padding(16).Children(func() { ui.Spinner(c) })
			return
		}
		if rep != nil && p.diagFor != rep {
			p.diagFor = rep
			p.file.SetDiagnostics(p.file.Buffer.Version(), issueDiagnostics(p.file.Buffer.String(), rep))
		}
		editor.View(c, p.file).Grow(1).FillWidth().MinHeight(200)
		ui.Box(c).Height(1).FillWidth().Background(t.Border)
		ui.Column(c.Key("issues")).FillWidth().Shrink(0).MaxHeight(180).Padding(12, 16).Gap(6).Background(t.Danger.Alpha(0.03)).Children(func() {
			switch {
			case rep == nil:
				ui.Row(c).Gap(8).Children(func() { ui.Spinner(c).Size(14, 14); small(c, L.CmpValidating()) })
			default:
				for i, is := range rep.Errors {
					issueLine(c, "e"+itoa(i), L.CmpError(), toneBad, is.String())
				}
				for i, is := range rep.Warnings {
					issueLine(c, "w"+itoa(i), L.CmpWarning(), toneWarn, is.String())
				}
				if rep.OK() && len(rep.Warnings) == 0 {
					ui.Row(c.Key("ok")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconCheck).Size(16, 16).TextColor(t.Success)
						ui.Text(c, L.CmpValid()).FontSize(13).TextColor(t.Success).Grow(1)
					})
				}
				native := L.CmpNativeNo()
				if a.report != nil && a.report.NativeCompose {
					native = L.CmpNativeYes()
				}
				ui.Row(c.Key("info")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconInfo).Size(16, 16).TextColor(t.TextMuted)
					small(c, native).Grow(1)
				})
			}
		})
	})
}

// issueLine is one problem of the panel: its kind, then the message.
func issueLine(c *ui.Context, key, kind string, k tone, msg string) {
	ui.Row(c.Key(key)).FillWidth().Gap(12).Children(func() {
		ui.Text(c, kind).FontSize(13).FontWeight(600).TextColor(toneColor(c.Theme(), k)).MinWidth(56).NoWrap().Shrink(0)
		ui.Text(c, msg).FontSize(13).Grow(1).Selectable()
	})
}

// issueDiagnostics places validation errors on the lines of their keys:
// the key's last segment under the service's block, else the service.
func issueDiagnostics(text string, rep *compose.Report) []editor.Diagnostic {
	var ds []editor.Diagnostic
	add := func(list []compose.Issue, sev editor.Severity) {
		for _, is := range list {
			if start, end, ok := locateKey(text, is.Service, is.Key); ok {
				ds = append(ds, editor.Diagnostic{Start: start, End: end, Severity: sev, Message: is.Message, Source: "WSLPilot"})
			}
		}
	}
	add(rep.Errors, editor.SeverityError)
	add(rep.Warnings, editor.SeverityWarning)
	return ds
}

// locateKey finds the byte range of "key:" inside service's block of a
// Compose file (a line search, enough to mark the line).
func locateKey(text, service, key string) (int, int, bool) {
	seg := key
	if i := strings.LastIndex(key, "."); i >= 0 {
		seg = key[i+1:]
	}
	off, inService := 0, service == ""
	for _, line := range strings.SplitAfter(text, "\n") {
		trim := strings.TrimSpace(line)
		if service != "" && strings.HasPrefix(trim, service+":") {
			inService = true
		} else if inService && seg != "" && strings.HasPrefix(trim, seg+":") {
			s := off + strings.Index(line, seg)
			return s, s + len(strings.TrimRight(strings.TrimSpace(line), "\r")), true
		}
		off += len(line)
	}
	return 0, 0, false
}

// saveComposeFile writes the edited Compose file (conflict-checked, backed
// up), then validates again.
func (a *App) saveComposeFile(pr *compose.Project) {
	p := &a.cmp
	text, snap := p.file.Buffer.String(), p.fileSnap
	if err := configdoc.ParseYAML(text).Validate(); err != nil {
		p.fileErr = friendlyError(err)
		return
	}
	a.async(func(ctx context.Context) func() {
		_, err := snap.Save(text)
		return func() {
			switch {
			case err == configdoc.ErrConflict:
				p.fileErr = L.CfgConflict()
			case err != nil:
				p.fileErr = friendlyError(err)
			default:
				p.fileErr, p.fileFor = "", ""
				delete(p.reports, pr.ID)
				a.notify(L.CfgSaved(), false)
			}
		}
	})
}

// backupProjectNow backs a project up into the default repository; live
// leaves running services running.
func (a *App) backupProjectNow(name string, live bool) {
	a.startJob(L.CmpBackUpTitle(name), func(ctx context.Context, j *jobs.Job) error {
		if a.k.ComposeBackup == nil {
			return nil
		}
		w := &backup.RepoWriter{Dir: filepath.Join(a.backupDir(), "Repository")}
		o := backupOptions()
		o.Live = live
		_, err := a.k.ComposeBackup(ctx, name, w, o)
		return err
	}, func() { a.bk.loaded = false })
}
