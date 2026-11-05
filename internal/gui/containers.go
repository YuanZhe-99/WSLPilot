package gui

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/restart"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
	"github.com/YuanZhe-99/WSLPilot/internal/wtprofile"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// containersPage is the state of the WSL Containers page.
type containersPage struct {
	tab int // 0 containers, 1 images, 2 networks

	loaded, loading  bool
	err              error
	list             []wslc.ContainerSummary
	detail           map[string]wslc.Container
	stats            map[string]wslc.Stats
	images           []wslc.Image
	networks         []wslc.Network
	cl, il, nl       ui.ListState
	csel, isel, nsel int
	showAll          bool
	wt               bool // Windows Terminal is installed

	logsFor    string
	logs       *logBuffer
	logsCancel context.CancelFunc
	follow     bool

	dlg string
	in  ctrInput
}

// ctrInput holds the fields of the container dialogs.
type ctrInput struct {
	image, name, ports, env, volumes, command, restart, network, ref string
	subnet                                                           string
	start                                                            bool
}

// logBuffer collects streamed log text, bounded, safe across goroutines.
type logBuffer struct {
	mu   sync.Mutex
	text []byte
	a    *App
}

// Write appends p and redraws.
func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.text = append(b.text, p...)
	if len(b.text) > 512<<10 {
		b.text = append([]byte(nil), b.text[len(b.text)-256<<10:]...)
	}
	b.mu.Unlock()
	b.a.update(func() {})
	return len(p), nil
}

// String returns the collected text.
func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.text)
}

// loadContainers reads containers, images and networks.
func (a *App) loadContainers() {
	p := &a.ctr
	if p.loading {
		return
	}
	p.loading = true
	a.async(func(ctx context.Context) func() {
		list, err := a.k.WSLc.Containers(ctx)
		ids := make([]string, 0, len(list))
		for _, s := range list {
			ids = append(ids, s.ID)
		}
		detail := map[string]wslc.Container{}
		if ds, derr := a.k.WSLc.InspectContainers(ctx, ids...); derr == nil {
			for _, d := range ds {
				detail[shortID(d.ID)] = d
			}
		}
		stats := map[string]wslc.Stats{}
		if ss, serr := a.k.WSLc.Stats(ctx); serr == nil {
			for _, s := range ss {
				stats[shortID(s.ID)] = s
			}
		}
		imgs, _ := a.k.WSLc.Images(ctx)
		nets, _ := a.k.WSLc.Networks(ctx)
		wt := wtprofile.Installed()
		sort.Slice(list, func(i, j int) bool { return list[i].Names < list[j].Names })
		return func() {
			p.loading, p.loaded, p.err = false, true, err
			p.list, p.detail, p.stats, p.images, p.networks, p.wt = list, detail, stats, imgs, nets, wt
		}
	})
}

// shortID is the 12-character form wslc lists use.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// visible returns the containers the list shows (helpers hidden).
func (p *containersPage) visible() []wslc.ContainerSummary {
	var res []wslc.ContainerSummary
	for _, s := range p.list {
		if d, ok := p.detail[shortID(s.ID)]; ok && d.Labels["io.wslpilot.helper"] == "1" {
			continue
		}
		if p.showAll || s.Running() || s.State == "created" || s.State == "exited" {
			res = append(res, s)
		}
	}
	return res
}

// containersView draws the WSL Containers page.
func (a *App) containersView(c *ui.Context) {
	p := &a.ctr
	if !p.loaded && !p.loading {
		p.cl.Selected, p.il.Selected, p.nl.Selected = &p.csel, &p.isel, &p.nsel
		p.csel, p.isel, p.nsel = -1, -1, -1
		a.loadContainers()
	}
	a.header(c, L.NavWSLc(), func() {
		ui.Button(c, L.Refresh()).Disabled(p.loading).OnClick(a.loadContainers)
	})
	if a.report != nil && !a.report.HasWSLc() {
		card(c, L.DashNotFound(), func() { muted(c, L.DashWSLcHint()) })
		return
	}
	if p.err != nil {
		errorText(c, friendlyError(p.err))
	}
	ui.Tabs(c, &p.tab, L.CtrContainers(), L.CtrImages(), L.CtrNetworks())
	switch p.tab {
	case 0:
		a.containerList(c)
	case 1:
		a.imageList(c)
	case 2:
		a.networkList(c)
	}
	a.containerDialogs(c)
}

