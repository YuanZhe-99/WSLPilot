package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/configdoc"
	"github.com/YuanZhe-99/WSLPilot/internal/elevate"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/vhdx"
	"github.com/YuanZhe-99/WSLPilot/internal/wsl"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// wslPage is the state of the WSL page.
type wslPage struct {
	view int // 0 distributions, 1 WSL settings (.wslconfig)

	loaded, loading bool
	err             error
	distros         []wsl.Distro
	list            ui.ListState
	sel             int
	tab             int // 0 overview, 1 settings (wsl.conf)
	df              map[string]wsl.DiskUsage
	compact         *bool // wsl.exe has --manage --compact

	conf       confForm
	confDistro string
	wslconfig  confForm

	dlg       string // open dialog: install, import, export, move, resize, user
	online    []wsl.OnlineDistro
	onlineErr error
	in        wslInput
}

// wslInput holds the fields of the WSL dialogs.
type wslInput struct {
	distro, name, location, file, format, size, user, choice string
	web, vhd                                                 bool
	version                                                  string
}

// loadDistros reads the distribution list.
func (a *App) loadDistros() {
	p := &a.wsl
	if p.loading {
		return
	}
	p.loading = true
	a.async(func(ctx context.Context) func() {
		ds, err := a.k.WSL.List(ctx)
		var comp *bool
		if p.compact == nil {
			v := a.k.WSL.SupportsCompact(ctx)
			comp = &v
		}
		return func() {
			p.loading, p.loaded, p.err, p.distros = false, true, err, ds
			if comp != nil {
				p.compact = comp
			}
			if p.sel >= len(ds) {
				p.sel = len(ds) - 1
			}
		}
	})
}

// selected returns the chosen distribution.
func (p *wslPage) selected() (wsl.Distro, bool) {
	if p.sel < 0 || p.sel >= len(p.distros) {
		return wsl.Distro{}, false
	}
	return p.distros[p.sel], true
}

// wslView draws the WSL page.
func (a *App) wslView(c *ui.Context) {
	p := &a.wsl
	if !p.loaded && !p.loading {
		p.list.Selected = &p.sel
		a.loadDistros()
	}
	a.header(c, L.NavWSL(), func() {
		ui.Segmented(c, &p.view, L.WSLDistros(), L.WSLSettings()).Label(L.NavWSL())
	})
	if p.view == 1 {
		a.wslconfigView(c)
		return
	}
	ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
		ui.PrimaryButton(c, L.WSLInstall()).OnClick(a.openInstall)
		ui.Button(c, L.WSLImport()).OnClick(func() { p.in = wslInput{version: "2"}; p.dlg = "import" })
		ui.Spacer(c)
		ui.Button(c, L.WSLUpdate()).OnClick(func() {
			a.startJob(L.WSLUpdate(), func(ctx context.Context, j *jobs.Job) error { return a.k.WSL.Update(ctx, false, j) }, a.Refresh)
		})
		ui.Button(c, L.WSLShutdown()).OnClick(func() {
			a.ask(L.WSLShutdownTitle(), L.WSLShutdownMessage(), L.WSLShutdown(), func(bool) {
				a.startJob(L.WSLShutdown(), func(ctx context.Context, j *jobs.Job) error {
					err := a.k.WSL.Shutdown(ctx)
					a.k.Jobs.Audit("wsl.shutdown", "", "", err)
					return err
				}, a.loadDistros)
			})
		})
		ui.Button(c, L.Refresh()).Disabled(p.loading).OnClick(a.loadDistros)
	})
	if p.err != nil {
		errorText(c, friendlyError(p.err))
	}
	if p.loaded && len(p.distros) == 0 {
		card(c, L.WSLNone(), func() { muted(c, L.WSLNoneHint()) })
	}
	ui.Row(c).Grow(1).FillWidth().Gap(gap).Children(func() {
		cols := []ui.TableColumn{{Title: L.ColName()}, {Title: L.ColState(), Width: 110}, {Title: L.ColVersion(), Width: 70}, {Title: L.ColDisk(), Width: 100, Align: ui.End}}
		ui.Table(c, &p.list, cols, len(p.distros), func(row, col int) {
			d := p.distros[row]
			switch col {
			case 0:
				name := d.Name
				if d.Default {
					name += "  ★"
				}
				ui.Text(c, name).SingleLine()
			case 1:
				if d.Running {
					ui.Text(c, "● "+L.StateRunning())
				} else {
					ui.Text(c, "○ "+L.StateStopped())
				}
			case 2:
				ui.Text(c, "WSL "+itoa(d.Version))
			case 3:
				if d.DiskBytes > 0 {
					ui.Text(c, jobs.FormatBytes(d.DiskBytes))
				}
			}
		}).Grow(1).FillHeight()
		if d, ok := p.selected(); ok {
			ui.Column(c).Width(detailWidth + 80).FillHeight().Gap(gap).Children(func() { a.distroDetail(c, d) })
		}
	})
	a.wslDialogs(c)
}

