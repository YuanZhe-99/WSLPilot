package gui

import (
	"context"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/compose"
	"github.com/YuanZhe-99/WSLPilot/internal/core"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// A project's data is shown the same way on the Compose page, in its
// backup dialog and on the Volumes page: a Volume (WSL Containers) or a
// Folder (Windows), by name, with its size and the services using it.

// loadProjectData reads what a project mounts and backs up.
func (a *App) loadProjectData(pr *compose.Project) {
	id := pr.ID
	proj := snapshotProject(pr)
	a.async(func(ctx context.Context) func() {
		d, err := a.k.ProjectData(ctx, proj)
		return func() {
			if a.cmp.data == nil {
				a.cmp.data = map[string]*core.ProjectData{}
			}
			if err != nil {
				a.cmp.data[id] = nil
				return
			}
			a.cmp.data[id] = &d
			for _, f := range d.Folders {
				a.measureFolder(f.Source)
			}
		}
	})
}

// measureFolder sums a Windows folder's file sizes in the background.
func (a *App) measureFolder(path string) {
	p := &a.cmp
	if p.fsize == nil {
		p.fsize = map[string]int64{}
	}
	if _, ok := p.fsize[path]; ok {
		return
	}
	p.fsize[path] = -1
	a.async(func(ctx context.Context) func() {
		var n int64
		filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
			if err != nil || ctx.Err() != nil {
				return nil
			}
			if d.Type().IsRegular() {
				if info, err := d.Info(); err == nil {
					n += info.Size()
				}
			}
			return nil
		})
		return func() { p.fsize[path] = n }
	})
}

// sizeText is a measured size, "…" while measuring and "—" when unknown.
func sizeText(n int64, ok bool) string {
	switch {
	case !ok:
		return "—"
	case n < 0:
		return "…"
	}
	return jobs.FormatBytes(n)
}

// volumeSize is a volume's size as the Volumes page knows it.
func (a *App) volumeSize(name string) (int64, bool) {
	n, ok := a.vol.sizes[name]
	return n, ok && n > 0
}

// Kinds of data rows: project files, a Windows folder, a volume.
const (
	rowFiles = iota
	rowFolder
	rowVolume
)

// dataKind is the type label of a data location, as on the Volumes page;
// project files have none.
func dataKind(c *ui.Context, kind int) {
	switch kind {
	case rowFolder:
		pill(c, L.DataFolder(), toneNeutral)
	case rowVolume:
		pill(c, L.DataVolume(), toneAccent)
	}
}

// dataRow is one data location: an icon, its name and type, a line of
// size and services, and an action on the right.
func dataRow(c *ui.Context, key string, kind int, name, line string, action func()) {
	t := c.Theme()
	ui.Row(c.Key(key)).FillWidth().Gap(10).Padding(8, 0).AlignItems(ui.Center).Children(func() {
		icon := iconDatabase
		switch kind {
		case rowFolder:
			icon = iconFolder
		case rowFiles:
			icon = iconFile
		}
		ui.Icon(c, icon).Size(18, 18).TextColor(t.TextMuted)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).FontWeight(600).SingleLine().Shrink(1).Tooltip(name)
				dataKind(c, kind)
			})
			small(c, line).SingleLine()
		})
		if action != nil {
			action()
		}
	})
}

// openVolume shows a volume on the Volumes page, selected.
func (a *App) openVolume(name string) {
	a.page = PageVolumes
	a.vol.focus = name
	a.loadVolumes()
}