func (a *App) containerList(c *ui.Context) {
	p := &a.ctr
	vis := p.visible()
	ui.Row(c).FillWidth().Gap(smallGap).AlignItems(ui.Center).Children(func() {
		ui.PrimaryButton(c, L.CtrRun()).OnClick(func() { p.in = ctrInput{start: true, restart: restart.No}; p.dlg = "run" })
		ui.Spacer(c)
		ui.Checkbox(c, &p.showAll, L.CtrShowAll())
		dangerButton(c, L.CtrPrune()).OnClick(func() {
			a.ask(L.CtrPruneTitle(), L.CtrPruneMessage(), L.CtrPrune(), func(bool) {
				a.wslcJob(L.CtrPrune(), "wslc.container.prune", func(ctx context.Context) error { _, err := a.k.WSLc.Prune(ctx, "container"); return err })
			})
		})
	})
	if p.loaded && len(vis) == 0 {
		muted(c, L.CtrNone())
	}
	ui.Row(c).Grow(1).FillWidth().Gap(gap).Children(func() {
		cols := []ui.TableColumn{{Title: L.ColName()}, {Title: L.CtrImage()}, {Title: L.ColState(), Width: 80}, {Title: L.CtrPorts()}, {Title: L.CtrCPU(), Width: 64, Align: ui.End}, {Title: L.CtrMemory(), Width: 90, Align: ui.End}}
		ui.Table(c, &p.cl, cols, len(vis), func(row, col int) {
			s := vis[row]
			st := p.stats[shortID(s.ID)]
			switch col {
			case 0:
				ui.Text(c, s.Names).SingleLine()
			case 1:
				ui.Text(c, s.Image).SingleLine()
			case 2:
				ui.Text(c, s.State)
			case 3:
				ui.Text(c, s.Ports).SingleLine()
			case 4:
				ui.Text(c, st.CPUPerc)
			case 5:
				ui.Text(c, strings.SplitN(st.MemUsage, " /", 2)[0])
			}
		}).Grow(1).FillHeight()
		if p.csel >= 0 && p.csel < len(vis) {
			s := vis[p.csel]
			ui.Column(c).Width(detailWidth + 80).FillHeight().Gap(gap).Children(func() { a.containerDetail(c, s) })
		}
	})
}

