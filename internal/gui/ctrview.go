package gui

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/restart"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
	"github.com/YuanZhe-99/WSLPilot/internal/wtprofile"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// openRun opens the run dialog, for an image or empty.
func (a *App) openRun(image string) {
	a.ctr.in = ctrInput{image: image, start: true, restart: restart.No}
	a.ctr.dlg = "run"
}

// containersView draws the WSL Containers page: containers, images or
// networks, as the design.
func (a *App) containersView(c *ui.Context) {
	p := &a.ctr
	if !p.loaded && !p.loading {
		a.loadContainers()
	}
	sub := ""
	if a.report != nil && a.report.HasWSLc() {
		sub = L.CtrSubtitle(a.report.WSLc.Client.Version)
	}
	a.head(c, paneHead{icon: iconBox, title: L.NavWSLc(), sub: sub, actions: func() {
		if len(p.sessions) > 1 {
			ui.Select(c.Key("session"), &p.choice, p.sessions).Label(L.CtrSession()).Width(190).OnChange(func() { a.selectSession(p.choice) })
		}
		ui.Segmented(c.Key("tabs"), &p.tab, L.CtrContainers(), L.CtrImages(), L.CtrNetworks()).Label(L.NavWSLc())
		iconButtonAction(c, "refresh", iconRefresh, L.Refresh(), a.loadContainers)
		switch p.tab {
		case 0:
			if ui.PrimaryButton(c.Key("run"), L.CtrRun()).Clicked() {
				a.openRun("")
			}
		case 1:
			if ui.PrimaryButton(c.Key("pull"), L.ImgPull()).Clicked() {
				p.in, p.dlg = ctrInput{}, "pull"
			}
		case 2:
			if ui.PrimaryButton(c.Key("net"), L.NetCreate()).Clicked() {
				p.in, p.dlg = ctrInput{}, "network"
			}
		}
	}})
	if a.report != nil && !a.report.HasWSLc() {
		body(c, func() { emptyState(c, iconBox, L.DashNotFound(), L.DashWSLcHint(), nil) })
		return
	}
	body(c, func() {
		if a.ctr.client.Load() != nil {
			callout(c, "other-session", toneWarn, iconAlert, L.CtrOtherSession(p.choice), nil)
		}
		if p.err != nil {
			callout(c, "err", toneBad, iconAlert, friendlyError(p.err), nil)
		}
		switch p.tab {
		case 1:
			a.imageList(c)
		case 2:
			a.networkList(c)
		default:
			a.containerList(c)
		}
	})
	a.containerDialogs(c)
}

// iconButtonAction is iconButton with its action.
func iconButtonAction(c *ui.Context, key string, icon *ui.SVG, label string, fn func()) {
	if iconButton(c, key, icon, label, false) {
		fn()
	}
}

// tableBox frames a table as the design's cards.
func tableBox(c *ui.Context, key string, grow bool, fn func()) {
	b := ui.Column(c.Key(key)).FillWidth().Radius(cardRadius).Border(1, c.Theme().Border).Clip()
	if grow {
		b.Grow(1).MinHeight(0)
	}
	b.Children(fn)
}

// containerState is a container's state as a colored label.
func containerState(s wslc.ContainerSummary, d wslc.Container) (string, tone) {
	switch {
	case s.Running() && d.State.Health != nil && d.State.Health.Status == "healthy":
		return L.CtrHealthy(), toneGood
	case s.Running() && d.State.Health != nil && d.State.Health.Status == "unhealthy":
		return L.CtrUnhealthy(), toneBad
	case s.Running():
		return L.StateRunning(), toneGood
	case s.State == "exited" && d.State.ExitCode != 0:
		return L.CtrExited(d.State.ExitCode), toneBad
	case s.State == "exited":
		return L.CtrExited(0), toneNeutral
	}
	return s.State, toneNeutral
}

// restartText names a restart policy.
func restartText(pol string) string {
	switch {
	case pol == "" || pol == restart.No:
		return L.CtrRestartNo()
	case pol == restart.Always:
		return L.CtrRestartAlways()
	case pol == restart.UnlessStopped:
		return L.CtrRestartUnless()
	}
	return L.CtrRestartOnFailure()
}