// dataCard lists where the project keeps its data. Named volumes can be
// moved between a volume and a folder ("Storage…").
func (a *App) dataCard(c *ui.Context, pr *compose.Project) {
	p := &a.cmp
	d := p.data[pr.ID]
	card(c, "", func() {
		cardHead(c, L.CmpData(), func() {
			if ui.Button(c.Key("backup"), L.CmpBackUp()).Clicked() {
				p.in, p.dlg = composeInput{}, "backup"
			}
		})
		small(c, L.CmpDataHelp())
		if d == nil {
			ui.Spinner(c).Size(16, 16)
			return
		}
		ui.Column(c).FillWidth().Children(func() {
			services := func(s []string) string {
				if len(s) == 0 {
					return L.DataUnused()
				}
				return L.DataUsedBy(strings.Join(s, ", "))
			}
			byPath := map[string]string{} // folder of a named volume → its key
			for key, ch := range pr.Volumes {
				if ch.Mode == compose.VolumeBind {
					byPath[strings.ToLower(filepath.Clean(ch.Path))] = key
				}
			}
			for _, v := range d.Volumes {
				size, ok := a.volumeSize(v.Name)
				dataRow(c, "v-"+v.Name, rowVolume, v.Name, sizeText(size, ok)+" · "+services(v.Services), func() {
					if ui.Button(c.Key("store-"+v.Key), L.DataStore()).Clicked() {
						a.openStore(pr, v.Key)
					}
					iconButtonAction(c, "open-"+v.Name, iconExternal, L.DataOpenVolume(), func() { a.openVolume(v.Name) })
				})
			}
			for _, f := range d.Folders {
				name := f.Source
				if f.Relative != "" {
					name = "./" + f.Relative
				}
				size, ok := p.fsize[f.Source]
				dataRow(c, "f-"+f.ID, rowFolder, name, sizeText(size, ok)+" · "+services(f.Services), func() {
					if key, ok := byPath[strings.ToLower(filepath.Clean(f.Source))]; ok {
						if ui.Button(c.Key("store-"+key), L.DataStore()).Clicked() {
							a.openStore(pr, key)
						}
					}
					src := f.Source
					if f.File {
						src = filepath.Dir(src)
					}
					iconButtonAction(c, "show-"+f.ID, iconExternal, L.ShowInExplorer(), func() { a.host.OpenPath(src) })
				})
			}
			for _, name := range d.External {
				dataRow(c, "x-"+name, rowVolume, name, L.DataExternal(), func() {
					iconButtonAction(c, "open-"+name, iconExternal, L.DataOpenVolume(), func() { a.openVolume(name) })
				})
			}
			if len(d.Volumes)+len(d.Folders)+len(d.External) == 0 {
				small(c, L.DataNone())
			}
		})
	})
}

// openStore opens the storage dialog for a named volume.
func (a *App) openStore(pr *compose.Project, key string) {
	ch := pr.Volumes[key]
	mode := ch.Mode
	if mode == "" {
		mode = "native"
	}
	a.cmp.in, a.cmp.dlg = composeInput{key: key, mode: mode, path: ch.Path}, "store"
}

// storeDialog chooses where a named volume keeps its data.
func (a *App) storeDialog(c *ui.Context, pr *compose.Project) {
	in := &a.cmp.in
	ui.Text(c, L.DataStoreTitle(in.key)).FontSize(16).Bold()
	ui.RadioGroup(c, func() {
		ui.Radio(c.Key("native"), &in.mode, "native", L.DataStoreVolume())
		ui.Radio(c.Key("bind"), &in.mode, "bind", L.DataStoreFolder())
	})
	if in.mode == "bind" {
		folderField(c, a, L.DataFolder(), &in.path)
	}
	cur := pr.Volumes[in.key]
	changed := (cur.Mode == "" && in.mode != "native") || (cur.Mode != "" && cur.Mode != in.mode) || cur.Path != strings.TrimSpace(in.path)
	if changed {
		ui.Text(c, L.CmpVolumesWarning()).TextColor(c.Theme().Warning)
	}
	ok := changed && (in.mode == "native" || strings.TrimSpace(in.path) != "")
	dialogButtons(c, a, L.Save(), ok, func() {
		if pr.Volumes == nil {
			pr.Volumes = map[string]compose.VolumeChoice{}
		}
		if in.mode == "native" {
			delete(pr.Volumes, in.key)
		} else {
			pr.Volumes[in.key] = compose.VolumeChoice{Mode: compose.VolumeBind, Path: strings.TrimSpace(in.path)}
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
				delete(a.cmp.data, proj.ID)
				a.notify(L.CfgSaved(), false)
			}
		})
	})
}

