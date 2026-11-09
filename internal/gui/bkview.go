package gui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/scheduler"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// backupsView draws the Backups page: restore first, or the plans.
func (a *App) backupsView(c *ui.Context) {
	p := &a.bk
	if !p.loaded && !p.loading {
		a.loadCatalog()
	}
	a.ensurePlans()
	a.head(c, paneHead{icon: iconHistory, title: L.NavBackups(), actions: func() {
		ui.Segmented(c.Key("view"), &p.view, L.BkRestore(), L.BkPlans()).Label(L.NavBackups())
		if p.view == 1 {
			if ui.PrimaryButton(c.Key("new-plan"), L.BkNewPlan()).Clicked() {
				a.editPlan(nil)
			}
			return
		}
		iconButtonAction(c, "refresh", iconRefresh, L.Refresh(), func() { p.loaded = false; a.loadCatalog() })
		if ui.PrimaryButton(c.Key("open"), L.BkOpenBackup()).Clicked() {
			a.pickBackup(false)
		}
	}})
	if p.view == 1 {
		a.plansView(c)
	} else {
		a.restoreView(c)
	}
	a.backupDialogs(c)
}

// pickBackup asks for a backup file or folder and adds its location.
func (a *App) pickBackup(folder bool) {
	go func() {
		var ps []string
		if folder {
			ps = a.host.PickFiles(L.BkOpenFolder(), true)
		} else {
			ps = a.host.PickFiles(L.BkOpenFile(), false, "tar", "gz", "tgz", "zst", "xz", "vhdx")
		}
		if len(ps) > 0 {
			a.update(func() { a.addLocation(ps[0]) })
		}
	}()
}

// restoreView: on the left, open a backup made anywhere, what can be
// restored and the locations; on the right, the versions of the chosen
// one and how to restore it.
func (a *App) restoreView(c *ui.Context) {
	p := &a.bk
	t := c.Theme()
	ui.Row(c.Key("restore")).Grow(1).FillWidth().MinHeight(0).Padding(4, panePadX, panePadX, panePadX).Gap(18).AlignItems(ui.Stretch).Children(func() {
		ui.Scroll(c.Key("bk-left")).Width(400).Shrink(0).Gap(14).Children(func() {
			ui.Column(c.Key("drop")).FillWidth().Gap(8).Padding(16).Radius(cardRadius).Border(1, t.Border.Alpha(0.9)).Background(t.Surface).Children(func() {
				ui.Text(c, L.BkDropTitle()).Bold()
				small(c, L.BkDropHelp())
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c.Key("pick-file"), L.BkChooseFile()).Clicked() {
						a.pickBackup(false)
					}
					if ui.Button(c.Key("pick-folder"), L.BkChooseFolder()).Clicked() {
						a.pickBackup(true)
					}
				})
			})
			for i, e := range p.scanErrs {
				callout(c, "se"+itoa(i), toneWarn, iconAlert, e, nil)
			}
			card(c, "", func() {
				small(c, L.BkWhat()).Bold()
				if p.loaded && len(p.groups) == 0 {
					small(c, L.BkNone())
				}
				for i, g := range p.groups {
					a.groupRow(c, i, g)
				}
			})
			card(c, "", func() {
				ui.Row(c).FillWidth().Children(func() {
					small(c, L.BkLocations()).Bold().Grow(1)
					b := ui.ButtonBase(c.Key("loc-add")).Label(L.BkAdd())
					b.Children(func() { ui.Text(c, L.BkAdd()).FontSize(13).FontWeight(600).TextColor(t.Accent) })
					if b.Clicked() {
						a.pickBackup(true)
					}
				})
				for _, l := range p.locations {
					a.locationRow(c, l)
				}
			})
		})
		ui.Scroll(c.Key("bk-right")).Grow(1).MinWidth(0).Gap(14).Children(func() {
			if p.gsel >= 0 && p.gsel < len(p.groups) {
				a.versions(c, p.groups[p.gsel])
			} else {
				card(c, "", func() { emptyState(c, iconHistory, L.BkPick(), L.BkRestoreHelp(), nil) })
			}
		})
	})
}