func (a *App) containerList(c *ui.Context) {
	p := &a.ctr
	vis := p.visible()
	if p.loaded && len(vis) == 0 {
		emptyState(c, iconBox, L.CtrNone(), L.CtrNoneHint(), func() {
			if ui.PrimaryButton(c.Key("run-empty"), L.CtrRun()).Clicked() {
				a.openRun("")
			}
		})
		return
	}
	hasLogs := p.csel >= 0 && p.csel < len(vis)
	tableBox(c, "ctr-table", !hasLogs, func() {
		cols := []ui.TableColumn{{Title: L.ColName()}, {Title: L.CtrImage()}, {Title: L.ColState(), Width: 120}, {Title: L.CtrPorts()}, {Title: L.CtrCPU(), Width: 64, Align: ui.End}, {Title: L.CtrMemory(), Width: 90, Align: ui.End}, {Title: L.CtrRestartPolicy(), Width: 130}}
		t := ui.Table(c, &p.cl, cols, len(vis), func(row, col int) {
			s := vis[row]
			d := p.detail[shortID(s.ID)]
			st := p.stats[shortID(s.ID)]
			switch col {
			case 0:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, s.Names).FontWeight(600).SingleLine()
					if proj := d.Labels["com.docker.compose.project"]; proj != "" {
						pill(c, proj, selTone(toneNeutral, row == p.csel))
					}
				})
			case 1:
				ui.Text(c, s.Image).SingleLine()
			case 2:
				label, k := containerState(s, d)
				stateText(c, label, selTone(k, row == p.csel))
			case 3:
				ui.Text(c, strings.ReplaceAll(s.Ports, "->", "→")).SingleLine()
			case 4:
				ui.Text(c, dashIfEmpty(st.CPUPerc))
			case 5:
				ui.Text(c, dashIfEmpty(strings.SplitN(st.MemUsage, " /", 2)[0]))
			case 6:
				ui.Text(c, restartText(d.Labels[restart.Label])).SingleLine()
			}
		})
		if !hasLogs {
			t.Grow(1)
		} else {
			t.Height(min(float32(len(vis)+1)*tableRowH+8, 260))
		}
	})
	if hasLogs {
		a.containerPanel(c, vis[p.csel])
	}
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// containerPanel is the selected container's logs card with its actions.
func (a *App) containerPanel(c *ui.Context, s wslc.ContainerSummary) {
	p := &a.ctr
	t := c.Theme()
	d := p.detail[shortID(s.ID)]
	if p.logsFor != s.ID {
		a.openLogs(s.ID)
	}
	ui.Column(c.Key("ctr-logs")).Grow(1).MinHeight(200).FillWidth().Gap(12).Padding(16).Radius(cardRadius).Border(1, t.Border).Background(t.Background).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, L.CtrLogsOf(s.Names)).FontSize(sizeHeading).Bold().Grow(1).SingleLine()
			ui.Row(c).Width(200).Height(32).Gap(6).Padding(0, 10).Radius(7).Border(1, t.Border).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconSearch).Size(13, 13).TextColor(t.TextMuted)
				ui.TextInputBase(c.Key("log-q"), &p.logQuery).Grow(1).Placeholder(L.CtrSearchLogs()).Label(L.CtrSearchLogs())
			})
			if s.Running() {
				if ui.Button(c.Key("shell"), L.CtrShell()).Clicked() {
					a.openTerminal(s.Names, []string{"wslc.exe", "container", "exec", "--interactive", "--tty", s.ID, "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh"})
				}
				if ui.Button(c.Key("restart"), L.Restart()).Clicked() {
					a.wslcJob(L.Restart()+" "+s.Names, "", func(ctx context.Context) error { return a.wc().Restart(ctx, s.ID) })
				}
				if ui.Button(c.Key("stop"), L.Stop()).Clicked() {
					a.wslcJob(L.JobStop(s.Names), "", func(ctx context.Context) error { return a.wc().Stop(ctx, -1, s.ID) })
				}
			} else if ui.PrimaryButton(c.Key("start"), L.Start()).Clicked() {
				a.wslcJob(L.JobStart(s.Names), "", func(ctx context.Context) error { return a.wc().Start(ctx, s.ID) })
			}
			ui.Button(c.Key("cmore"), "").Label(L.More()).Children(func() { ui.Icon(c, iconSliders).Size(14, 14) }).Menu(func(m *ui.Menu) {
				if d.ID != "" && d.Labels["com.docker.compose.project"] == "" && m.Item(L.CtrEdit()).Chosen() {
					p.in, p.dlg = editInput(d), "run"
				}
				if p.wt && m.Item(L.CtrAddProfile()).Chosen() {
					name := s.Names
					a.async(func(ctx context.Context) func() {
						fr, err := wtprofile.Default()
						if err == nil {
							err = fr.Put(wtprofile.ContainerShell(name, "sh"))
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
				m.Separator()
				if m.Item(L.CtrRemove()).Chosen() {
					a.ask(L.CtrRemoveTitle(s.Names), L.CtrRemoveMessage(), L.CtrRemove(), func(bool) {
						a.wslcJob(L.CtrRemove()+" "+s.Names, "wslc.container.remove", func(ctx context.Context) error {
							return a.wc().Remove(ctx, true, false, s.ID)
						})
					})
				}
			})
		})
		text := ""
		if p.logs != nil {
			text = lastLines(p.logs.String(), 400)
		}
		if q := strings.TrimSpace(p.logQuery); q != "" {
			var keep []string
			for _, l := range strings.Split(text, "\n") {
				if strings.Contains(strings.ToLower(l), strings.ToLower(q)) {
					keep = append(keep, l)
				}
			}
			text = strings.Join(keep, "\n")
		}
		if text == "" {
			text = L.CtrNoLogs()
		}
		ui.Column(c.Key("console-wrap")).Grow(1).MinHeight(0).FillWidth().Children(func() {
			console(c, "console", text, 2000).Grow(1)
		})
		small(c, L.CtrRestartNote())
	})
}

