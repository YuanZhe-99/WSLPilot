package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/compose"
	"github.com/YuanZhe-99/WSLPilot/internal/elevate"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/vhdx"
	"github.com/YuanZhe-99/WSLPilot/internal/volume"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// volumesPage is the state of the Volumes page.
type volumesPage struct {
	loaded, loading bool
	err             error
	list            []wslc.Volume
	users           map[string][]string // volume → container names
	sizes           map[string]int64    // vhd volume → VHDX file size
	ls              ui.ListState
	sel             int
	// focus is a volume to select once the list loads (from the Compose page).
	focus string

	dlg string
	in  volInput
	// imp is the inline import panel; impSize is the size of the file
	// impSizeOf names, read off the view.
	imp       volInput
	impSize   int64
	impSizeOf string
}

// volInput holds the volume dialog fields.
type volInput struct {
	name, driver, size, file, target string
	newVolume, replace, check        bool
}

func (a *App) loadVolumes() {
	p := &a.vol
	if p.loading {
		return
	}
	p.loading = true
	a.async(func(ctx context.Context) func() {
		vs, err := a.k.WSLc.Volumes(ctx)
		users := map[string][]string{}
		if cs, cerr := a.k.WSLc.ContainersDetailed(ctx); cerr == nil {
			for _, c := range cs {
				if c.Labels[volume.HelperLabel] == "1" {
					continue
				}
				for _, m := range c.Mounts {
					if m.Type == "volume" {
						users[m.Name] = append(users[m.Name], c.DisplayName())
					}
				}
			}
		}
		sizes := map[string]int64{}
		for _, v := range vs {
			if v.Status.HostPath != "" {
				if st, err := os.Stat(v.Status.HostPath); err == nil {
					sizes[v.Name] = st.Size()
				}
			}
		}
		return func() {
			p.loading, p.loaded, p.err, p.list, p.users, p.sizes = false, true, err, vs, users, sizes
			if p.focus != "" {
				for i, v := range vs {
					if v.Name == p.focus {
						p.sel = i
					}
				}
				p.focus = ""
			}
		}
	})
}

// volumesView draws the Volumes page as the design: the table with the
// chosen volume's actions under it, and the TAR import beside it.
func (a *App) volumesView(c *ui.Context) {
	p := &a.vol
	if !p.loaded && !p.loading {
		a.loadVolumes()
	}
	if !a.cmp.loaded && !a.cmp.loading && a.k.Compose != nil {
		a.loadProjects()
	}
	a.head(c, paneHead{icon: iconDatabase, title: L.NavVolumes(), sub: L.VolSubtitle(), actions: func() {
		iconButtonAction(c, "refresh", iconRefresh, L.Refresh(), a.loadVolumes)
		unused := a.unusedVolumes()
		if ui.Button(c.Key("prune"), L.VolRemoveUnused()).Disabled(len(unused) == 0).Clicked() {
			a.removeVolumes(unused)
		}
		if ui.PrimaryButton(c.Key("new"), L.VolNew()).Clicked() {
			p.in, p.dlg = volInput{driver: "guest"}, "create"
		}
	}})
	if a.report != nil && !a.report.HasWSLc() {
		body(c, func() { emptyState(c, iconDatabase, L.DashNotFound(), L.DashWSLcHint(), nil) })
		return
	}
	ui.Row(c.Key("vol")).Grow(1).FillWidth().MinHeight(0).Padding(4, panePadX, panePadX, panePadX).Gap(18).AlignItems(ui.Start).Children(func() {
		ui.Column(c.Key("vol-left")).Grow(1).MinWidth(0).Gap(12).Children(func() {
			if p.err != nil {
				callout(c, "err", toneBad, iconAlert, friendlyError(p.err), nil)
			}
			if p.loaded && len(p.list) == 0 {
				card(c, "", func() { emptyState(c, iconDatabase, L.VolNone(), L.VolHelp(), nil) })
				return
			}
			tableBox(c, "vols", false, func() {
				cols := []ui.TableColumn{{Title: L.ColName()}, {Title: L.NetDriver(), Width: 80}, {Title: L.VolSize(), Width: 90}, {Title: L.VolUsedBy()}}
				ui.Table(c, &p.ls, cols, len(p.list), func(row, col int) {
					v := p.list[row]
					switch col {
					case 0:
						ui.Text(c, v.Name).FontWeight(600).SingleLine()
					case 1:
						ui.Text(c, v.Driver)
					case 2:
						if n := p.sizes[v.Name]; n > 0 {
							ui.Text(c, jobs.FormatBytes(n))
						} else {
							ui.Text(c, "—")
						}
					case 3:
						a.volumeUsers(c, v, row == p.sel)
					}
				}).FillWidth().Height(min(float32(len(p.list)+1)*44+8, 420))
				ui.Box(c).Height(1).FillWidth().Background(c.Theme().Border)
				ui.Column(c).FillWidth().Gap(8).Padding(12).Children(func() { a.volumeActions(c) })
			})
		})
		ui.Column(c.Key("vol-import")).Width(450).Shrink(0).Children(func() { a.importCard(c) })
	})
	a.volumeDialogs(c)
}