// distroDetail is the side panel for one distribution.
func (a *App) distroDetail(c *ui.Context, d wsl.Distro) {
	p := &a.wsl
	ui.Text(c, d.Name).FontSize(sectionSize).Bold()
	ui.Tabs(c, &p.tab, L.WSLOverview(), L.WSLDistroSettings())
	if p.tab == 1 {
		a.distroConf(c, d)
		return
	}
	ui.Scroll(c).Grow(1).FillWidth().Gap(gap).Children(func() {
		card(c, "", func() {
			kv(c, L.ColState(), map[bool]string{true: L.StateRunning(), false: L.StateStopped()}[d.Running])
			kv(c, L.ColVersion(), "WSL "+itoa(d.Version))
			if d.Flavor != "" {
				kv(c, L.WSLFlavor(), d.Flavor+" "+d.OSVersion)
			}
			if d.VHDPath != "" {
				kv(c, L.WSLDiskFile(), d.VHDPath)
				kv(c, L.ColDisk(), jobs.FormatBytes(d.DiskBytes))
			} else if d.BasePath != "" {
				kv(c, L.WSLFolder(), d.BasePath)
			}
			if u, ok := p.df[d.Name]; ok {
				kv(c, L.WSLUsedInside(), jobs.FormatBytes(u.UsedBytes))
				if d.DiskBytes > u.UsedBytes {
					muted(c, L.WSLReclaimable(jobs.FormatBytes(d.DiskBytes-u.UsedBytes)))
				}
			} else if d.Running && d.Version == 2 {
				ui.Button(c, L.WSLMeasure()).OnClick(func() { a.measure(d.Name) })
			}
		})
		ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
			ui.PrimaryButton(c, L.WSLOpenTerminal()).OnClick(func() { a.openTerminal(d.Name, []string{"wsl.exe", "--distribution", d.Name, "--cd", "~"}) })
			if d.Running {
				ui.Button(c, L.Stop()).OnClick(func() {
					a.distroJob(L.JobStop(d.Name), d.Name, "wsl.terminate", func(ctx context.Context, j *jobs.Job) error { return a.k.WSL.Terminate(ctx, d.Name) })
				})
			} else {
				ui.Button(c, L.Start()).OnClick(func() {
					a.distroJob(L.JobStart(d.Name), d.Name, "", func(ctx context.Context, j *jobs.Job) error { return a.k.WSL.Start(ctx, d.Name) })
				})
			}
			if d.BasePath != "" {
				ui.Button(c, L.ShowInExplorer()).OnClick(func() { a.host.OpenPath(d.BasePath) })
			}
		})
		card(c, L.WSLManage(), func() {
			actions(c, func() {
				if !d.Default {
					ui.Button(c, L.WSLSetDefault()).OnClick(func() {
						a.distroJob(L.WSLSetDefault(), d.Name, "", func(ctx context.Context, j *jobs.Job) error { return a.k.WSL.SetDefault(ctx, d.Name) })
					})
				}
				ui.Button(c, L.WSLBackupNow()).OnClick(func() { a.backupDistroNow(d.Name) })
				ui.Button(c, L.WSLExport()).OnClick(func() { p.in = wslInput{distro: d.Name, format: "tar"}; p.dlg = "export" })
				ui.Button(c, L.WSLDefaultUser()).OnClick(func() { p.in = wslInput{distro: d.Name}; p.dlg = "user" })
				other := 1
				if d.Version == 1 {
					other = 2
				}
				ui.Button(c, L.WSLConvert(other)).OnClick(func() {
					a.ask(L.WSLConvertTitle(d.Name, other), L.WSLConvertMessage(), L.WSLConvert(other), func(bool) {
						a.distroJob(L.WSLConvert(other), d.Name, "wsl.set-version", func(ctx context.Context, j *jobs.Job) error {
							return a.k.WSL.SetVersion(ctx, d.Name, other, j)
						})
					})
				})
			})
		})
		if d.Version == 2 {
			card(c, L.WSLDisk(), func() {
				actions(c, func() {
					ui.Button(c, L.WSLCompact()).OnClick(func() { a.askCompact(d) })
					ui.Button(c, L.WSLMove()).OnClick(func() { p.in = wslInput{distro: d.Name}; p.dlg = "move" })
					ui.Button(c, L.WSLResize()).OnClick(func() { p.in = wslInput{distro: d.Name}; p.dlg = "resize" })
					ui.Button(c, L.WSLSparseOn()).OnClick(func() { a.setSparse(d.Name, true) })
					ui.Button(c, L.WSLSparseOff()).OnClick(func() { a.setSparse(d.Name, false) })
				})
				muted(c, L.WSLSparseHelp())
			})
		}
		card(c, L.DangerZone(), func() {
			dangerButton(c, L.WSLUnregister()).OnClick(func() {
				a.askDanger(L.WSLUnregisterTitle(d.Name), L.WSLUnregisterMessage(), L.WSLUnregister(), d.Name, func(backupFirst bool) {
					a.distroJob(L.WSLUnregister()+" "+d.Name, d.Name, "wsl.unregister", func(ctx context.Context, j *jobs.Job) error {
						if backupFirst {
							j.SetProgress(-1, L.JobBackingUp())
							if _, err := a.k.Backup.BackupDistro(ctx, d.Name, a.safetyWriter(), backupOptions()); err != nil {
								return fmt.Errorf("%s: %w", L.JobBackupFailedStop(), err)
							}
						}
						return a.k.WSL.Unregister(ctx, d.Name)
					})
				})
			})
		})
	})
}