func (a *App) imageList(c *ui.Context) {
	p := &a.ctr
	if p.loaded && len(p.images) == 0 {
		emptyState(c, iconImage, L.ImgNone(), L.ImgNoneHint(), nil)
		return
	}
	tableBox(c, "img-table", true, func() {
		cols := []ui.TableColumn{{Title: L.ImgRepository()}, {Title: L.ImgTag(), Width: 130}, {Title: "ID", Width: 130}, {Title: L.ColDisk(), Width: 90, Align: ui.End}, {Title: L.ImgCreated(), Width: 180}}
		ui.Table(c, &p.il, cols, len(p.images), func(row, col int) {
			im := p.images[row]
			switch col {
			case 0:
				ui.Text(c, im.Repository).FontWeight(600).SingleLine()
			case 1:
				ui.Text(c, im.Tag)
			case 2:
				ui.Text(c, shortID(strings.TrimPrefix(im.ID, "sha256:")))
			case 3:
				ui.Text(c, im.Size)
			case 4:
				ui.Text(c, im.CreatedAt).SingleLine()
			}
		}).Grow(1)
	})
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		if p.isel >= 0 && p.isel < len(p.images) {
			im := p.images[p.isel]
			ui.Text(c, im.Ref()).Bold().Grow(1)
			if actionButton(c, iconPlay, L.CtrRun(), false).Clicked() {
				a.openRun(im.Ref())
			}
			if dangerButton(c, L.ImgRemove()).Clicked() {
				a.ask(L.ImgRemoveTitle(im.Ref()), L.ImgRemoveMessage(), L.ImgRemove(), func(bool) {
					a.wslcJob(L.ImgRemove()+" "+im.Ref(), "wslc.image.remove", func(ctx context.Context) error { return a.wc().RemoveImages(ctx, false, im.Ref()) })
				})
			}
		} else {
			small(c, L.ImgPick()).Grow(1)
		}
		if ui.Button(c.Key("prune"), L.ImgPrune()).Clicked() {
			a.ask(L.ImgPruneTitle(), L.ImgPruneMessage(), L.ImgPrune(), func(bool) {
				a.wslcJob(L.ImgPrune(), "wslc.image.prune", func(ctx context.Context) error { _, err := a.wc().Prune(ctx, "image"); return err })
			})
		}
	})
}

func (a *App) networkList(c *ui.Context) {
	p := &a.ctr
	tableBox(c, "net-table", true, func() {
		cols := []ui.TableColumn{{Title: L.ColName()}, {Title: L.NetDriver(), Width: 100}, {Title: L.NetSubnet(), Width: 170}, {Title: L.CtrContainers(), Width: 110, Align: ui.End}}
		ui.Table(c, &p.nl, cols, len(p.networks), func(row, col int) {
			n := p.networks[row]
			switch col {
			case 0:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, n.Name).FontWeight(600)
					if n.Builtin() {
						pill(c, L.NetBuiltin(), toneNeutral)
					}
				})
			case 1:
				ui.Text(c, n.Driver)
			case 2:
				if len(n.IPAM.Config) > 0 {
					ui.Text(c, n.IPAM.Config[0].Subnet)
				}
			case 3:
				ui.Text(c, itoa(len(n.Containers)))
			}
		}).Grow(1)
	})
	if p.nsel >= 0 && p.nsel < len(p.networks) && !p.networks[p.nsel].Builtin() {
		n := p.networks[p.nsel]
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, n.Name).Bold().Grow(1)
			if dangerButton(c, L.NetRemove()).Clicked() {
				a.ask(L.NetRemoveTitle(n.Name), L.NetRemoveMessage(), L.NetRemove(), func(bool) {
					a.wslcJob(L.NetRemove()+" "+n.Name, "wslc.network.remove", func(ctx context.Context) error { return a.wc().RemoveNetworks(ctx, n.Name) })
				})
			}
		})
	}
}