// unusedVolumes lists the volumes no container uses and no added Compose
// project owns: a project taken down has no containers, but its volumes
// still hold its data.
func (a *App) unusedVolumes() []string {
	p := &a.vol
	var ns []string
	for _, v := range p.list {
		if len(p.users[v.Name]) == 0 && a.projectIndex(v.Labels[compose.LabelProject]) < 0 {
			ns = append(ns, v.Name)
		}
	}
	return ns
}

// volumeActions are the chosen volume's buttons under the table.
func (a *App) volumeActions(c *ui.Context) {
	p := &a.vol
	has := p.sel >= 0 && p.sel < len(p.list)
	var v wslc.Volume
	if has {
		v = p.list[p.sel]
	}
	actions(c, func() {
		if ui.Button(c.Key("export"), L.VolExport()).Disabled(!has).Clicked() {
			p.in, p.dlg = volInput{name: v.Name}, "export"
		}
		if ui.Button(c.Key("backup"), L.VolBackUp()).Disabled(!has).Clicked() {
			name := v.Name
			a.startJob(L.WSLBackupNow()+" "+name, func(ctx context.Context, j *jobs.Job) error {
				_, err := a.k.Backup.BackupVolume(ctx, name, &backup.RepoWriter{Dir: filepath.Join(a.backupDir(), "Repository")}, backupOptions())
				return err
			}, func() { a.bk.loaded = false; a.loadVolumes() })
		}
		if has && v.Driver == "vhd" && v.Status.HostPath != "" && ui.Button(c.Key("compact"), L.WSLCompact()).Clicked() {
			a.compactVolume(v)
		}
		if dangerButton(c, L.VolDelete()).Disabled(!has).Clicked() {
			a.removeVolumes([]string{v.Name})
		}
	})
	if has {
		if i := a.projectIndex(v.Labels[compose.LabelProject]); i >= 0 {
			name := a.cmp.projects[i].Name
			callout(c, "of-project", toneAccent, iconInfo, L.VolOfProject(name), func() {
				if ui.Button(c.Key("backup-project"), L.CmpBackUp()).Clicked() {
					a.openProject(i)
					a.cmp.in, a.cmp.dlg = composeInput{}, "backup"
				}
			})
		}
	}
	if !has {
		small(c, L.VolPickHint())
	} else if v.Status.HostPath != "" {
		small(c, v.Status.HostPath).Selectable()
	}
}

