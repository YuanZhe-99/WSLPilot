package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
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

	dlg string
	in  volInput
}

// volInput holds the volume dialog fields.
type volInput struct {
	name, driver, size, file, target string
	newVolume, replace               bool
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
		return func() { p.loading, p.loaded, p.err, p.list, p.users, p.sizes = false, true, err, vs, users, sizes }
	})
}

// volumesView draws the Volumes page.
func (a *App) volumesView(c *ui.Context) {
	p := &a.vol
	if !p.loaded && !p.loading {
		p.ls.Selected = &p.sel
		p.sel = -1
		a.loadVolumes()
	}
	a.header(c, L.NavVolumes(), func() {
		ui.Button(c, L.Refresh()).Disabled(p.loading).OnClick(a.loadVolumes)
	})
	ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
		ui.PrimaryButton(c, L.VolCreate()).OnClick(func() { p.in = volInput{driver: "guest"}; p.dlg = "create" })
		ui.Button(c, L.VolImport()).OnClick(func() { p.in = volInput{newVolume: true, driver: "guest"}; p.dlg = "import" })
	})
	muted(c, L.VolHelp())
	if p.err != nil {
		errorText(c, friendlyError(p.err))
	}
	ui.Row(c).Grow(1).FillWidth().Gap(gap).Children(func() {
		cols := []ui.TableColumn{{Title: L.ColName()}, {Title: L.NetDriver(), Width: 80}, {Title: L.VolUsedBy()}, {Title: L.ColDisk(), Width: 100, Align: ui.End}}
		ui.Table(c, &p.ls, cols, len(p.list), func(row, col int) {
			v := p.list[row]
			switch col {
			case 0:
				ui.Text(c, v.Name).SingleLine()
			case 1:
				ui.Text(c, v.Driver)
			case 2:
				ui.Text(c, strings.Join(p.users[v.Name], ", ")).SingleLine()
			case 3:
				if n := p.sizes[v.Name]; n > 0 {
					ui.Text(c, jobs.FormatBytes(n))
				}
			}
		}).Grow(1).FillHeight()
		if p.sel >= 0 && p.sel < len(p.list) {
			v := p.list[p.sel]
			ui.Column(c).Width(detailWidth).FillHeight().Gap(gap).Children(func() { a.volumeDetail(c, v) })
		}
	})
	a.volumeDialogs(c)
}

func (a *App) volumeDetail(c *ui.Context, v wslc.Volume) {
	p := &a.vol
	ui.Text(c, v.Name).FontSize(sectionSize).Bold()
	card(c, "", func() {
		kv(c, L.NetDriver(), v.Driver)
		kv(c, L.ImgCreated(), v.CreatedAt)
		if v.Status.HostPath != "" {
			kv(c, L.WSLDiskFile(), v.Status.HostPath)
		}
		if users := p.users[v.Name]; len(users) > 0 {
			kv(c, L.VolUsedBy(), strings.Join(users, ", "))
		}
	})
	actions(c, func() {
		ui.Button(c, L.VolImportInto()).OnClick(func() { p.in = volInput{name: v.Name, target: v.Name}; p.dlg = "import" })
		ui.Button(c, L.VolExport()).OnClick(func() { p.in = volInput{name: v.Name}; p.dlg = "export" })
		ui.Button(c, L.WSLBackupNow()).OnClick(func() {
			name := v.Name
			a.startJob(L.WSLBackupNow()+" "+name, func(ctx context.Context, j *jobs.Job) error {
				_, err := a.k.Backup.BackupVolume(ctx, name, &backup.RepoWriter{Dir: filepath.Join(a.backupDir(), "Repository")}, backupOptions())
				return err
			}, func() { a.bk.loaded = false; a.loadVolumes() })
		})
		if v.Driver == "vhd" && v.Status.HostPath != "" {
			ui.Button(c, L.WSLCompact()).OnClick(func() {
				path := v.Status.HostPath
				a.ask(L.WSLCompactTitle(v.Name), L.VolCompactMessage(), L.WSLCompact(), func(bool) {
					a.startJob(L.WSLCompact()+" "+v.Name, func(ctx context.Context, j *jobs.Job) error {
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
						a.k.Jobs.Audit("wslc.volume.compact", v.Name, "", err)
						return err
					}, a.loadVolumes)
				})
			})
		}
	})
	card(c, L.DangerZone(), func() {
		dangerButton(c, L.VolRemove()).OnClick(func() {
			name := v.Name
			a.askDanger(L.VolRemoveTitle(name), L.VolRemoveMessage(), L.VolRemove(), name, func(backupFirst bool) {
				a.startJob(L.VolRemove()+" "+name, func(ctx context.Context, j *jobs.Job) error {
					if backupFirst {
						j.SetProgress(-1, L.JobBackingUp())
						if _, err := a.k.Backup.BackupVolume(ctx, name, a.safetyWriter(), backupOptions()); err != nil {
							return fmt.Errorf("%s: %w", L.JobBackupFailedStop(), err)
						}
					}
					err := a.k.WSLc.RemoveVolumes(ctx, name)
					a.k.Jobs.Audit("wslc.volume.remove", name, "", err)
					return err
				}, a.loadVolumes)
			})
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
				ui.Text(c, L.VolCreate()).FontSize(sectionSize).Bold()
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
			case "import":
				ui.Text(c, L.VolImport()).FontSize(sectionSize).Bold()
				muted(c, L.VolImportHelp())
				pathField(c, a, L.VolTarFile(), &p.in.file, false, "tar", "gz", "zst")
				if p.in.target == "" {
					volumeFields(c, &p.in)
				} else {
					kv(c, L.VolTarget(), p.in.target)
					ui.Checkbox(c, &p.in.replace, L.VolReplace())
					if p.in.replace {
						ui.Text(c, L.VolReplaceWarning()).TextColor(c.Theme().Warning)
					}
				}
				ok := p.in.file != "" && (p.in.target != "" || volumeInputOK(p.in))
				dialogButtons(c, a, L.VolImport(), ok, func() { a.importVolume(p.in) })
			case "export":
				ui.Text(c, L.VolExport()+" "+p.in.name).FontSize(sectionSize).Bold()
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