// kv draws a label and a value.
func kv(c *ui.Context, k, v string) {
	ui.Row(c.Key(k)).FillWidth().Gap(smallGap).Children(func() {
		muted(c, k).Width(120)
		ui.Text(c, v).Grow(1).Selectable()
	})
}

// distroJob runs a job that changes a distribution and reloads the list;
// audit names a destructive action to log ("" for none).
func (a *App) distroJob(title, distro, audit string, fn jobs.Func) {
	a.startJob(title, func(ctx context.Context, j *jobs.Job) error {
		err := fn(ctx, j)
		if audit != "" {
			a.k.Jobs.Audit(audit, distro, "", err)
		}
		return err
	}, a.loadDistros)
}

// measure reads df in a running distribution.
func (a *App) measure(name string) {
	a.async(func(ctx context.Context) func() {
		u, err := a.k.WSL.DF(ctx, name)
		return func() {
			if err != nil {
				a.notify(friendlyError(err), true)
				return
			}
			if a.wsl.df == nil {
				a.wsl.df = map[string]wsl.DiskUsage{}
			}
			a.wsl.df[name] = u
		}
	})
}

// askCompact confirms and runs a compaction.
func (a *App) askCompact(d wsl.Distro) {
	official := a.wsl.compact != nil && *a.wsl.compact
	msg := L.WSLCompactMessage()
	if !official {
		msg = L.WSLCompactFallbackMessage()
	}
	a.ask(L.WSLCompactTitle(d.Name), msg, L.WSLCompact(), func(bool) {
		a.distroJob(L.WSLCompact()+" "+d.Name, d.Name, "wsl.compact", func(ctx context.Context, j *jobs.Job) error {
			before := d.DiskBytes
			if d.Running {
				j.SetProgress(-1, L.JobTrim())
				a.k.WSL.Exec(ctx, d.Name, "root", "fstrim", "-a")
			}
			if official {
				j.SetProgress(-1, L.WSLCompact())
				if err := a.k.WSL.Compact(ctx, d.Name, j); err != nil {
					return err
				}
			} else {
				j.SetProgress(-1, L.WSLShutdown())
				if err := a.k.WSL.Shutdown(ctx); err != nil {
					return err
				}
				time.Sleep(2 * time.Second)
				j.SetProgress(-1, L.JobElevated())
				if _, err := vhdx.Compact(ctx, elevate.Run, d.VHDPath, j.Logf); err != nil {
					return err
				}
			}
			if st, err := os.Stat(d.VHDPath); err == nil {
				j.Logf(L.WSLCompacted(jobs.FormatBytes(before), jobs.FormatBytes(st.Size())))
			}
			return nil
		})
	})
}