// removeVolumes confirms and removes volumes, backing each up first unless
// the person turns that off (behavior contract).
func (a *App) removeVolumes(names []string) {
	title, typeName := L.VolRemoveUnusedTitle(len(names)), ""
	msg := L.VolRemoveUnusedMessage(strings.Join(names, ", "))
	if len(names) == 1 {
		title, msg, typeName = L.VolRemoveTitle(names[0]), L.VolRemoveMessage(), names[0]
	}
	a.askDanger(title, msg, L.Delete(), typeName, func(backupFirst bool) {
		a.startJob(L.VolRemove()+" "+strings.Join(names, ", "), func(ctx context.Context, j *jobs.Job) error {
			for _, name := range names {
				if backupFirst {
					j.SetProgress(-1, L.JobBackingUp()+" "+name)
					if _, err := a.k.Backup.BackupVolume(ctx, name, a.safetyWriter(), backupOptions()); err != nil {
						return fmt.Errorf("%s: %w", L.JobBackupFailedStop(), err)
					}
				}
				err := a.k.WSLc.RemoveVolumes(ctx, name)
				a.k.Jobs.Audit("wslc.volume.remove", name, "", err)
				if err != nil {
					return err
				}
			}
			return nil
		}, func() { a.vol.sel = -1; a.loadVolumes() })
	})
}

// compactVolume confirms and compacts a vhd volume's disk.
func (a *App) compactVolume(v wslc.Volume) {
	path, name := v.Status.HostPath, v.Name
	a.ask(L.WSLCompactTitle(name), L.VolCompactMessage(), L.WSLCompact(), func(bool) {
		a.startJob(L.WSLCompact()+" "+name, func(ctx context.Context, j *jobs.Job) error {
			j.SetProgress(-1, L.VolStoppingSession())
			if err := a.k.WSLc.TerminateSession(ctx); err != nil {
				return err
			}
			time.Sleep(2 * time.Second)
			j.SetProgress(-1, L.JobElevated())
			r, err := vhdx.Compact(ctx, elevate.Run, path, j.Logf)
			if err == nil {
				j.Logf(L.WSLCompacted(jobs.FormatBytes(r.Before), jobs.FormatBytes(r.After)))
			}
			a.k.Jobs.Audit("wslc.volume.compact", name, "", err)
			return err
		}, a.loadVolumes)
	})
}

// importCard is the inline "Import a TAR archive" panel.
func (a *App) importCard(c *ui.Context) {
	p := &a.vol
	in := &p.imp
	t := c.Theme()
	card(c, L.VolImportTitle(), func() {
		ui.Text(c, L.VolArchive()).FontWeight(600)
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.TextInput(c.Key("imp-file"), &in.file).Label(L.VolArchive()).Placeholder(L.VolTarFile()).Grow(1)
			if ui.Button(c.Key("imp-browse"), L.Browse()).Clicked() {
				go func() {
					ps := a.host.PickFiles(L.VolArchive(), false, "tar", "gz", "tgz", "zst")
					if len(ps) > 0 {
						a.update(func() { in.file = ps[0] })
					}
				}()
			}
		})
		if p.impSize > 0 && p.impSizeOf == in.file {
			small(c, L.VolArchiveSize(jobs.FormatBytes(p.impSize)))
		} else if in.file != "" && p.impSizeOf != in.file {
			file := in.file
			p.impSizeOf, p.impSize = file, 0
			go func() {
				if st, err := os.Stat(file); err == nil {
					a.update(func() {
						if p.impSizeOf == file {
							p.impSize = st.Size()
						}
					})
				}
			}()
		}
		ui.Text(c, L.VolTarget()).FontWeight(600)
		ui.RadioGroup(c.Key("imp-into"), func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Radio(c.Key("imp-new"), &in.newVolume, true, L.VolIntoNew())
				if in.newVolume {
					ui.TextInput(c.Key("imp-name"), &in.name).Label(L.ColName()).Placeholder(L.ColName()).Width(170)
				}
			})
			ui.Radio(c.Key("imp-over"), &in.newVolume, false, L.VolIntoExisting()).Disabled(len(p.list) == 0)
		})
		if in.newVolume {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				small(c, L.NetDriver())
				ui.Select(c.Key("imp-driver"), &in.driver, []string{"guest", "vhd"}).Label(L.NetDriver()).Width(110)
				if in.driver == "vhd" {
					ui.TextInput(c.Key("imp-size"), &in.size).Placeholder("10GB").Label(L.VolSize()).Width(110)
				}
			})
		} else {
			names := make([]string, len(p.list))
			for i, v := range p.list {
				names[i] = v.Name
			}
			ui.Select(c.Key("imp-target"), &in.target, names).Label(L.VolTarget()).FillWidth()
			ui.Text(c, L.VolReplaceWarning()).FontSize(13).TextColor(t.Warning)
		}
		ui.Column(c).FillWidth().Gap(4).Padding(12).Radius(8).Background(t.Accent.Alpha(0.06)).Children(func() {
			ui.Text(c, L.VolKeptTitle()).FontSize(13).Bold()
			ui.Text(c, L.VolKept()).FontSize(13)
			small(c, L.VolImportHelp())
		})
		ui.Checkbox(c.Key("imp-check"), &in.check, L.VolCheckAfter())
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c.Key("imp-cancel"), L.Cancel()).Disabled(in.file == "").Clicked() {
				p.imp = volInput{newVolume: true, driver: "guest", check: true}
			}
			ok := in.file != "" && ((in.newVolume && volumeInputOK(*in)) || (!in.newVolume && in.target != ""))
			if ui.PrimaryButton(c.Key("imp-go"), L.VolImportGo()).Disabled(!ok).Clicked() {
				job := *in
				if job.newVolume {
					job.target = ""
				} else {
					job.replace = true
				}
				a.importVolume(job)
				p.imp = volInput{newVolume: true, driver: "guest", check: true}
			}
		})
	})
}