// groupRow is one restorable source in the list.
func (a *App) groupRow(c *ui.Context, i int, g backup.Group) {
	p := &a.bk
	t := c.Theme()
	b := ui.ButtonBase(c.Key("grp-"+itoa(i))).FillWidth().Radius(8).Padding(8, 10).Label(groupTitle(g))
	switch {
	case p.gsel == i:
		b.Background(t.Accent.Alpha(0.1))
	case b.Hovered():
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, groupTitle(g)).FontWeight(600).SingleLine()
				it := g.Items[0]
				switch {
				case it.Note == backup.NoteEncrypted:
					pill(c, L.BkLocked(), toneWarn)
				case it.Kind == backup.KindPlainTar:
					pill(c, L.BkKindTar(), toneNeutral)
				case it.Source.Host != "" && !strings.EqualFold(it.Source.Host, a.k.Backup.Host):
					pill(c, L.BkFromOtherPC(), toneNeutral)
				}
			})
			small(c, L.BkGroupLine(len(g.Items), g.Items[0].Time.Local().Format("2006-01-02 15:04"))).SingleLine()
		})
	})
	if b.Clicked() {
		p.gsel, p.vsel = i, 0
	}
}

// locationRow is one backup location.
func (a *App) locationRow(c *ui.Context, l backup.Location) {
	ui.Row(c.Key("loc-" + l.Path)).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		label := l.Path
		if l.Label != "" {
			label = l.Label
		}
		ui.Text(c, label).FontSize(13).SingleLine().Grow(1).Tooltip(l.Path)
		if l.ReadOnly {
			small(c, L.BkReadOnly())
		}
		if l.Label == "" {
			path := l.Path
			ui.Button(c.Key("loc-menu-"+path), "…").Label(L.More()).Menu(func(m *ui.Menu) {
				if l.ReadOnly && m.Item(L.BkTakeOver()).Chosen() {
					a.setLocation(path, false, false)
				}
				if m.Item(L.BkForget()).Chosen() {
					a.setLocation(path, false, true)
				}
			})
		}
	})
}