// backupDialog says what a project backup holds, where it goes and
// whether the project stops, then runs it.
func (a *App) backupDialog(c *ui.Context, pr *compose.Project, running bool) {
	p := &a.cmp
	in := &p.in
	d := p.data[pr.ID]
	t := c.Theme()
	ui.Text(c, L.CmpBackUpTitle(pr.Name)).FontSize(16).Bold()
	muted(c, L.CmpBackUpIncludes())
	if d == nil {
		ui.Spinner(c)
		return
	}
	ui.Column(c).FillWidth().Gap(2).Padding(4, 12).Radius(cardRadius).Border(1, t.Border).Children(func() {
		var files []string
		for name := range d.Files {
			files = append(files, name)
		}
		sort.Strings(files)
		dataLine := func(key string, icon *ui.SVG, name, size string) {
			ui.Row(c.Key(key)).FillWidth().Gap(10).Padding(6, 0).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon).Size(16, 16).TextColor(t.TextMuted)
				ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().Tooltip(name)
				small(c, size)
			})
		}
		dataLine("files", iconFile, L.CmpBackUpFiles(strings.Join(files, ", ")), "")
		for _, f := range d.Folders {
			name := f.Source
			if f.Relative != "" {
				name = "./" + f.Relative
			}
			size, ok := p.fsize[f.Source]
			dataLine("f-"+f.ID, iconFolder, L.DataFolder()+" "+name, sizeText(size, ok))
		}
		for _, v := range d.Volumes {
			size, ok := a.volumeSize(v.Name)
			dataLine("v-"+v.Name, iconDatabase, L.DataVolume()+" "+v.Name, sizeText(size, ok))
		}
	})
	if len(d.External) > 0 {
		callout(c, "external", toneWarn, iconInfo, L.CmpBackUpExternal(strings.Join(d.External, ", ")), func() {
			if ui.Button(c.Key("ext-backup"), L.VolBackUp()).Clicked() {
				for _, name := range d.External {
					a.backupVolumeNow(name)
				}
			}
		})
	}
	small(c, L.CmpBackUpTo(filepath.Join(a.backupDir(), "Repository")))
	if running {
		ui.Checkbox(c.Key("live"), &in.live, L.CmpBackUpLive())
		small(c, L.CmpBackUpLiveHelp())
	}
	dialogButtons(c, a, L.CmpBackUpStart(), true, func() { a.backupProjectNow(pr.Name, in.live) })
}

// backupVolumeNow backs one volume up into the default repository.
func (a *App) backupVolumeNow(name string) {
	a.startJob(L.WSLBackupNow()+" "+name, func(ctx context.Context, j *jobs.Job) error {
		_, err := a.k.Backup.BackupVolume(ctx, name, &backup.RepoWriter{Dir: filepath.Join(a.backupDir(), "Repository")}, backupOptions())
		return err
	}, func() { a.bk.loaded = false; a.loadVolumes() })
}

// serviceCard is one service's container: its name and state, image and
// usage, the ports it publishes and what it mounts.
func (a *App) serviceCard(c *ui.Context, s compose.ContainerStatus, sd core.ServiceData) {
	t := c.Theme()
	ui.Column(c.Key("svc-"+s.Name)).FillWidth().Gap(4).Padding(14, 16).Radius(cardRadius).Border(1, t.Border).Background(t.Background).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, s.Service).Bold().Grow(1)
			label, k := s.State, toneNeutral
			switch {
			case s.Health == "healthy":
				label, k = L.CtrHealthy(), toneGood
			case s.Health == "unhealthy":
				label, k = L.CtrUnhealthy(), toneBad
			case s.State == "running":
				label, k = L.StateRunning(), toneGood
			case s.State == "exited" && s.Exit != 0:
				label, k = L.CtrExited(s.Exit), toneBad
			case s.State == "exited":
				label = L.StateStopped()
			}
			ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(toneColor(t, k))
		})
		line := s.Image
		if st, ok := a.ctr.stats[shortID(s.ID)]; ok && s.State == "running" {
			line += " · " + L.CmpUsage(st.CPUPerc, strings.SplitN(st.MemUsage, " /", 2)[0])
		}
		small(c, line).SingleLine().Tooltip(line)
		detail := func(key string, icon *ui.SVG, text string) {
			ui.Row(c.Key(key)).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon).Size(13, 13).TextColor(t.TextMuted)
				small(c, text).SingleLine().Shrink(1).Tooltip(text)
			})
		}
		for i, port := range sd.Ports {
			detail("port"+itoa(i), iconNetwork, port)
		}
		for i, m := range sd.Mounts {
			switch m.Kind {
			case core.MountFolder:
				detail("m"+itoa(i), iconFolder, m.Display+" → "+m.Target+" · "+L.DataFolder()+roText(m.ReadOnly))
			case core.MountVolume:
				detail("m"+itoa(i), iconDatabase, m.Source+" → "+m.Target+" · "+L.DataVolume()+roText(m.ReadOnly))
			}
		}
		small(c, L.CmpContainer(s.Name)).SingleLine()
	})
}

func roText(ro bool) string {
	if ro {
		return " · " + L.DataReadOnly()
	}
	return ""
}