func (a *App) volumeDialogs(c *ui.Context) {
	p := &a.vol
	if p.dlg == "" {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(dialogWidth).Gap(gap).Children(func() {
			switch p.dlg {
			case "create":
				ui.Text(c, L.VolCreate()).FontSize(16).Bold()
				volumeFields(c, &p.in)
				dialogButtons(c, a, L.VolCreate(), volumeInputOK(p.in), func() {
					in := p.in
					a.startJob(L.VolCreate()+" "+in.name, func(ctx context.Context, j *jobs.Job) error {
						opts := map[string]string{}
						if in.driver == "vhd" {
							opts["SizeBytes"] = strconv.FormatInt(parseSize(in.size), 10)
						}
						return a.k.WSLc.CreateVolume(ctx, in.name, in.driver, opts, nil)
					}, a.loadVolumes)
				})
			case "export":
				ui.Text(c, L.VolExport()+" "+p.in.name).FontSize(16).Bold()
				muted(c, L.VolExportHelp())
				pathField(c, a, L.WSLExportTo(), &p.in.file, false)
				dialogButtons(c, a, L.VolExport(), p.in.file != "", func() {
					in := p.in
					a.startJob(L.VolExport()+" "+in.name, func(ctx context.Context, j *jobs.Job) error {
						f, err := os.Create(in.file)
						if err != nil {
							return err
						}
						st, err := a.k.Volumes.Export(ctx, in.name, &jobs.ProgressWriter{Job: j, Writer: f, Step: L.VolExport()})
						if cerr := f.Close(); err == nil {
							err = cerr
						}
						if err != nil {
							os.Remove(in.file)
							return err
						}
						j.Logf(L.VolEntries(int(st.Entries), jobs.FormatBytes(st.Bytes)))
						return nil
					}, nil)
				})
			}
		})
	})
	if !open {
		p.dlg = ""
	}
}