// containerDetail is the side panel for one container.
func (a *App) containerDetail(c *ui.Context, s wslc.ContainerSummary) {
	p := &a.ctr
	d := p.detail[shortID(s.ID)]
	ui.Text(c, s.Names).FontSize(sectionSize).Bold()
	ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
		if s.Running() {
			ui.PrimaryButton(c, L.CtrShell()).OnClick(func() {
				a.openTerminal(s.Names, []string{"wslc.exe", "container", "exec", "--interactive", "--tty", s.ID, "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh"})
			})
			ui.Button(c, L.Stop()).OnClick(func() {
				a.wslcJob(L.JobStop(s.Names), "", func(ctx context.Context) error { return a.k.WSLc.Stop(ctx, -1, s.ID) })
			})
			ui.Button(c, L.Restart()).OnClick(func() {
				a.wslcJob(L.Restart()+" "+s.Names, "", func(ctx context.Context) error { return a.k.WSLc.Restart(ctx, s.ID) })
			})
		} else {
			ui.PrimaryButton(c, L.Start()).OnClick(func() {
				a.wslcJob(L.JobStart(s.Names), "", func(ctx context.Context) error { return a.k.WSLc.Start(ctx, s.ID) })
			})
		}
		ui.Button(c, L.CtrLogs()).OnClick(func() { a.openLogs(s.ID) })
	})
	ui.Scroll(c).Grow(1).FillWidth().Gap(gap).Children(func() {
		card(c, "", func() {
			kv(c, L.CtrImage(), s.Image)
			kv(c, L.ColState(), s.Status)
			kv(c, "ID", shortID(s.ID))
			if s.Ports != "" {
				kv(c, L.CtrPorts(), s.Ports)
			}
			for name, n := range d.NetworkSettings.Networks {
				kv(c, L.CtrNetwork(), name+"  "+n.IPAddress)
			}
			for _, m := range d.Mounts {
				src := m.Source
				if m.Name != "" {
					src = m.Name
				}
				kv(c, L.CtrMount(), src+" → "+m.Destination)
			}
			if pol := d.Labels[restart.Label]; pol != "" {
				kv(c, L.CtrRestartPolicy(), pol)
			}
			if proj := d.Labels["com.docker.compose.project"]; proj != "" {
				kv(c, L.NavCompose(), proj+" / "+d.Labels["com.docker.compose.service"])
			}
		})
		if p.logsFor == s.ID && p.logs != nil {
			card(c, L.CtrLogs(), func() {
				ui.Row(c).Gap(smallGap).Children(func() {
					ui.Button(c, L.Close()).OnClick(a.closeLogs)
				})
				ui.Text(c, lastLines(p.logs.String(), 300)).Font("monospace").FontSize(11).Selectable()
			})
		}
		if p.wt {
			ui.Button(c, L.CtrAddProfile()).OnClick(func() {
				fr, err := wtprofile.Default()
				if err == nil {
					err = fr.Put(wtprofile.ContainerShell(s.Names, "sh"))
				}
				if err != nil {
					a.notify(friendlyError(err), true)
				} else {
					a.notify(L.CtrProfileAdded(), false)
				}
			})
		}
		card(c, L.DangerZone(), func() {
			dangerButton(c, L.CtrRemove()).OnClick(func() {
				a.ask(L.CtrRemoveTitle(s.Names), L.CtrRemoveMessage(), L.CtrRemove(), func(bool) {
					a.wslcJob(L.CtrRemove()+" "+s.Names, "wslc.container.remove", func(ctx context.Context) error {
						return a.k.WSLc.Remove(ctx, true, false, s.ID)
					})
				})
			})
		})
	})
}

// lastLines keeps the last n lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// openLogs starts following a container's log.
func (a *App) openLogs(id string) {
	a.closeLogs()
	p := &a.ctr
	ctx, cancel := context.WithCancel(context.Background())
	p.logsFor, p.logsCancel, p.logs = id, cancel, &logBuffer{a: a}
	buf := p.logs
	go a.k.WSLc.Logs(ctx, id, wslc.LogOptions{Follow: true, Tail: 500, Timestamps: true}, buf)
}

// closeLogs stops following.
func (a *App) closeLogs() {
	p := &a.ctr
	if p.logsCancel != nil {
		p.logsCancel()
	}
	p.logsFor, p.logsCancel, p.logs = "", nil, nil
}

// wslcJob runs a WSLc command as a job and reloads the page.
func (a *App) wslcJob(title, audit string, fn func(ctx context.Context) error) {
	a.startJob(title, func(ctx context.Context, j *jobs.Job) error {
		err := fn(ctx)
		if audit != "" {
			a.k.Jobs.Audit(audit, title, "", err)
		}
		return err
	}, func() { a.loadContainers(); a.vol.loaded = false })
}