// setSparse turns sparse VHD on or off.
func (a *App) setSparse(name string, on bool) {
	a.distroJob(L.WSLSparseOn(), name, "", func(ctx context.Context, j *jobs.Job) error {
		if err := a.k.WSL.Terminate(ctx, name); err != nil {
			return err
		}
		return a.k.WSL.SetSparse(ctx, name, on)
	})
}

// backupOptions are the defaults for safety and manual backups.
func backupOptions() backup.Options { return backup.Options{} }

// safetyWriter is where backups made before a danger action go.
func (a *App) safetyWriter() backup.Writer {
	return &backup.ArchiveWriter{Dir: filepath.Join(a.backupDir(), "Safety"), Compression: backup.CompressZstd}
}

// backupDistroNow backs a distribution up into the default location.
func (a *App) backupDistroNow(name string) {
	a.startJob(L.WSLBackupNow()+" "+name, func(ctx context.Context, j *jobs.Job) error {
		w := &backup.RepoWriter{Dir: filepath.Join(a.backupDir(), "Repository")}
		it, err := a.k.Backup.BackupDistro(ctx, name, w, backupOptions())
		if err == nil {
			j.Logf(it.Path)
		}
		return err
	}, func() { a.loadDistros(); a.bk.loaded = false })
}

// openInstall opens the install dialog and loads the online list.
func (a *App) openInstall() {
	p := &a.wsl
	p.in = wslInput{version: "2"}
	p.dlg = "install"
	if p.online != nil {
		return
	}
	a.async(func(ctx context.Context) func() {
		ds, err := a.k.WSL.ListOnline(ctx)
		return func() { p.online, p.onlineErr = ds, err }
	})
}

// wslDialogs draws the open dialog of the WSL page.
func (a *App) wslDialogs(c *ui.Context) {
	p := &a.wsl
	if p.dlg == "" {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(dialogWidth).Gap(gap).Children(func() {
			switch p.dlg {
			case "install":
				a.installDialog(c)
			case "import":
				a.importDialog(c)
			case "export":
				a.exportDialog(c)
			case "move":
				ui.Text(c, L.WSLMoveTitle(p.in.distro)).FontSize(sectionSize).Bold()
				muted(c, L.WSLMoveHelp())
				folderField(c, a, L.WSLNewLocation(), &p.in.location)
				dialogButtons(c, a, L.WSLMove(), p.in.location != "", func() {
					in := p.in
					a.distroJob(L.WSLMove()+" "+in.distro, in.distro, "wsl.move", func(ctx context.Context, j *jobs.Job) error {
						if err := a.k.WSL.Terminate(ctx, in.distro); err != nil {
							return err
						}
						return a.k.WSL.Move(ctx, in.distro, in.location, j)
					})
				})
			case "resize":
				ui.Text(c, L.WSLResizeTitle(p.in.distro)).FontSize(sectionSize).Bold()
				muted(c, L.WSLResizeHelp())
				ui.Field(c, L.WSLNewSize(), func() { ui.TextInput(c, &p.in.size).Placeholder("256GB").Label(L.WSLNewSize()) })
				ok := (configdoc.Field{Kind: configdoc.Size}).Validate(p.in.size) == nil && p.in.size != ""
				dialogButtons(c, a, L.WSLResize(), ok, func() {
					in := p.in
					a.distroJob(L.WSLResize()+" "+in.distro, in.distro, "wsl.resize", func(ctx context.Context, j *jobs.Job) error {
						if err := a.k.WSL.Terminate(ctx, in.distro); err != nil {
							return err
						}
						return a.k.WSL.Resize(ctx, in.distro, in.size)
					})
				})
			case "user":
				ui.Text(c, L.WSLDefaultUser()).FontSize(sectionSize).Bold()
				ui.Field(c, L.WSLUserName(), func() { ui.TextInput(c, &p.in.user).Label(L.WSLUserName()) })
				dialogButtons(c, a, L.Save(), strings.TrimSpace(p.in.user) != "", func() {
					in := p.in
					a.distroJob(L.WSLDefaultUser(), in.distro, "", func(ctx context.Context, j *jobs.Job) error {
						return a.k.WSL.SetDefaultUser(ctx, in.distro, strings.TrimSpace(in.user))
					})
				})
			}
		})
	})
	if !open {
		p.dlg = ""
	}
}