// importVolume runs a TAR import, with a safety backup before replacing.
func (a *App) importVolume(in volInput) {
	title := L.VolImport() + " " + in.name
	a.startJob(title, func(ctx context.Context, j *jobs.Job) error {
		f, err := os.Open(in.file)
		if err != nil {
			return err
		}
		defer f.Close()
		size := int64(0)
		if st, err := f.Stat(); err == nil {
			size = st.Size()
		}
		o := volume.ImportOptions{Labels: map[string]string{}}
		name := in.target
		if name == "" {
			name, o.Create, o.Driver, o.Size = in.name, true, in.driver, parseSize(in.size)
		} else if in.replace {
			j.SetProgress(-1, L.JobBackingUp())
			if _, err := a.k.Backup.BackupVolume(ctx, name, a.safetyWriter(), backupOptions()); err != nil {
				return fmt.Errorf("%s: %w", L.JobBackupFailedStop(), err)
			}
			o.Replace = true
		}
		st, found, err := a.k.Volumes.Import(ctx, name, &jobs.ProgressReader{Job: j, Reader: f, Total: size, Step: L.VolImport()}, o)
		if in.replace {
			a.k.Jobs.Audit("wslc.volume.replace", name, in.file, err)
		}
		if err != nil {
			return err
		}
		j.Logf(L.VolImported(int(st.Entries), found))
		// The archive lists "./" itself, which is not counted inside.
		if in.check && found < st.Entries-1 {
			return errors.New(L.VolCheckFailed(st.Entries, found))
		}
		return nil
	}, a.loadVolumes)
}

// volumeFields are the name/driver/size inputs.
func volumeFields(c *ui.Context, in *volInput) {
	ui.Form(c, func() {
		ui.Field(c, L.ColName(), func() { ui.TextInput(c, &in.name).Label(L.ColName()) })
		ui.Field(c, L.NetDriver(), func() { ui.Select(c, &in.driver, []string{"guest", "vhd"}).Label(L.NetDriver()) }).Description(L.VolDriverHelp())
		if in.driver == "vhd" {
			ui.Field(c, L.VolSize(), func() { ui.TextInput(c, &in.size).Placeholder("10GB").Label(L.VolSize()) })
		}
	})
}

func volumeInputOK(in volInput) bool {
	if strings.TrimSpace(in.name) == "" {
		return false
	}
	return in.driver != "vhd" || parseSize(in.size) > 0
}

// parseSize reads "10GB", "512M" or bytes (binary units, as wslc).
func parseSize(s string) int64 {
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1)
	for _, u := range []struct {
		suf string
		m   int64
	}{{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(s, u.suf) {
			s, mult = strings.TrimSpace(strings.TrimSuffix(s, u.suf)), u.m
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n * mult
}

// projectIndex is the index of the added Compose project named name, or -1.
func (a *App) projectIndex(name string) int {
	if name == "" {
		return -1
	}
	for i, pr := range a.cmp.projects {
		if pr.Name == name {
			return i
		}
	}
	return -1
}

// openProject shows a Compose project's page.
func (a *App) openProject(i int) {
	a.page = PageCompose
	a.cmp.sel = i
}

// volumeUsers is the Used by cell: the owning Compose project (a link to
// it) and the containers mounting the volume.
func (a *App) volumeUsers(c *ui.Context, v wslc.Volume, selected bool) {
	t := c.Theme()
	us := a.vol.users[v.Name]
	project := v.Labels[compose.LabelProject]
	if project == "" {
		if len(us) > 0 {
			ui.Text(c, strings.Join(us, ", ")).SingleLine()
		} else {
			ui.Text(c, L.VolNotInUse()).TextColor(t.TextMuted)
		}
		return
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
		i := a.projectIndex(project)
		label := L.VolProject(project)
		if i >= 0 {
			link := ui.ButtonBase(c.Key("proj-" + v.Name)).Label(label)
			link.Children(func() {
				col := toneColor(t, selTone(toneAccent, selected))
				ui.Text(c, label).TextColor(col).FontWeight(600).SingleLine()
			})
			if link.Clicked() {
				a.openProject(i)
			}
		} else {
			ui.Text(c, label+" · "+L.VolProjectNotAdded()).SingleLine()
		}
		rest := L.VolProjectStopped()
		if len(us) > 0 {
			rest = strings.Join(us, ", ")
		}
		ui.Text(c, "· "+rest).SingleLine().Shrink(1)
	})
}
