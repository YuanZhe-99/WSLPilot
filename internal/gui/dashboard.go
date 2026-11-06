package gui

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/upstream"
	"github.com/YuanZhe-99/WSLPilot/internal/wsl"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// dashboard shows what WSLPilot found (doc/en-us/gui.md, "Dashboard"):
// three stat cards, disk usage, activity and quick actions, as the design.
func (a *App) dashboard(c *ui.Context) {
	sub := L.DashChecking()
	if r := a.report; r != nil && !a.probing {
		sub = L.DashCheckedAt(r.At.Local().Format("15:04"))
	}
	a.head(c, paneHead{icon: iconDashboard, title: L.NavDashboard(), sub: sub, actions: func() {
		if actionButton(c, iconRefresh, L.DashRefresh(), false).Disabled(a.probing).Clicked() {
			a.Refresh()
			a.wsl.loaded, a.ctr.loaded, a.vol.loaded, a.cmp.loaded = false, false, false, false
		}
		if a.host != nil && a.host.UpdatesEnabled() {
			if actionButton(c, iconArrowUp, L.UpdCheckNow(), true).Clicked() {
				a.OpenUpdates()
			}
		}
	}})
	r := a.report
	if r == nil {
		body(c, func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				muted(c, L.DashChecking())
			})
		})
		return
	}
	a.dashLoad()
	scrollBody(c, func() {
		if r.HasWSL() && upstream.NewerThanTested(r.WSL.WSL) {
			callout(c, "untested", toneWarn, iconAlert, L.DashUntested(r.WSL.WSL), nil)
		}
		ui.Row(c.Key("stats")).FillWidth().Gap(16).AlignItems(ui.Stretch).Children(func() {
			a.statWSL(c)
			a.statContainers(c)
			a.statCompose(c)
		})
		ui.Row(c.Key("mid")).FillWidth().Gap(16).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(3).Basis(0).MinWidth(0).Children(func() { a.diskCard(c) })
			ui.Column(c).Grow(2).Basis(0).MinWidth(0).Children(func() { a.activityCard(c) })
		})
		actions(c, func() {
			if d, ok := a.defaultDistro(); ok {
				if actionButton(c, iconTerminal, L.DashOpenTerminal(d.Name), false).Clicked() {
					a.openTerminal(d.Name, []string{"wsl.exe", "--distribution", d.Name, "--cd", "~"})
				}
			}
			if actionButton(c, iconPlus, L.DashInstallDistro(), false).Clicked() {
				a.page = PageWSL
				a.openInstall()
			}
			if r.HasWSLc() {
				if actionButton(c, iconPlay, L.DashRunContainer(), false).Clicked() {
					a.page = PageWSLc
					a.openRun("")
				}
				if actionButton(c, iconUpload, L.DashImportTar(), false).Clicked() {
					a.page = PageVolumes
					a.vol.in, a.vol.dlg = volInput{newVolume: true, driver: "guest"}, "import"
				}
			}
		})
	})
}

// dashLoad starts the loads the dashboard reads (each loads once).
func (a *App) dashLoad() {
	if !a.wsl.loaded && !a.wsl.loading {
		a.loadDistros()
	}
	if a.report.HasWSLc() {
		if !a.ctr.loaded && !a.ctr.loading {
			a.loadContainers()
		}
		if !a.vol.loaded && !a.vol.loading {
			a.loadVolumes()
		}
	}
	if a.k.Compose != nil && !a.cmp.loaded && !a.cmp.loading {
		a.loadProjects()
	}
	a.ensurePlans()
	for _, d := range a.wsl.distros {
		if d.Running && d.Version == 2 {
			if _, ok := a.wsl.df[d.Name]; !ok && !a.wsl.measuring[d.Name] {
				a.measure(d.Name)
			}
		}
	}
}

// defaultDistro is the default distribution, when there is one.
func (a *App) defaultDistro() (wsl.Distro, bool) {
	for _, d := range a.wsl.distros {
		if d.Default {
			return d, true
		}
	}
	return wsl.Distro{}, false
}