// dialogButtons draws Cancel and the action; act runs and closes.
func dialogButtons(c *ui.Context, a *App, action string, enabled bool, act func()) {
	ui.Row(c).FillWidth().Gap(smallGap).Justify(ui.End).Children(func() {
		ui.Button(c, L.Cancel()).OnClick(a.closeDialogs)
		ui.PrimaryButton(c, action).Disabled(!enabled).OnClick(func() {
			act()
			a.closeDialogs()
		})
	})
}

// closeDialogs closes every page dialog.
func (a *App) closeDialogs() {
	a.wsl.dlg, a.ctr.dlg, a.vol.dlg, a.bk.dlg, a.cmp.dlg = "", "", "", "", ""
}

// folderField is a text field with a Browse button for a folder.
func folderField(c *ui.Context, a *App, label string, v *string) {
	pathField(c, a, label, v, true)
}

// pathField is a text field with a Browse button.
func pathField(c *ui.Context, a *App, label string, v *string, folder bool, exts ...string) {
	ui.Field(c.Key(label), label, func() {
		ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
			ui.TextInput(c, v).Label(label).Grow(1)
			ui.Button(c, L.Browse()).OnClick(func() {
				go func() {
					ps := a.host.PickFiles(label, folder, exts...)
					if len(ps) > 0 {
						a.update(func() { *v = ps[0] })
					}
				}()
			})
		})
	})
}

func (a *App) installDialog(c *ui.Context) {
	p := &a.wsl
	ui.Text(c, L.WSLInstallTitle()).FontSize(sectionSize).Bold()
	switch {
	case p.onlineErr != nil:
		errorText(c, friendlyError(p.onlineErr))
	case p.online == nil:
		ui.Spinner(c)
	default:
		names := make([]string, len(p.online))
		for i, d := range p.online {
			names[i] = d.FriendlyName + " (" + d.Name + ")"
		}
		ui.Field(c, L.WSLDistribution(), func() {
			ui.Select(c, &p.in.choice, names).Label(L.WSLDistribution())
		})
		for i, n := range names {
			if n == p.in.choice {
				p.in.distro = p.online[i].Name
			}
		}
	}
	ui.Field(c, L.WSLNameOptional(), func() { ui.TextInput(c, &p.in.name).Label(L.WSLNameOptional()) })
	folderField(c, a, L.WSLLocationOptional(), &p.in.location)
	ui.Checkbox(c, &p.in.web, L.WSLWebDownload())
	dialogButtons(c, a, L.WSLInstall(), p.in.distro != "", func() {
		in := p.in
		a.startJob(L.WSLInstall()+" "+in.distro, func(ctx context.Context, j *jobs.Job) error {
			return a.k.WSL.Install(ctx, in.distro, wsl.InstallOptions{Name: in.name, Location: in.location, WebDownload: in.web}, j)
		}, a.loadDistros)
	})
}

func (a *App) importDialog(c *ui.Context) {
	p := &a.wsl
	ui.Text(c, L.WSLImportTitle()).FontSize(sectionSize).Bold()
	muted(c, L.WSLImportHelp())
	pathField(c, a, L.WSLImportFile(), &p.in.file, false, "tar", "gz", "xz", "vhdx", "wsl")
	ui.Field(c, L.ColName(), func() { ui.TextInput(c, &p.in.name).Label(L.ColName()) })
	folderField(c, a, L.WSLLocation(), &p.in.location)
	ui.Checkbox(c, &p.in.vhd, L.WSLImportVHD())
	ok := p.in.file != "" && validDistroName(p.in.name) && p.in.location != ""
	if p.in.name != "" && !validDistroName(p.in.name) {
		errorText(c, L.WSLBadName())
	}
	dialogButtons(c, a, L.WSLImport(), ok, func() {
		in := p.in
		a.startJob(L.WSLImport()+" "+in.name, func(ctx context.Context, j *jobs.Job) error {
			vhd := in.vhd || strings.HasSuffix(strings.ToLower(in.file), ".vhdx")
			return a.k.WSL.Import(ctx, in.name, in.location, in.file, wsl.ImportOptions{VHD: vhd}, nil, j)
		}, a.loadDistros)
	})
}

