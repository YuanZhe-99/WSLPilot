package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/configdoc"
	"github.com/YuanZhe-99/WSLPilot/internal/elevate"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/vhdx"
	"github.com/YuanZhe-99/WSLPilot/internal/wsl"
	"github.com/YuanZhe-99/WSLPilot/internal/wtprofile"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// selectDistro selects a distribution by name in the list.
func (a *App) selectDistro(name string) {
	for i, d := range a.wsl.distros {
		if d.Name == name {
			a.wsl.sel = i
		}
	}
	a.wsl.compactOpen = false
}

// wslView draws the WSL page: the distributions with the selected one's
// card beside them, or a distribution's settings page.
func (a *App) wslView(c *ui.Context) {
	p := &a.wsl
	if !p.loaded && !p.loading {
		a.loadDistros()
	}
	if p.settingsFor != "" {
		a.distroSettingsPage(c)
		return
	}
	sub := L.WSLSubtitle()
	if a.report != nil && a.report.HasWSL() {
		sub = L.WSLSubtitleVersion(a.report.WSL.WSL)
	}
	a.head(c, paneHead{icon: iconWSL, title: L.WSLTitle(), sub: sub, actions: func() {
		if ui.Button(c.Key("import"), L.WSLImport()).Clicked() {
			p.in, p.dlg = wslInput{version: "2"}, "import"
		}
		if ui.Button(c.Key("shutdown"), L.WSLShutdown()).Clicked() {
			a.ask(L.WSLShutdownTitle(), L.WSLShutdownMessage(), L.WSLShutdown(), func(bool) {
				a.startJob(L.WSLShutdown(), func(ctx context.Context, j *jobs.Job) error {
					err := a.k.WSL.Shutdown(ctx)
					a.k.Jobs.Audit("wsl.shutdown", "", "", err)
					return err
				}, a.loadDistros)
			})
		}
		ui.Button(c.Key("more"), "").Label(L.More()).Children(func() { ui.Icon(c, iconSliders).Size(14, 14) }).Menu(func(m *ui.Menu) {
			if m.Item(L.WSLSettingsAll()).Chosen() {
				if d, ok := p.selected(); ok {
					p.settingsFor, p.scope = d.Name, 1
				} else {
					p.settingsFor, p.scope = "*", 1
				}
			}
			if m.Item(L.WSLUpdate()).Chosen() {
				a.startJob(L.WSLUpdate(), func(ctx context.Context, j *jobs.Job) error { return a.k.WSL.Update(ctx, false, j) }, a.Refresh)
			}
			m.Separator()
			if m.Item(L.Refresh()).Chosen() {
				a.loadDistros()
			}
		})
		if ui.PrimaryButton(c.Key("install"), L.WSLInstall()).Clicked() {
			a.openInstall()
		}
	}})
	body(c, func() {
		if p.err != nil {
			callout(c, "err", toneBad, iconAlert, friendlyError(p.err), nil)
		}
		if p.loaded && len(p.distros) == 0 {
			emptyState(c, iconWSL, L.WSLNone(), L.WSLNoneHint(), func() {
				if ui.PrimaryButton(c.Key("install-empty"), L.WSLInstall()).Clicked() {
					a.openInstall()
				}
			})
			return
		}
		ui.Row(c.Key("wsl-main")).Grow(1).FillWidth().MinHeight(0).Gap(16).AlignItems(ui.Start).Children(func() {
			ui.Column(c.Key("wsl-table")).Grow(1).MinWidth(0).FillHeight().Radius(cardRadius).Border(1, c.Theme().Border).Clip().Children(func() {
				a.distroTable(c)
			})
			if d, ok := p.selected(); ok {
				ui.Scroll(c.Key("wsl-side")).Width(detailWidth).Shrink(0).FillHeight().Children(func() { a.distroCard(c, d) })
			}
		})
	})
	a.wslDialogs(c)
}