// statCard is one of the three cards at the top: title and pill, a big
// number, a line, and a link to its page.
func statCard(c *ui.Context, key, title, state string, k tone, number, line, link string, onLink func()) {
	t := c.Theme()
	ui.Column(c.Key(key)).Grow(1).Basis(0).MinWidth(0).Gap(8).Padding(18).Radius(cardRadius).Border(1, t.Border).Background(t.Background).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(sizeHeading).Bold().Grow(1)
			if state != "" {
				pill(c, state, k)
			}
		})
		ui.Text(c, number).FontSize(sizeStat).Bold()
		small(c, line)
		ui.Box(c).Grow(1)
		if link != "" {
			b := ui.ButtonBase(c.Key(key + "-link")).Label(link)
			b.Children(func() { ui.Text(c, link).FontSize(13).FontWeight(600).TextColor(t.Accent) })
			if b.Clicked() {
				onLink()
			}
		}
	})
}

func (a *App) statWSL(c *ui.Context) {
	r := a.report
	if !r.HasWSL() {
		statCard(c, "st-wsl", L.DashWSL(), L.DashNotFound(), toneBad, "—", L.DashWSLMissing(), L.DashInstallWSL(), func() { a.host.OpenURL("https://aka.ms/wslinstall") })
		return
	}
	running, def := 0, "—"
	for _, d := range a.wsl.distros {
		if d.Running {
			running++
		}
		if d.Default {
			def = d.Name
		}
	}
	line := L.DashWSLLine(running, def, r.WSL.WSL, r.WSL.Kernel)
	statCard(c, "st-wsl", L.DashWSL(), L.DashReady(), toneGood, L.DashDistros(len(a.wsl.distros)), line, L.DashManageDistros(), func() { a.page = PageWSL })
}

func (a *App) statContainers(c *ui.Context) {
	r := a.report
	if !r.HasWSLc() {
		statCard(c, "st-wslc", L.DashWSLc(), L.DashNotFound(), toneNeutral, "—", L.DashWSLcHint(), "", nil)
		return
	}
	running := 0
	for _, s := range a.ctr.visible() {
		if s.Running() {
			running++
		}
	}
	line := L.DashWSLcLine(running, len(a.ctr.images), len(a.vol.list), r.WSLc.Client.Version)
	statCard(c, "st-wslc", L.DashWSLc(), L.DashReady(), toneGood, L.DashContainersN(len(a.ctr.visible())), line, L.DashManageContainers(), func() { a.page = PageWSLc })
}

func (a *App) statCompose(c *ui.Context) {
	state, k, line := L.DashBuiltin(), toneNeutral, L.DashComposeBuiltin()
	if a.report.NativeCompose {
		state, k, line = L.DashNative(), toneAccent, L.DashComposeNative()
	}
	statCard(c, "st-compose", L.DashCompose(), state, k, L.DashProjects(len(a.cmp.projects)), line, L.DashOpenProjects(), func() { a.page = PageCompose })
}

// diskCard shows each WSL 2 disk: used inside, reclaimable, and the WSLc
// data; a disk with room to reclaim links to Compact.
func (a *App) diskCard(c *ui.Context) {
	t := c.Theme()
	card(c, L.DashDiskUsage(), func() {
		any, unmeasured := false, false
		// Bars share one scale, the largest disk, so sizes compare at a glance.
		largest := float64(a.ctr.storageBytes)
		for _, d := range a.wsl.distros {
			largest = max(largest, float64(d.DiskBytes))
		}
		onDisk := t.TextMuted.Alpha(0.45)
		for _, d := range a.wsl.distros {
			if d.Version != 2 || d.DiskBytes == 0 {
				continue
			}
			any = true
			u, measured := a.wsl.df[d.Name]
			ui.Column(c.Key("disk-" + d.Name)).FillWidth().Gap(6).Children(func() {
				ui.Row(c).FillWidth().Children(func() {
					ui.Text(c, d.Name+" · ext4.vhdx").Grow(1).SingleLine()
					if measured {
						small(c, L.DashDiskLine(jobs.FormatBytes(d.DiskBytes), jobs.FormatBytes(u.UsedBytes)))
					} else {
						small(c, L.DashDiskOnly(jobs.FormatBytes(d.DiskBytes)))
					}
				})
				if measured && d.DiskBytes > 0 {
					used := float64(u.UsedBytes) / largest
					reclaim := float64(max(d.DiskBytes-u.UsedBytes, 0)) / largest
					bar(c, barH, barSeg{used, t.Accent}, barSeg{reclaim, reclaimColor})
					if d.DiskBytes-u.UsedBytes > 1<<30 {
						name := d
						ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
							small(c, L.WSLReclaimable(jobs.FormatBytes(d.DiskBytes-u.UsedBytes)))
							b := ui.ButtonBase(c.Key("compact-" + d.Name)).Label(L.WSLCompact())
							b.Children(func() { ui.Text(c, L.WSLCompact()).FontSize(sizeSmall).FontWeight(600).TextColor(t.Accent) })
							if b.Clicked() {
								a.page = PageWSL
								a.selectDistro(name.Name)
								a.wsl.compactOpen = true
							}
						})
					}
				} else {
					unmeasured = true
					bar(c, barH, barSeg{float64(d.DiskBytes) / largest, onDisk})
				}
			})
		}
		if a.ctr.storageBytes > 0 {
			any = true
			ui.Column(c.Key("disk-wslc")).FillWidth().Gap(6).Children(func() {
				ui.Row(c).FillWidth().Children(func() {
					ui.Text(c, L.DashWSLcData()).Grow(1)
					small(c, jobs.FormatBytes(a.ctr.storageBytes))
				})
				unmeasured = true
				bar(c, barH, barSeg{float64(a.ctr.storageBytes) / largest, onDisk})
			})
		}
		if !any {
			small(c, L.DashNoDisks())
			return
		}
		ui.Row(c).Gap(16).Children(func() {
			legend(c, t.Accent, L.DashUsed())
			legend(c, reclaimColor, L.DashReclaimable())
			if unmeasured {
				legend(c, onDisk, L.DashOnDisk())
			}
		})
	})
}