func (a *App) exportDialog(c *ui.Context) {
	p := &a.wsl
	ui.Text(c, L.WSLExportTitle(p.in.distro)).FontSize(sectionSize).Bold()
	muted(c, L.WSLExportHelp())
	ui.Field(c, L.WSLFormat(), func() {
		ui.Select(c, &p.in.format, []string{"tar", "tar.gz", "tar.xz", "vhd"}).Label(L.WSLFormat())
	})
	pathField(c, a, L.WSLExportTo(), &p.in.file, false)
	dialogButtons(c, a, L.WSLExport(), p.in.file != "", func() {
		in := p.in
		a.distroJob(L.WSLExport()+" "+in.distro, in.distro, "", func(ctx context.Context, j *jobs.Job) error {
			d, err := a.k.WSL.Get(ctx, in.distro)
			if err != nil {
				return err
			}
			if d.Running {
				j.SetProgress(-1, L.JobStopping(in.distro))
				if err := a.k.WSL.Terminate(ctx, in.distro); err != nil {
					return err
				}
			}
			err = a.k.WSL.Export(ctx, in.distro, in.file, wsl.ExportFormat(in.format), nil)
			if err != nil {
				os.Remove(in.file)
			}
			return err
		})
	})
}

// validDistroName follows WSL's rule: letters, digits, '.', '-', '_'.
func validDistroName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// distroConf is the wsl.conf form of a distribution.
func (a *App) distroConf(c *ui.Context, d wsl.Distro) {
	p := &a.wsl
	f := &p.conf
	if p.confDistro != d.Name {
		p.confDistro = d.Name
		*f = confForm{fields: configdoc.WSLConf}
	}
	reload := func() {
		f.loading = true
		f.loaded = false
		a.async(func(ctx context.Context) func() {
			text, exists, err := a.k.WSL.ReadFile(ctx, d.Name, "/etc/wsl.conf")
			return func() {
				if err != nil {
					f.err, f.loading = err, false
					return
				}
				f.reset(configdoc.FromText("/etc/wsl.conf", text, exists))
			}
		})
	}
	if !f.loaded && !f.loading && f.err == nil {
		reload()
	}
	f.view(c, a, "wsl.conf", `\\wsl.localhost\`+d.Name+`\etc\wsl.conf`, L.CfgEffectDistro(d.Name), func(ctx context.Context, text string) error {
		cur, _, err := a.k.WSL.ReadFile(ctx, d.Name, "/etc/wsl.conf")
		if err != nil {
			return err
		}
		if f.snap.Changed(cur) {
			return configdoc.ErrConflict
		}
		backup := "/etc/wsl.conf.wslpilot-" + time.Now().Format("20060102-150405")
		return a.k.WSL.WriteFile(ctx, d.Name, "/etc/wsl.conf", backup, []byte(text))
	}, reload)
}

// wslconfigView is the .wslconfig form.
func (a *App) wslconfigView(c *ui.Context) {
	f := &a.wsl.wslconfig
	if f.fields == nil {
		f.fields = configdoc.WSLConfig
	}
	path := filepath.Join(os.Getenv("USERPROFILE"), ".wslconfig")
	reload := func() {
		f.loading, f.loaded = true, false
		a.async(func(ctx context.Context) func() {
			snap, err := configdoc.ReadFile(path)
			return func() {
				if err != nil {
					f.err, f.loading = err, false
					return
				}
				f.reset(snap)
			}
		})
	}
	if !f.loaded && !f.loading && f.err == nil {
		reload()
	}
	f.view(c, a, ".wslconfig", path, L.CfgEffectWSL(), func(ctx context.Context, text string) error {
		_, err := f.snap.Save(text)
		return err
	}, reload)
}