func (a *App) imageList(c *ui.Context) {
	p := &a.ctr
	ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
		ui.PrimaryButton(c, L.ImgPull()).OnClick(func() { p.in = ctrInput{}; p.dlg = "pull" })
		ui.Spacer(c)
		dangerButton(c, L.ImgPrune()).OnClick(func() {
			a.ask(L.ImgPruneTitle(), L.ImgPruneMessage(), L.ImgPrune(), func(bool) {
				a.wslcJob(L.ImgPrune(), "wslc.image.prune", func(ctx context.Context) error { _, err := a.k.WSLc.Prune(ctx, "image"); return err })
			})
		})
	})
	cols := []ui.TableColumn{{Title: L.ImgRepository()}, {Title: L.ImgTag(), Width: 120}, {Title: "ID", Width: 130}, {Title: L.ColDisk(), Width: 90, Align: ui.End}, {Title: L.ImgCreated(), Width: 170}}
	ui.Table(c, &p.il, cols, len(p.images), func(row, col int) {
		im := p.images[row]
		switch col {
		case 0:
			ui.Text(c, im.Repository).SingleLine()
		case 1:
			ui.Text(c, im.Tag)
		case 2:
			muted(c, shortID(strings.TrimPrefix(im.ID, "sha256:")))
		case 3:
			ui.Text(c, im.Size)
		case 4:
			muted(c, im.CreatedAt).SingleLine()
		}
	}).Grow(1).FillWidth()
	if p.isel >= 0 && p.isel < len(p.images) {
		im := p.images[p.isel]
		ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
			ui.Text(c, im.Ref()).Bold().Grow(1)
			ui.Button(c, L.CtrRun()).OnClick(func() { p.in = ctrInput{image: im.Ref(), start: true, restart: restart.No}; p.dlg = "run" })
			dangerButton(c, L.ImgRemove()).OnClick(func() {
				a.ask(L.ImgRemoveTitle(im.Ref()), L.ImgRemoveMessage(), L.ImgRemove(), func(bool) {
					a.wslcJob(L.ImgRemove()+" "+im.Ref(), "wslc.image.remove", func(ctx context.Context) error { return a.k.WSLc.RemoveImages(ctx, false, im.Ref()) })
				})
			})
		})
	}
}

func (a *App) networkList(c *ui.Context) {
	p := &a.ctr
	ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
		ui.PrimaryButton(c, L.NetCreate()).OnClick(func() { p.in = ctrInput{}; p.dlg = "network" })
	})
	cols := []ui.TableColumn{{Title: L.ColName()}, {Title: L.NetDriver(), Width: 90}, {Title: L.NetSubnet(), Width: 150}, {Title: L.CtrContainers(), Width: 100, Align: ui.End}}
	ui.Table(c, &p.nl, cols, len(p.networks), func(row, col int) {
		n := p.networks[row]
		switch col {
		case 0:
			ui.Text(c, n.Name)
		case 1:
			ui.Text(c, n.Driver)
		case 2:
			if len(n.IPAM.Config) > 0 {
				ui.Text(c, n.IPAM.Config[0].Subnet)
			}
		case 3:
			ui.Text(c, itoa(len(n.Containers)))
		}
	}).Grow(1).FillWidth()
	if p.nsel >= 0 && p.nsel < len(p.networks) {
		n := p.networks[p.nsel]
		if !n.Builtin() {
			dangerButton(c, L.NetRemove()+" "+n.Name).OnClick(func() {
				a.ask(L.NetRemoveTitle(n.Name), L.NetRemoveMessage(), L.NetRemove(), func(bool) {
					a.wslcJob(L.NetRemove()+" "+n.Name, "wslc.network.remove", func(ctx context.Context) error { return a.k.WSLc.RemoveNetworks(ctx, n.Name) })
				})
			})
		}
	}
}