// activityCard shows the running job with its progress, the last results
// and the next scheduled backup.
func (a *App) activityCard(c *ui.Context) {
	t := c.Theme()
	card(c, L.DashActivity(), func() {
		list := a.k.Jobs.List()
		shown := 0
		for _, s := range list {
			if s.State != jobs.Running {
				continue
			}
			shown++
			id := s.ID
			ui.Column(c.Key("act-run-" + itoa(id))).FillWidth().Gap(6).Padding(12).Radius(8).Background(t.Accent.Alpha(0.07)).Children(func() {
				ui.Row(c).FillWidth().Children(func() {
					ui.Text(c, s.Title).FontWeight(600).Grow(1).SingleLine()
					if s.Progress >= 0 {
						small(c, itoa(int(s.Progress*100))+"%")
					}
				})
				if s.Progress >= 0 {
					bar(c, 6, barSeg{s.Progress, t.Accent})
				} else {
					ui.Progress(c, -1).Label(s.Title).FillWidth()
				}
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					small(c, s.Step).Grow(1).SingleLine()
					b := ui.ButtonBase(c.Key("act-cancel-" + itoa(id))).Label(L.Cancel())
					b.Children(func() { ui.Text(c, L.Cancel()).FontSize(sizeSmall).FontWeight(600).TextColor(t.Accent) })
					if b.Clicked() {
						if j := a.k.Jobs.Get(id); j != nil {
							j.Cancel()
						}
					}
				})
			})
		}
		done := 0
		for _, s := range list {
			if s.State == jobs.Running || done >= 4 {
				continue
			}
			done++
			label, k := L.JobStateDone(), toneGood
			switch s.State {
			case jobs.Failed:
				label, k = L.JobStateFailed(), toneBad
			case jobs.Cancelled:
				label, k = L.JobStateCancelled(), toneNeutral
			}
			ui.Row(c.Key("act-" + itoa(s.ID))).FillWidth().Gap(10).Children(func() {
				ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(toneColor(t, k)).Width(72).Shrink(0)
				ui.Text(c, s.Title).FontSize(13).Grow(1).SingleLine()
			})
		}
		if shown+done == 0 {
			small(c, L.DashNoActivity())
		}
		ui.Box(c).Height(1).FillWidth().Background(t.Border)
		if next, name := a.nextBackup(); !next.IsZero() {
			small(c, L.DashNextBackup(name, next.Local().Format("01-02 15:04")))
		} else {
			small(c, L.DashNoPlans())
		}
	})
}

// nextBackup is the soonest scheduled run of an enabled plan.
func (a *App) nextBackup() (time.Time, string) {
	var at time.Time
	var name string
	for _, p := range a.bk.plans {
		st := a.bk.states[p.ID]
		if !p.Enabled || st.NextRun.IsZero() {
			continue
		}
		if at.IsZero() || st.NextRun.Before(at) {
			at, name = st.NextRun, p.Name
		}
	}
	return at, name
}

// ensureDashboardFresh reloads the dashboard data when it is shown again.
func (a *App) ensureDashboardFresh(ctx context.Context) {}