// distroTable is the list of distributions.
func (a *App) distroTable(c *ui.Context) {
	p := &a.wsl
	cols := []ui.TableColumn{{Title: L.ColName(), Width: 180}, {Title: L.ColState(), Width: 100}, {Title: L.ColVersion(), Width: 76}, {Title: L.ColDisk(), Width: 84, Align: ui.End}, {Title: L.ColLocation()}}
	ui.Table(c, &p.list, cols, len(p.distros), func(row, col int) {
		d := p.distros[row]
		switch col {
		case 0:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, d.Name).FontWeight(600).SingleLine()
				if d.Default {
					pill(c, L.WSLDefault(), selTone(toneAccent, row == p.sel))
				}
			})
		case 1:
			if d.Running {
				stateText(c, L.StateRunning(), selTone(toneGood, row == p.sel))
			} else {
				stateText(c, L.StateStopped(), selTone(toneNeutral, row == p.sel))
			}
		case 2:
			ui.Text(c, "WSL "+itoa(d.Version))
		case 3:
			if d.DiskBytes > 0 {
				ui.Text(c, jobs.FormatBytes(d.DiskBytes))
			}
		case 4:
			ui.Text(c, d.BasePath).SingleLine().Tooltip(d.BasePath).TextColor(toneColor(c.Theme(), selTone(toneNeutral, row == p.sel)))
		}
	}).Grow(1).FillWidth()
}

// distroCard is the selected distribution's card: its actions in a grid,
// the sparse disk, the compaction panel and Unregister.
func (a *App) distroCard(c *ui.Context, d wsl.Distro) {
	p := &a.wsl
	t := c.Theme()
	card(c, "", func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, d.Name).FontSize(17).Bold().Grow(1).SingleLine()
			if d.Flavor != "" {
				small(c, d.Flavor+" "+d.OSVersion)
			}
		})
		grid := func(fn func()) { ui.Grid(c).FillWidth().Columns(2).Gap(8).Children(fn) }
		grid(func() {
			if ui.PrimaryButton(c.Key("open-term"), L.WSLOpenTerminal()).FillWidth().Clicked() {
				a.openTerminal(d.Name, []string{"wsl.exe", "--distribution", d.Name, "--cd", "~"})
			}
			if d.Running {
				if ui.Button(c.Key("stop"), L.Stop()).FillWidth().Clicked() {
					a.distroJob(L.JobStop(d.Name), d.Name, "wsl.terminate", func(ctx context.Context, j *jobs.Job) error { return a.k.WSL.Terminate(ctx, d.Name) })
				}
			} else if ui.Button(c.Key("start"), L.Start()).FillWidth().Clicked() {
				a.distroJob(L.JobStart(d.Name), d.Name, "", func(ctx context.Context, j *jobs.Job) error { return a.k.WSL.Start(ctx, d.Name) })
			}
			if ui.Button(c.Key("move"), L.WSLMove()).FillWidth().Clicked() {
				p.in, p.dlg = wslInput{distro: d.Name}, "move"
			}
			if ui.Button(c.Key("export"), L.WSLExportBackup()).FillWidth().Clicked() {
				p.in, p.dlg = wslInput{distro: d.Name, format: "tar"}, "export"
			}
			if ui.Button(c.Key("settings"), L.WSLSettingsBtn()).FillWidth().Clicked() {
				p.settingsFor, p.scope = d.Name, 0
			}
			if ui.Button(c.Key("profile"), L.WSLTerminalProfile()).FillWidth().Clicked() {
				a.addDistroProfile(d.Name)
			}
		})
		ui.Button(c.Key("dmore"), L.WSLMoreActions()).FillWidth().Menu(func(m *ui.Menu) {
			if !d.Default && m.Item(L.WSLSetDefault()).Chosen() {
				a.distroJob(L.WSLSetDefault(), d.Name, "", func(ctx context.Context, j *jobs.Job) error { return a.k.WSL.SetDefault(ctx, d.Name) })
			}
			if m.Item(L.WSLBackupNow()).Chosen() {
				a.backupDistroNow(d.Name)
			}
			if cmd := updateCommand(d.Flavor); cmd != "" && m.Item(L.WSLUpdatePackages()).Chosen() {
				a.openTerminal(d.Name, []string{"wsl.exe", "--distribution", d.Name, "--user", "root", "--exec", "sh", "-c", cmd + "; echo; echo Done.; sleep 600"})
			}
			if m.Item(L.WSLDefaultUser()).Chosen() {
				p.in, p.dlg = wslInput{distro: d.Name}, "user"
			}
			if d.Version == 2 && m.Item(L.WSLResize()).Chosen() {
				p.in, p.dlg = wslInput{distro: d.Name}, "resize"
			}
			other := 1
			if d.Version == 1 {
				other = 2
			}
			if m.Item(L.WSLConvert(other)).Chosen() {
				a.ask(L.WSLConvertTitle(d.Name, other), L.WSLConvertMessage(), L.WSLConvert(other), func(bool) {
					a.distroJob(L.WSLConvert(other), d.Name, "wsl.set-version", func(ctx context.Context, j *jobs.Job) error {
						return a.k.WSL.SetVersion(ctx, d.Name, other, j)
					})
				})
			}
			if d.BasePath != "" && m.Item(L.ShowInExplorer()).Chosen() {
				a.host.OpenPath(d.BasePath)
			}
		})
		if d.Version == 2 {
			ui.Box(c).Height(1).FillWidth().Background(t.Border)
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, L.WSLSparse())
					small(c, L.WSLSparseHelp())
				})
				ui.Button(c.Key("sparse"), L.WSLSparseSet()).Menu(func(m *ui.Menu) {
					if m.Item(L.WSLSparseOn()).Chosen() {
						a.setSparse(d.Name, true)
					}
					if m.Item(L.WSLSparseOff()).Chosen() {
						a.setSparse(d.Name, false)
					}
				})
			})
			a.compactPanel(c, d)
		}
		if ui.Button(c.Key("unregister"), L.WSLUnregister()).TextColor(t.Danger).Border(1, t.Danger.Alpha(0.45)).Clicked() {
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
		}
	})
}

