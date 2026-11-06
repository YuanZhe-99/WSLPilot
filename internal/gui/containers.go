package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/configdoc"
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

	// sessions are the WSLc sessions running; choice is the selected name.
	// client addresses it (nil = the default CLI session); it is read by
	// background jobs, hence atomic.
	sessions []string
	choice   string
	client   atomic.Pointer[wslc.Client]

	storageBytes int64 // the CLI session's storage.vhdx
	logQuery     string

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
	// replace is the ID of the container the dialog recreates ("" = new).
	replace, replaceName string
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

// wc is the wslc client of the session the page shows.
func (a *App) wc() *wslc.Client {
	if c := a.ctr.client.Load(); c != nil {
		return c
	}
	return a.k.WSLc
}

// selectSession switches the page to another WSLc session.
func (a *App) selectSession(name string) {
	if name == "" || wslc.IsCLISession(name) {
		a.ctr.client.Store(nil)
	} else {
		a.ctr.client.Store(a.k.WSLc.WithSession(name))
	}
	a.closeLogs()
	a.ctr.csel, a.ctr.isel, a.ctr.nsel = -1, -1, -1
	a.loadContainers()
}

// loadContainers reads containers, images and networks.
func (a *App) loadContainers() {
	p := &a.ctr
	if p.loading {
		return
	}
	p.loading = true
	a.async(func(ctx context.Context) func() {
		var sessions []string
		if info, ierr := a.k.WSLc.Info(ctx); ierr == nil {
			for _, s := range info.Server.Sessions {
				sessions = append(sessions, s.Name)
			}
		}
		list, err := a.wc().Containers(ctx)
		ids := make([]string, 0, len(list))
		for _, s := range list {
			ids = append(ids, s.ID)
		}
		detail := map[string]wslc.Container{}
		if ds, derr := a.wc().InspectContainers(ctx, ids...); derr == nil {
			for _, d := range ds {
				detail[shortID(d.ID)] = d
			}
		}
		stats := map[string]wslc.Stats{}
		if ss, serr := a.wc().Stats(ctx); serr == nil {
			for _, s := range ss {
				stats[shortID(s.ID)] = s
			}
		}
		imgs, _ := a.wc().Images(ctx)
		nets, _ := a.wc().Networks(ctx)
		wt := wtprofile.Installed()
		var storage int64
		for _, s := range sessions {
			if wslc.IsCLISession(s) {
				storage = sessionStorageSize(s)
			}
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Names < list[j].Names })
		return func() {
			p.loading, p.loaded, p.err = false, true, err
			p.list, p.detail, p.stats, p.images, p.networks, p.wt = list, detail, stats, imgs, nets, wt
			p.sessions, p.storageBytes = sessions, storage
			if p.choice == "" {
				for _, s := range sessions {
					if wslc.IsCLISession(s) {
						p.choice = s
					}
				}
			}
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
	go a.wc().Logs(ctx, id, wslc.LogOptions{Follow: true, Tail: 500, Timestamps: true}, buf)
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
				if p.in.replace != "" {
					ui.Text(c, L.CtrEditTitle(p.in.replaceName)).FontSize(16).Bold()
					ui.Text(c, L.CtrEditWarning()).TextColor(c.Theme().Warning)
				} else {
					ui.Text(c, L.CtrRunTitle()).FontSize(16).Bold()
				}
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
					if p.in.replace != "" {
						a.recreate(p.in.replace, spec)
						return
					}
					a.wslcJob(L.CtrRun()+" "+spec.Image, "", func(ctx context.Context) error {
						id, err := a.wc().Create(ctx, spec)
						if err != nil {
							return err
						}
						return a.wc().Start(ctx, id)
					})
				})
			case "pull":
				ui.Text(c, L.ImgPull()).FontSize(16).Bold()
				ui.Field(c, L.CtrImage(), func() { ui.TextInput(c, &p.in.ref).Placeholder("alpine:3.20").Label(L.CtrImage()).AutoFocus() })
				dialogButtons(c, a, L.ImgPull(), strings.TrimSpace(p.in.ref) != "", func() {
					ref := strings.TrimSpace(p.in.ref)
					a.startJob(L.ImgPull()+" "+ref, func(ctx context.Context, j *jobs.Job) error { return a.wc().Pull(ctx, ref, j) }, a.loadContainers)
				})
			case "network":
				ui.Text(c, L.NetCreate()).FontSize(16).Bold()
				ui.Field(c, L.ColName(), func() { ui.TextInput(c, &p.in.network).Label(L.ColName()) })
				ui.Field(c, L.NetSubnet(), func() { ui.TextInput(c, &p.in.subnet).Placeholder("172.30.0.0/16").Label(L.NetSubnet()) })
				dialogButtons(c, a, L.NetCreate(), strings.TrimSpace(p.in.network) != "", func() {
					in := p.in
					a.wslcJob(L.NetCreate()+" "+in.network, "", func(ctx context.Context) error {
						return a.wc().CreateNetwork(ctx, strings.TrimSpace(in.network), wslc.NetworkOptions{Subnet: strings.TrimSpace(in.subnet)})
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

// editInput fills the run dialog from a container's inspect data.
// Notes: environment variables the image sets (such as PATH) are shown
// too; they are passed again unchanged.
func editInput(d wslc.Container) ctrInput {
	in := ctrInput{image: d.Config.Image, name: d.DisplayName(), start: d.State.Running, replace: d.ID, replaceName: d.DisplayName(), restart: restart.No}
	in.env = strings.Join(d.Config.Env, "\n")
	in.command = strings.Join(d.Config.Cmd, " ")
	var ports []string
	for cport, binds := range d.Ports {
		for _, b := range binds {
			p := b.HostPort + ":" + cport
			if b.HostIP != "" {
				p = b.HostIP + ":" + p
			}
			ports = append(ports, strings.TrimSuffix(p, "/tcp"))
		}
	}
	sort.Strings(ports)
	in.ports = strings.Join(ports, ", ")
	var vols []string
	for _, m := range d.Mounts {
		src := m.Source
		if m.Type == "volume" {
			src = m.Name
		}
		if m.Type == "tmpfs" || src == "" {
			continue
		}
		v := src + ":" + m.Destination
		if !m.ReadWrite {
			v += ":ro"
		}
		vols = append(vols, v)
	}
	in.volumes = strings.Join(vols, "\n")
	if pol := d.Labels[restart.Label]; pol != "" {
		in.restart = pol
	}
	return in
}

// recreate replaces a container by one made from spec. The new spec is
// tried first under a temporary name, so an invalid configuration never
// costs the old container.
func (a *App) recreate(oldID string, spec wslc.ContainerSpec) {
	a.wslcJob(L.CtrEdit()+" "+spec.Name, "wslc.container.recreate", func(ctx context.Context) error {
		probe := spec
		probe.Name = spec.Name + "-wslpilot-check"
		probe.Ports = nil // the old container still holds them
		id, err := a.wc().Create(ctx, probe)
		if err != nil {
			return err
		}
		a.wc().Remove(ctx, true, false, id)
		// Safety copy of the container's own files (wslc exports only
		// stopped containers).
		if err := a.wc().Stop(ctx, -1, oldID); err != nil {
			return err
		}
		dir := filepath.Join(a.backupDir(), "Safety")
		os.MkdirAll(dir, 0o755)
		safety := filepath.Join(dir, backup.SafeName("container-"+spec.Name)+"-"+time.Now().Format("20060102-150405")+".tar")
		f, err := os.Create(safety)
		if err != nil {
			return err
		}
		err = a.wc().ExportContainer(ctx, oldID, f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(safety)
			return fmt.Errorf("%s: %w", L.JobBackupFailedStop(), err)
		}
		if err := a.wc().Remove(ctx, true, false, oldID); err != nil {
			return err
		}
		id, err = a.wc().Create(ctx, spec)
		if err != nil {
			return err
		}
		return a.wc().Start(ctx, id)
	})
}

// sessionStorageSize is the size of a session's storage.vhdx under the
// storage path of settings.yaml (default %LOCALAPPDATA%).
func sessionStorageSize(session string) int64 {
	base := os.Getenv("LOCALAPPDATA")
	if snap, err := configdoc.ReadFile(filepath.Join(base, "wslc", "settings.yaml")); err == nil {
		if v, ok := configdoc.ParseYAML(snap.Text).Get("session.storagePath"); ok && v != "default" && v != "" {
			base = strings.Trim(v, `"'`)
		}
	}
	st, err := os.Stat(filepath.Join(base, "wslc", "sessions", session, "storage.vhdx"))
	if err != nil {
		return 0
	}
	return st.Size()
}