// containerDialogs draws the open dialog of the page.
func (a *App) containerDialogs(c *ui.Context) {
	p := &a.ctr
	if p.dlg == "" {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(dialogWidth).Gap(gap).Children(func() {
			switch p.dlg {
			case "run":
				ui.Text(c, L.CtrRunTitle()).FontSize(sectionSize).Bold()
				ui.Form(c, func() {
					ui.Field(c, L.CtrImage(), func() { ui.TextInput(c, &p.in.image).Placeholder("nginx:latest").Label(L.CtrImage()) })
					ui.Field(c, L.ColName(), func() { ui.TextInput(c, &p.in.name).Label(L.ColName()) })
					ui.Field(c, L.CtrPorts(), func() { ui.TextInput(c, &p.in.ports).Placeholder("8080:80, 443:443").Label(L.CtrPorts()) }).Description(L.CtrPortsHelp())
					ui.Field(c, L.CtrEnv(), func() { ui.TextArea(c, &p.in.env).Lines(2, 6).Label(L.CtrEnv()) }).Description(L.CtrEnvHelp())
					ui.Field(c, L.CtrVolumes(), func() { ui.TextArea(c, &p.in.volumes).Lines(1, 4).Label(L.CtrVolumes()) }).Description(L.CtrVolumesHelp())
					ui.Field(c, L.CtrCommand(), func() { ui.TextInput(c, &p.in.command).Label(L.CtrCommand()) })
					ui.Field(c, L.CtrRestartPolicy(), func() {
						ui.Select(c, &p.in.restart, []string{restart.No, restart.Always, restart.UnlessStopped, restart.OnFailure}).Label(L.CtrRestartPolicy())
					}).Description(L.CtrRestartHelp())
				})
				dialogButtons(c, a, L.CtrRun(), strings.TrimSpace(p.in.image) != "", func() {
					spec := runSpec(p.in)
					a.wslcJob(L.CtrRun()+" "+spec.Image, "", func(ctx context.Context) error {
						id, err := a.k.WSLc.Create(ctx, spec)
						if err != nil {
							return err
						}
						return a.k.WSLc.Start(ctx, id)
					})
				})
			case "pull":
				ui.Text(c, L.ImgPull()).FontSize(sectionSize).Bold()
				ui.Field(c, L.CtrImage(), func() { ui.TextInput(c, &p.in.ref).Placeholder("alpine:3.20").Label(L.CtrImage()).AutoFocus() })
				dialogButtons(c, a, L.ImgPull(), strings.TrimSpace(p.in.ref) != "", func() {
					ref := strings.TrimSpace(p.in.ref)
					a.startJob(L.ImgPull()+" "+ref, func(ctx context.Context, j *jobs.Job) error { return a.k.WSLc.Pull(ctx, ref, j) }, a.loadContainers)
				})
			case "network":
				ui.Text(c, L.NetCreate()).FontSize(sectionSize).Bold()
				ui.Field(c, L.ColName(), func() { ui.TextInput(c, &p.in.network).Label(L.ColName()) })
				ui.Field(c, L.NetSubnet(), func() { ui.TextInput(c, &p.in.subnet).Placeholder("172.30.0.0/16").Label(L.NetSubnet()) })
				dialogButtons(c, a, L.NetCreate(), strings.TrimSpace(p.in.network) != "", func() {
					in := p.in
					a.wslcJob(L.NetCreate()+" "+in.network, "", func(ctx context.Context) error {
						return a.k.WSLc.CreateNetwork(ctx, strings.TrimSpace(in.network), wslc.NetworkOptions{Subnet: strings.TrimSpace(in.subnet)})
					})
				})
			}
		})
	})
	if !open {
		p.dlg = ""
	}
}

// runSpec turns the run dialog into a container spec.
func runSpec(in ctrInput) wslc.ContainerSpec {
	s := wslc.ContainerSpec{Image: strings.TrimSpace(in.image), Name: strings.TrimSpace(in.name)}
	for _, p := range splitList(in.ports) {
		parts := strings.Split(p, ":")
		pb := wslc.PortBinding{ContainerPort: parts[len(parts)-1]}
		if cp, proto, ok := strings.Cut(pb.ContainerPort, "/"); ok {
			pb.ContainerPort, pb.Protocol = cp, proto
		}
		switch len(parts) {
		case 2:
			pb.HostPort = parts[0]
		case 3:
			pb.HostIP, pb.HostPort = parts[0], parts[1]
		}
		s.Ports = append(s.Ports, pb)
	}
	for _, e := range strings.Split(in.env, "\n") {
		if e = strings.TrimSpace(e); e != "" {
			s.Env = append(s.Env, e)
		}
	}
	for _, v := range strings.Split(in.volumes, "\n") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		// "source:target[:ro]"; a Windows source has a drive colon.
		ro := strings.HasSuffix(v, ":ro")
		v = strings.TrimSuffix(v, ":ro")
		i := strings.LastIndex(v, ":")
		if i <= 1 {
			continue
		}
		src, dst := v[:i], v[i+1:]
		kind := "volume"
		if strings.ContainsAny(src, `\/`) {
			kind = "bind"
		}
		s.Mounts = append(s.Mounts, wslc.MountSpec{Type: kind, Source: src, Target: dst, ReadOnly: ro})
	}
	if cmd := strings.TrimSpace(in.command); cmd != "" {
		s.Command = strings.Fields(cmd)
	}
	if in.restart != "" && in.restart != restart.No {
		s.Labels = map[string]string{restart.Label: in.restart}
	}
	return s
}

// splitList splits "a, b c" on commas and spaces.
func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' })
}