// compactPanel is "Compact disk": closed, a button with what can be
// reclaimed; open, the steps it will take, "Export a backup first" and
// the action, in a tinted panel as the design.
func (a *App) compactPanel(c *ui.Context, d wsl.Distro) {
	p := &a.wsl
	t := c.Theme()
	official := p.compact != nil && *p.compact
	u, measured := p.df[d.Name]
	reclaim := ""
	if measured && d.DiskBytes > u.UsedBytes {
		reclaim = L.WSLReclaimShort(jobs.FormatBytes(d.DiskBytes - u.UsedBytes))
	}
	if !p.compactOpen {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, L.WSLCompact())
				if reclaim != "" {
					small(c, reclaim)
				} else if d.Running && !measured {
					small(c, L.WSLMeasuring())
				}
			})
			if ui.Button(c.Key("compact-open"), L.WSLCompactOpen()).Clicked() {
				p.compactOpen, p.compactBackup = true, true
				if d.Running && !measured {
					a.measure(d.Name)
				}
			}
		})
		return
	}
	warn := t.Warning
	ui.Column(c.Key("compact")).FillWidth().Gap(8).Padding(14).Radius(10).Border(1, warn.Alpha(0.5)).Background(warn.Alpha(0.08)).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, L.WSLCompact()).Bold().Grow(1)
			if reclaim != "" {
				small(c, reclaim)
			}
		})
		steps := []string{L.WSLCompactStep1(d.Name)}
		if official {
			steps = append(steps, L.WSLCompactStep2Official(d.Name))
		} else {
			others := 0
			for _, o := range p.distros {
				if o.Running && o.Name != d.Name {
					others++
				}
			}
			steps = append(steps, L.WSLCompactStep2Shutdown(others), L.WSLCompactStep3Admin())
		}
		steps = append(steps, L.WSLCompactStepCheck(d.Name))
		for i, s := range steps {
			ui.Text(c.Key("step-"+itoa(i)), itoa(i+1)+". "+s).FontSize(13)
		}
		ui.Checkbox(c.Key("compact-backup"), &p.compactBackup, L.WSLCompactBackupFirst())
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c.Key("compact-cancel"), L.Cancel()).Clicked() {
				p.compactOpen = false
			}
			label := L.WSLCompact()
			if !official {
				label = L.WSLCompactShutdown()
			}
			if ui.PrimaryButton(c.Key("compact-go"), label).Clicked() {
				p.compactOpen = false
				a.runCompact(d, official, p.compactBackup)
			}
		})
	})
}