// versions is the chosen source's versions and the chosen version's
// restore card.
func (a *App) versions(c *ui.Context, g backup.Group) {
	p := &a.bk
	t := c.Theme()
	card(c, L.BkVersionsOf(groupTitle(g)), func() {
		cols := []ui.TableColumn{{Title: L.BkWhen(), Width: 150}, {Title: L.BkKind(), Width: 120}, {Title: L.BkWhere()}, {Title: L.ColDisk(), Width: 90, Align: ui.End}, {Title: L.BkFrom()}, {Title: L.BkCheck(), Width: 80}}
		ui.Table(c, &p.vl, cols, len(g.Items), func(row, col int) {
			it := g.Items[row]
			// The whole cell takes the right-click menu.
			cell := ui.Row(c.Key("cell")).FillWidth().FillHeight().AlignItems(ui.Center).ContextMenu(a.versionMenu(row, it))
			if col == 3 {
				cell.Justify(ui.End)
			}
			cell.Children(func() {
				switch col {
				case 0:
					ui.Text(c, it.Time.Local().Format("2006-01-02 15:04")).FontWeight(600)
				case 1:
					if it.Kind == backup.KindRepo && it.Full {
						ui.Text(c, L.BkKindFull()).Tooltip(L.BkKindFullHelp())
					} else {
						ui.Text(c, kindLabel(it.Kind))
					}
				case 2:
					ui.Text(c, filepath.Base(filepath.Dir(it.Path))+`\`+filepath.Base(it.Path)).SingleLine()
				case 3:
					if it.Size > 0 {
						ui.Text(c, jobs.FormatBytes(it.Size))
					}
				case 4:
					from := a.planName(it.Plan)
					if it.Plan == "" {
						from = L.BkManual()
					}
					if !it.Consistent {
						from += " · " + L.BkInconsistent()
					}
					ui.Text(c, from).SingleLine()
				case 5:
					switch v := p.verified[itemKey(it)]; v {
					case "":
					case "ok":
						ui.Text(c, L.BkCheckOK()).TextColor(toneColor(t, selTone(toneGood, row == p.vsel)))
					default:
						ui.Text(c, L.BkCheckBad()).TextColor(toneColor(t, selTone(toneBad, row == p.vsel)))
					}
				}
			})
		}).FillWidth().Height(min(float32(len(g.Items)+1)*tableRowH+8, 300))
	})
	if p.vsel < 0 || p.vsel >= len(g.Items) {
		return
	}
	it := g.Items[p.vsel]
	card(c, L.BkRestoreFrom(groupTitle(g), it.Time.Local().Format("2006-01-02 15:04")), func() {
		step := func(n int, title, about string, k tone) {
			ui.Row(c.Key("step-" + itoa(n))).FillWidth().Gap(12).AlignItems(ui.Start).Children(func() {
				ui.Box(c).Size(24, 24).Radius(12).Center().Background(toneColor(t, k).Alpha(0.14)).Children(func() {
					ui.Text(c, itoa(n)).FontSize(sizeSmall).Bold().TextColor(toneColor(t, k))
				})
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, title).FontWeight(600)
					if about != "" {
						small(c, about)
					}
				})
			})
		}
		if it.Note == backup.NoteEncrypted {
			step(1, L.BkLocked(), L.BkNoteEncrypted(), toneWarn)
			if ui.PrimaryButton(c.Key("unlock"), L.BkUnlock()).Clicked() {
				p.rin, p.dlg = restoreInput{mode: "unlock", dir: it.Path}, "restore"
			}
			return
		}
		switch v := p.verified[itemKey(it)]; v {
		case "ok":
			step(1, L.BkStepChecked(), it.Path, toneGood)
		case "":
			step(1, L.BkStepCheck(), L.BkStepCheckAbout(), toneAccent)
		default:
			step(1, L.BkCheckBad(), v, toneBad)
		}
		if it.Note != "" {
			callout(c, "note", toneWarn, iconInfo, noteText(it.Note), nil)
		}
		if it.Source.Kind == backup.SourceCompose {
			a.projectContents(c, it)
		}
		step(2, L.BkStepRestoreAs(), "", toneAccent)
		ui.Column(c).FillWidth().Gap(8).Padding(0, 0, 0, 36).Children(func() {
			actions(c, func() {
				switch it.Source.Kind {
				case backup.SourceDistro, backup.SourceUnknown:
					if ui.PrimaryButton(c.Key("as-distro"), L.BkRestoreNewDistro()).Clicked() {
						p.rin, p.dlg = restoreInput{mode: "distro"}, "restore"
					}
					if it.Kind != backup.KindVHDX && ui.Button(c.Key("files"), L.BkRestoreFiles()).Clicked() {
						p.rin, p.dlg = restoreInput{mode: "files", dir: "/"}, "restore"
					}
				}
				if it.Kind != backup.KindVHDX && it.Source.Kind != backup.SourceCompose && it.Source.Kind != backup.SourceDistro {
					if ui.PrimaryButton(c.Key("as-volume"), L.BkRestoreNewVolume()).Clicked() {
						p.rin, p.dlg = restoreInput{mode: "volume"}, "restore"
					}
				}
				if it.Source.Kind == backup.SourceCompose {
					a.projectRestoreActions(c, it)
				}
				if it.Source.Name != "" && (it.Source.Kind == backup.SourceDistro || it.Source.Kind == backup.SourceVolume) {
					mode := "over-distro"
					if it.Source.Kind == backup.SourceVolume {
						mode = "over-volume"
					}
					if dangerButton(c, L.BkRestoreOver(it.Source.Name)).Clicked() {
						p.rin, p.dlg = restoreInput{mode: mode, name: it.Source.Name}, "restore"
					}
				}
			})
		})
		ui.Box(c).Height(1).FillWidth().Background(t.Border)
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			small(c, it.Path).Grow(1).SingleLine().Tooltip(it.Path)
			if ui.Button(c.Key("verify"), L.BkVerify()).Clicked() {
				a.verifyItem(it)
			}
			if a.canDelete(it) && dangerButton(c, L.Delete()).Clicked() {
				a.deleteVersion(it)
			}
		})
	})
}

// plansView is the plans table and the chosen plan's card.
func (a *App) plansView(c *ui.Context) {
	p := &a.bk
	t := c.Theme()
	body(c, func() {
		if a.settings.agentChecked && !a.settings.agentInstalled {
			callout(c, "agent", toneWarn, iconAlert, L.BkAgentMissing(), func() {
				if ui.Button(c.Key("agent-install"), L.SetInstallAgent()).Clicked() {
					a.installAgent()
				}
			})
		}
		if len(p.plans) == 0 {
			emptyState(c, iconHistory, L.DashNoPlans(), L.BkPlansHelp(), func() {
				if ui.PrimaryButton(c.Key("new-empty"), L.BkNewPlan()).Clicked() {
					a.editPlan(nil)
				}
			})
			return
		}
		hasDetail := p.psel >= 0 && p.psel < len(p.plans)
		tableBox(c, "plans", !hasDetail, func() {
			cols := []ui.TableColumn{{Title: L.BkPlan()}, {Title: L.BkKind(), Width: 140}, {Title: L.BkSchedule(), Width: 170}, {Title: L.BkLast(), Width: 200}, {Title: L.BkNext(), Width: 150}}
			tb := ui.Table(c, &p.pl, cols, len(p.plans), func(row, col int) {
				pl := p.plans[row]
				st := p.states[pl.ID]
				switch col {
				case 0:
					ui.Column(c).Gap(1).Children(func() {
						name := pl.Name
						if !pl.Enabled {
							name += " (" + L.BkPaused() + ")"
						}
						ui.Text(c, name).FontWeight(600).SingleLine()
						small(c, planTargets(pl)).SingleLine()
					})
				case 1:
					ui.Text(c, kindLabel(pl.Destination.Kind))
				case 2:
					ui.Text(c, scheduleText(pl.Schedule)).SingleLine()
				case 3:
					switch {
					case st.LastResult == "ok":
						ui.Text(c, st.LastRun.Local().Format("01-02 15:04")).FontWeight(600).TextColor(toneColor(t, selTone(toneGood, row == p.psel)))
					case st.LastResult != "":
						ui.Text(c, L.JobStateFailed()+" · "+friendlyError(errors.New(st.LastResult))).FontWeight(600).TextColor(toneColor(t, selTone(toneBad, row == p.psel))).SingleLine()
					default:
						ui.Text(c, "—")
					}
				case 4:
					if pl.Enabled && !st.NextRun.IsZero() {
						ui.Text(c, st.NextRun.Local().Format("01-02 15:04"))
					}
				}
			})
			if hasDetail {
				tb.Height(min(float32(len(p.plans)+1)*54+8, 300))
			} else {
				tb.Grow(1)
			}
		})
		if hasDetail {
			a.planDetail(c, p.plans[p.psel])
		}
	})
}

// planTargets lists a plan's targets for people.
func planTargets(pl scheduler.Plan) string {
	var ts []string
	for _, t := range pl.Targets {
		ts = append(ts, groupTitle(backup.Group{Source: backup.Source{Kind: t.Kind, Name: t.Name}, Items: []backup.Item{{}}}))
	}
	return strings.Join(ts, ", ")
}

// planDetail is the chosen plan as rows, with its actions and history.
func (a *App) planDetail(c *ui.Context, pl scheduler.Plan) {
	p := &a.bk
	ui.Scroll(c.Key("plan-detail")).Grow(1).MinHeight(0).FillWidth().Children(func() {
		card(c, "", func() {
			cardHead(c, pl.Name, func() {
				if ui.PrimaryButton(c.Key("run"), L.BkRunNow()).Clicked() {
					plan := pl
					a.startJob(L.BkRunNow()+" "+plan.Name, func(ctx context.Context, j *jobs.Job) error {
						r := a.k.Scheduler.Run(ctx, plan)
						if r.Result != "ok" {
							return errors.New(r.Result)
						}
						return nil
					}, func() { p.plansLoaded, p.loaded = false, false; p.history = nil })
				}
				if ui.Button(c.Key("edit"), L.Edit()).Clicked() {
					a.editPlan(&pl)
				}
				if dangerButton(c, L.Delete()).Clicked() {
					a.ask(L.BkDeletePlanTitle(pl.Name), L.BkDeletePlanMessage(), L.Delete(), func(bool) {
						id := pl.ID
						a.async(func(ctx context.Context) func() {
							err := a.k.Scheduler.Delete(id)
							return func() {
								if err != nil {
									a.notify(friendlyError(err), true)
								}
								p.plansLoaded, p.psel = false, -1
							}
						})
					})
				}
			})
			ui.Column(c.Key("plan-rows")).FillWidth().Gap(2).Children(func() {
				planRow(c, L.BkTargets(), planTargets(pl))
				planRow(c, L.BkDestination(), pl.Destination.Path)
				planRow(c, L.BkKind(), kindLabel(pl.Destination.Kind))
				planRow(c, L.BkSchedule(), scheduleText(pl.Schedule))
				r := pl.Retention
				planRow(c, L.BkRetention(), L.BkRetentionText(r.KeepLast, r.KeepDaily, r.KeepWeekly, r.KeepMonthly))
				planRow(c, L.BkRetries(), itoa(pl.Retry.Max))
				cons := L.BkStopFirst()
				if pl.Live {
					cons = L.BkLive()
				}
				planRow(c, L.BkConsistency(), cons)
			})
			small(c, L.BkRunsClosed())
		})
		ui.Box(c).Height(12)
		card(c, L.BkHistory(), func() {
			if p.history == nil {
				id := pl.ID
				p.history = []scheduler.Record{}
				a.async(func(ctx context.Context) func() {
					h := a.k.Scheduler.History(id, 30)
					return func() { p.history = h }
				})
			}
			if len(p.history) == 0 {
				small(c, L.BkNoHistory())
			}
			for i, r := range p.history {
				ui.Row(c.Key("h" + itoa(i))).FillWidth().Gap(12).Children(func() {
					small(c, r.Start.Local().Format("2006-01-02 15:04")).Width(130).Shrink(0)
					if r.Result == "ok" {
						ui.Text(c, L.BkHistoryOK(len(r.Items), len(r.Expired))).FontSize(13).TextColor(c.Theme().Success)
					} else {
						ui.Text(c, friendlyError(errors.New(r.Result))).FontSize(13).TextColor(c.Theme().Danger)
					}
				})
			}
		})
	})
}

// planRow is one label and value of the plan card: the label in a fixed
// column, the value beside it.
func planRow(c *ui.Context, label, value string) {
	ui.Row(c.Key("pr-" + label)).FillWidth().MinHeight(32).Gap(16).AlignItems(ui.Center).Children(func() {
		muted(c, label).Width(160).Shrink(0)
		ui.Text(c, value).Grow(1).MinWidth(0).SingleLine().Tooltip(value).Selectable()
	})
}

// projectManifest returns a project backup's manifest, reading it in the
// background the first time. done is false while it is being read; a
// version 1 backup is done with a nil manifest.
func (a *App) projectManifest(it backup.Item) (m *backup.ProjectManifest, done bool) {
	p := &a.bk
	key := itemKey(it)
	if p.manifestDone[key] {
		return p.manifests[key], true
	}
	if p.manifests == nil {
		p.manifests, p.manifestDone = map[string]*backup.ProjectManifest{}, map[string]bool{}
	}
	if _, reading := p.manifests[key]; !reading {
		p.manifests[key] = nil
		a.async(func(ctx context.Context) func() {
			m, err := backup.ReadProjectManifest(ctx, it)
			return func() {
				if err != nil {
					a.notify(friendlyError(err), true)
				}
				p.manifests[key], p.manifestDone[key] = m, true
			}
		})
	}
	return nil, false
}

// projectContents lists what a project backup holds, with the same names
// and types as the Compose page's Data card.
func (a *App) projectContents(c *ui.Context, it backup.Item) {
	m, done := a.projectManifest(it)
	ui.Column(c.Key("contents")).FillWidth().Gap(2).Padding(0, 0, 0, 36).Children(func() {
		if !done {
			ui.Spinner(c).Size(16, 16)
			return
		}
		if m == nil {
			small(c, L.BkProjectV1())
			return
		}
		dataRow(c, "files", rowFiles, strings.Join(m.Files, ", "), L.BkProjectFiles(), nil)
		for _, f := range m.Folders {
			name := f.Source
			if f.Relative != "" {
				name = "./" + f.Relative
			}
			dataRow(c, "f-"+f.ID, rowFolder, name, f.Source, nil)
		}
		for _, v := range m.Volumes {
			dataRow(c, "v-"+v.Name, rowVolume, v.Name, L.BkProjectVolumeLine(v.Key), nil)
		}
		if len(m.External) > 0 {
			small(c, L.BkProjectExternal(strings.Join(m.External, ", ")))
		}
	})
}

// projectRestoreActions are the restore buttons of a project backup: the
// whole project, or one of its volumes.
func (a *App) projectRestoreActions(c *ui.Context, it backup.Item) {
	p := &a.bk
	m, done := a.projectManifest(it)
	if !done {
		return
	}
	choices := func(only string) []volChoice {
		var vs []volChoice
		if m != nil {
			for _, v := range m.Volumes {
				vs = append(vs, volChoice{from: v.Name, key: v.Key, into: v.Name, on: only == "" || only == v.Name})
			}
		}
		return vs
	}
	if ui.PrimaryButton(c.Key("project"), L.BkRestoreProject()).Clicked() {
		p.rin, p.dlg = restoreInput{mode: "project", vols: choices("")}, "restore"
		a.loadVolumes()
	}
	if m == nil {
		return
	}
	for _, v := range m.Volumes {
		if ui.Button(c.Key("pv-"+v.Name), L.BkRestoreVolumeNamed(v.Name)).Clicked() {
			p.rin, p.dlg = restoreInput{mode: "project", vols: choices(v.Name)}, "restore"
			a.loadVolumes()
		}
	}
}

// canDelete reports whether a version can be deleted here: an archive, a
// folder backup or a repository snapshot, in a location of this PC.
func (a *App) canDelete(it backup.Item) bool {
	if a.bk.locationOf(it).ReadOnly {
		return false
	}
	switch it.Kind {
	case backup.KindArchive, backup.KindFolder:
		return true
	case backup.KindRepo:
		return it.ID != ""
	}
	return false
}

// repoVersions counts the snapshots the catalog shows for a repository.
func (a *App) repoVersions(dir string) int {
	n := 0
	for _, g := range a.bk.groups {
		for _, it := range g.Items {
			if it.Kind == backup.KindRepo && it.ID != "" && strings.EqualFold(it.Path, dir) {
				n++
			}
		}
	}
	return n
}

// deleteVersion confirms and deletes one version. In a repository the
// version is forgotten and its data merged into the versions after it;
// the last version takes the repository with it.
func (a *App) deleteVersion(it backup.Item) {
	p := &a.bk
	loc := p.locationOf(it)
	msg := L.BkDeleteMessage()
	if it.Kind == backup.KindRepo {
		msg = L.BkDeleteRepoMessage()
		if a.repoVersions(it.Path) <= 1 {
			msg = L.BkDeleteRepoLast(it.Path)
		}
	}
	a.ask(L.BkDeleteTitle(), msg, L.Delete(), func(bool) {
		a.startJob(L.BkDeleteJob(it.Time.Local().Format("2006-01-02 15:04")), func(ctx context.Context, j *jobs.Job) error {
			err := backup.DeleteVersion(ctx, loc, it)
			a.k.Jobs.Audit("backup.delete", it.Path, it.ID, err)
			return err
		}, func() {
			p.vsel = -1
			p.loaded = false
			a.loadCatalog()
		})
	})
}

// versionMenu is the right-click menu of a version: check, restore, delete.
func (a *App) versionMenu(row int, it backup.Item) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item(L.BkVerify()).Chosen() {
			a.bk.vsel = row
			a.verifyItem(it)
		}
		if m.Item(L.Delete() + "…").Disabled(!a.canDelete(it)).Chosen() {
			a.bk.vsel = row
			a.deleteVersion(it)
		}
	}
}