// runCompact compacts a distribution's disk as a job.
func (a *App) runCompact(d wsl.Distro, official, backupFirst bool) {
	a.distroJob(L.WSLCompact()+" "+d.Name, d.Name, "wsl.compact", func(ctx context.Context, j *jobs.Job) error {
		before := d.DiskBytes
		if backupFirst {
			j.SetProgress(-1, L.JobBackingUp())
			if _, err := a.k.Backup.BackupDistro(ctx, d.Name, a.safetyWriter(), backupOptions()); err != nil {
				return fmt.Errorf("%s: %w", L.JobBackupFailedStop(), err)
			}
		}
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
}

// addDistroProfile adds a Windows Terminal profile for the distribution.
func (a *App) addDistroProfile(name string) {
	a.async(func(ctx context.Context) func() {
		fr, err := wtprofile.Default()
		if err == nil {
			err = fr.Put(wtprofile.DistroShell(name, "", "~"))
		}
		return func() {
			if err != nil {
				a.notify(friendlyError(err), true)
			} else {
				a.notify(L.CtrProfileAdded(), false)
			}
		}
	})
}

// distroSettingsPage is a distribution's settings (wsl.conf) or all of
// WSL (.wslconfig), with the changes card beside the form.
func (a *App) distroSettingsPage(c *ui.Context) {
	p := &a.wsl
	name := p.settingsFor
	back := func() { p.settingsFor = "" }
	title := L.WSLDistroSettingsTitle(name)
	if name == "*" || p.scope == 1 {
		title = L.WSLSettingsAll()
	}
	a.head(c, paneHead{crumb: L.WSLTitle(), onCrumb: back, title: title, actions: func() {
		if name != "*" {
			ui.Segmented(c.Key("scope"), &p.scope, L.WSLThisDistro(), L.WSLSettingsAll()).Label(L.WSLSettingsAll())
		}
	}})
	if p.scope == 1 || name == "*" {
		a.wslconfigForm(c)
		return
	}
	f := &p.conf
	if p.confDistro != name {
		p.confDistro = name
		*f = confForm{fields: configdoc.WSLConf}
	}
	reload := func() {
		f.loading, f.loaded = true, false
		a.async(func(ctx context.Context) func() {
			text, exists, err := a.k.WSL.ReadFile(ctx, name, "/etc/wsl.conf")
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
	save := func(ctx context.Context, text string) error {
		cur, _, err := a.k.WSL.ReadFile(ctx, name, "/etc/wsl.conf")
		if err != nil {
			return err
		}
		if f.snap.Changed(cur) {
			return configdoc.ErrConflict
		}
		backup := "/etc/wsl.conf.wslpilot-" + time.Now().Format("20060102-150405")
		return a.k.WSL.WriteFile(ctx, name, "/etc/wsl.conf", backup, []byte(text))
	}
	restart := func() {
		a.distroJob(L.Restart()+" "+name, name, "", func(ctx context.Context, j *jobs.Job) error {
			if err := a.k.WSL.Terminate(ctx, name); err != nil {
				return err
			}
			return a.k.WSL.Start(ctx, name)
		})
	}
	f.formSplit(c, a, `\\wsl.localhost\`+name+`\etc\wsl.conf`, L.CfgEffectDistro(name), save, reload, L.CfgSaveRestart(name), restart)
}

// wslconfigForm is the .wslconfig form.
func (a *App) wslconfigForm(c *ui.Context) {
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
	f.formSplit(c, a, path, L.CfgEffectWSL(), func(ctx context.Context, text string) error {
		_, err := f.snap.Save(text)
		return err
	}, reload, "", nil)
}
