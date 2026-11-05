package compose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// Engine is the part of the wslc adapter the builtin backend uses;
// *wslc.Client implements it and tests substitute a fake.
type Engine interface {
	Info(ctx context.Context) (wslc.Info, error)
	ContainersDetailed(ctx context.Context, filters ...string) ([]wslc.Container, error)
	InspectContainers(ctx context.Context, ids ...string) ([]wslc.Container, error)
	Create(ctx context.Context, s wslc.ContainerSpec) (string, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, timeout int, ids ...string) error
	Restart(ctx context.Context, ids ...string) error
	Remove(ctx context.Context, force, anonVolumes bool, ids ...string) error
	Logs(ctx context.Context, id string, o wslc.LogOptions, w io.Writer) error
	Pull(ctx context.Context, ref string, log io.Writer) error
	Build(ctx context.Context, o wslc.BuildOptions, log io.Writer) error
	InspectImage(ctx context.Context, ref string) (wslc.ImageDetail, error)
	CreateVolume(ctx context.Context, name, driver string, opts, labels map[string]string) error
	Volumes(ctx context.Context, filters ...string) ([]wslc.Volume, error)
	RemoveVolumes(ctx context.Context, names ...string) error
	CreateNetwork(ctx context.Context, name string, o wslc.NetworkOptions) error
	Networks(ctx context.Context) ([]wslc.Network, error)
	RemoveNetworks(ctx context.Context, names ...string) error
	Connect(ctx context.Context, network, id string, aliases ...string) error
}

var _ Engine = (*wslc.Client)(nil)

// Builtin is the Compose backend that translates projects into wslc
// containers, networks and volumes.
type Builtin struct {
	Engine Engine
	// PollInterval spaces depends_on checks; 0 means one second.
	PollInterval time.Duration
}

var _ Backend = (*Builtin)(nil)

// NewBuiltin returns the builtin backend over the real wslc client.
func NewBuiltin() *Builtin { return &Builtin{Engine: wslc.New()} }

// ID returns BackendBuiltin.
func (b *Builtin) ID() BackendID { return BackendBuiltin }

// Detect reports the builtin backend available when wslc answers `info`.
func (b *Builtin) Detect(ctx context.Context) (Availability, error) {
	info, err := b.Engine.Info(ctx)
	if err != nil {
		return Availability{Reason: "WSL Containers (wslc.exe) is not available"}, nil
	}
	return Availability{Available: true, Version: info.Client.Version}, nil
}

// projectFilter is the wslc filter selecting a project's objects.
func projectFilter(name string) string { return "label=" + LabelProject + "=" + name }

// Validate loads the project, translates it and checks that external
// networks and volumes exist.
// Returns: the report; an error only when loading or wslc fails.
func (b *Builtin) Validate(ctx context.Context, p *Project) (Report, error) {
	_, r, err := b.prepare(ctx, p)
	return r, err
}

// prepare loads, translates and checks external resources.
func (b *Builtin) prepare(ctx context.Context, p *Project) (*Plan, Report, error) {
	proj, err := Load(ctx, p)
	if err != nil {
		return nil, Report{}, err
	}
	plan, r := Translate(proj, p)
	if err := b.checkExternal(ctx, plan, &r); err != nil {
		return nil, r, err
	}
	return plan, r, nil
}

// checkExternal reports external networks and volumes that do not exist.
func (b *Builtin) checkExternal(ctx context.Context, plan *Plan, r *Report) error {
	needNets, needVols := false, false
	for _, n := range plan.Networks {
		needNets = needNets || n.External
	}
	for _, v := range plan.Volumes {
		needVols = needVols || (v.External && v.Bind == "")
	}
	if needNets {
		nets, err := b.Engine.Networks(ctx)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		for _, n := range nets {
			have[n.Name] = true
		}
		for _, n := range plan.Networks {
			if n.External && !have[n.Name] {
				r.errorf("", "networks."+n.Key, "external network %q does not exist", n.Name)
			}
		}
	}
	if needVols {
		vols, err := b.Engine.Volumes(ctx)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		for _, v := range vols {
			have[v.Name] = true
		}
		for _, v := range plan.Volumes {
			if v.External && v.Bind == "" && !have[v.Name] {
				r.errorf("", "volumes."+v.Key, "external volume %q does not exist", v.Name)
			}
		}
	}
	return nil
}

// Up creates or updates the project: networks, volumes, images, then
// containers in dependency order; unchanged containers are kept.
//
// Returns: *ValidationError (and no change) when the project has errors.
// Side effects: creates wslc objects labelled with the project; creates
// missing bind-mount folders; pulls or builds images; with RemoveOrphans,
// removes containers of services no longer in the files.
// Notes: ctx cancellation stops between and during wslc calls.
func (b *Builtin) Up(ctx context.Context, p *Project, o UpOptions, progress Progress) error {
	progress.emit("", StepLoad, "reading %d Compose file(s)", len(p.Files))
	plan, r, err := b.prepare(ctx, p)
	if err != nil {
		return err
	}
	for _, w := range r.Warnings {
		progress.emit(w.Service, StepLoad, "warning: %s", w.String())
	}
	if !r.OK() {
		return &ValidationError{Report: r}
	}
	return b.apply(ctx, plan, o, progress)
}

// apply executes a validated plan.
func (b *Builtin) apply(ctx context.Context, plan *Plan, o UpOptions, progress Progress) error {
	if err := b.ensureNetworks(ctx, plan, progress); err != nil {
		return err
	}
	if err := b.ensureVolumes(ctx, plan, progress); err != nil {
		return err
	}
	for _, s := range plan.Services {
		for _, d := range s.BindDirs {
			if err := os.MkdirAll(d, 0o755); err != nil {
				return fmt.Errorf("compose: %s: create bind folder: %w", s.Name, err)
			}
		}
	}
	imageIDs, err := b.ensureImages(ctx, plan, o, progress)
	if err != nil {
		return err
	}
	actual, err := b.Engine.ContainersDetailed(ctx, projectFilter(plan.Project))
	if err != nil {
		return err
	}
	acts := Diff(plan, actual, DiffOptions{RemoveOrphans: o.RemoveOrphans, ForceRecreate: o.ForceRecreate, ImageIDs: imageIDs})

	// Removals first (orphans, scale-down, duplicates), so names are free.
	for _, a := range acts {
		if a.Kind == ActRemove {
			step := StepRemove
			if a.Orphan {
				step = StepOrphan
			}
			progress.emit(a.Service, step, "removing %s (%s)", a.Name, a.Reason)
			if err := b.Engine.Remove(ctx, true, false, a.ID); err != nil {
				return err
			}
		} else if a.Orphan {
			progress.emit(a.Service, StepOrphan, "%s belongs to a service no longer in the Compose files", a.Name)
		}
	}
	ids := map[string][]string{}
	wait := time.Duration(o.WaitTimeout) * time.Second
	if wait <= 0 {
		wait = 2 * time.Minute
	}
	for _, s := range plan.Services {
		for _, d := range s.DependsOn {
			if plan.Service(d.Service) == nil {
				continue
			}
			if err := b.waitFor(ctx, s.Name, d, ids[d.Service], wait, progress); err != nil {
				return err
			}
		}
		for _, a := range acts {
			if a.Service != s.Name || a.Orphan || a.Kind == ActRemove {
				continue
			}
			id, err := b.do(ctx, a, progress)
			if err != nil {
				return fmt.Errorf("compose: %s: %w", s.Name, err)
			}
			ids[s.Name] = append(ids[s.Name], id)
		}
	}
	return nil
}

// do executes one container action and returns the container ID.
func (b *Builtin) do(ctx context.Context, a Action, progress Progress) (string, error) {
	switch a.Kind {
	case ActKeep:
		progress.emit(a.Service, StepKeep, "%s is up to date", a.Name)
		return a.ID, nil
	case ActStart:
		progress.emit(a.Service, StepStart, "starting %s", a.Name)
		return a.ID, b.Engine.Start(ctx, a.ID)
	case ActRecreate:
		progress.emit(a.Service, StepRecreate, "recreating %s (%s)", a.Name, a.Reason)
		if err := b.Engine.Stop(ctx, -1, a.ID); err != nil {
			return "", err
		}
		if err := b.Engine.Remove(ctx, true, false, a.ID); err != nil {
			return "", err
		}
	default:
		progress.emit(a.Service, StepCreate, "creating %s", a.Want.Name)
	}
	id, err := b.Engine.Create(ctx, a.Want.Spec)
	if err != nil {
		return "", err
	}
	for _, at := range a.Want.Extra {
		if err := b.Engine.Connect(ctx, at.Network, id, at.Aliases...); err != nil {
			return id, err
		}
	}
	progress.emit(a.Service, StepStart, "starting %s", a.Want.Name)
	return id, b.Engine.Start(ctx, id)
}

// ensureNetworks creates the project's missing networks.
func (b *Builtin) ensureNetworks(ctx context.Context, plan *Plan, progress Progress) error {
	if len(plan.Networks) == 0 {
		return nil
	}
	nets, err := b.Engine.Networks(ctx)
	if err != nil {
		return err
	}
	have := map[string]wslc.Network{}
	for _, n := range nets {
		have[n.Name] = n
	}
	for _, n := range plan.Networks {
		if n.External {
			continue
		}
		if h, ok := have[n.Name]; ok {
			if h.Labels[LabelProject] != plan.Project {
				progress.emit("", StepNetwork, "warning: network %s exists but was not created for this project; using it", n.Name)
			}
			continue
		}
		progress.emit("", StepNetwork, "creating network %s", n.Name)
		if err := b.Engine.CreateNetwork(ctx, n.Name, n.Options); err != nil {
			return err
		}
	}
	return nil
}

// ensureVolumes creates the project's missing named volumes.
func (b *Builtin) ensureVolumes(ctx context.Context, plan *Plan, progress Progress) error {
	need := false
	for _, v := range plan.Volumes {
		need = need || (!v.External && v.Bind == "")
	}
	if !need {
		return nil
	}
	vols, err := b.Engine.Volumes(ctx)
	if err != nil {
		return err
	}
	have := map[string]wslc.Volume{}
	for _, v := range vols {
		have[v.Name] = v
	}
	for _, v := range plan.Volumes {
		if v.External || v.Bind != "" {
			continue
		}
		if h, ok := have[v.Name]; ok {
			if h.Labels[LabelProject] != plan.Project {
				progress.emit("", StepVolume, "warning: volume %s exists but was not created for this project; using it", v.Name)
			}
			continue
		}
		progress.emit("", StepVolume, "creating volume %s", v.Name)
		if err := b.Engine.CreateVolume(ctx, v.Name, v.Driver, v.Opts, v.Labels); err != nil {
			return err
		}
	}
	return nil
}

// ensureImages pulls or builds each service's image per its pull policy
// and returns the image IDs by service.
func (b *Builtin) ensureImages(ctx context.Context, plan *Plan, o UpOptions, progress Progress) (map[string]string, error) {
	log := o.Log
	if log == nil {
		log = io.Discard
	}
	ids := map[string]string{}
	done := map[string]string{}
	for _, s := range plan.Services {
		if id, ok := done[s.Image]; ok {
			ids[s.Name] = id
			continue
		}
		img, err := b.Engine.InspectImage(ctx, s.Image)
		missing := errors.Is(err, wslc.ErrNotFound)
		if err != nil && !missing {
			return nil, err
		}
		build := s.Build != nil && (o.Build || s.PullPolicy == "build" || missing)
		pull := !build && (s.PullPolicy == "always" || missing)
		switch {
		case build:
			progress.emit(s.Name, StepBuild, "building %s", s.Image)
			if err := b.Engine.Build(ctx, *s.Build, log); err != nil {
				return nil, fmt.Errorf("compose: %s: build: %w", s.Name, err)
			}
		case pull && s.PullPolicy == "never":
			return nil, fmt.Errorf("compose: %s: image %s is missing and pull_policy is never", s.Name, s.Image)
		case pull:
			progress.emit(s.Name, StepPull, "pulling %s", s.Image)
			if err := b.Engine.Pull(ctx, s.Image, log); err != nil {
				return nil, fmt.Errorf("compose: %s: pull: %w", s.Name, err)
			}
		}
		if build || pull {
			if img, err = b.Engine.InspectImage(ctx, s.Image); err != nil {
				return nil, fmt.Errorf("compose: %s: image %s: %w", s.Name, s.Image, err)
			}
		}
		done[s.Image] = img.ID
		ids[s.Name] = img.ID
	}
	return ids, nil
}

// waitFor blocks until a dependency meets its depends_on condition.
// Returns: an error when it fails (unhealthy, non-zero exit), on timeout
// or when ctx ends.
func (b *Builtin) waitFor(ctx context.Context, service string, d Dependency, ids []string, timeout time.Duration, progress Progress) error {
	if d.Condition == "service_started" || len(ids) == 0 {
		return nil
	}
	progress.emit(service, StepWait, "waiting for %s (%s)", d.Service, d.Condition)
	poll := b.PollInterval
	if poll <= 0 {
		poll = time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		cs, err := b.Engine.InspectContainers(ctx, ids...)
		if err != nil {
			return err
		}
		ready := len(cs) == len(ids)
		for _, c := range cs {
			ok, err := conditionMet(d, c)
			if err != nil {
				return fmt.Errorf("compose: %s: dependency %s: %w", service, d.Service, err)
			}
			ready = ready && ok
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("compose: %s: timed out waiting for %s (%s)", service, d.Service, d.Condition)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// conditionMet checks one container against a depends_on condition.
func conditionMet(d Dependency, c wslc.Container) (bool, error) {
	switch d.Condition {
	case "service_healthy":
		if c.State.Health == nil {
			if !c.State.Running && c.State.Status == "exited" {
				return false, fmt.Errorf("%s exited with code %d", c.DisplayName(), c.State.ExitCode)
			}
			return false, nil
		}
		switch c.State.Health.Status {
		case "healthy":
			return true, nil
		case "unhealthy":
			return false, fmt.Errorf("%s is unhealthy", c.DisplayName())
		}
		return false, nil
	case "service_completed_successfully":
		if c.State.Status != "exited" {
			return false, nil
		}
		if c.State.ExitCode != 0 {
			return false, fmt.Errorf("%s exited with code %d", c.DisplayName(), c.State.ExitCode)
		}
		return true, nil
	}
	return true, nil
}

// containers returns the project's containers, optionally only some
// services', sorted by service and number.
func (b *Builtin) containers(ctx context.Context, p *Project, services []string) ([]wslc.Container, error) {
	cs, err := b.Engine.ContainersDetailed(ctx, projectFilter(p.Name))
	if err != nil {
		return nil, err
	}
	want := keySet(services...)
	var res []wslc.Container
	for _, c := range cs {
		if len(services) == 0 || want[labelsOf(c)[LabelService]] {
			res = append(res, c)
		}
	}
	sort.SliceStable(res, func(i, j int) bool {
		li, lj := labelsOf(res[i]), labelsOf(res[j])
		if li[LabelService] != lj[LabelService] {
			return li[LabelService] < lj[LabelService]
		}
		ni, _ := strconv.Atoi(li[LabelNumber])
		nj, _ := strconv.Atoi(lj[LabelNumber])
		return ni < nj
	})
	return res, nil
}

// orderByDependencies sorts containers so dependencies come first, when
// the Compose files can still be loaded; otherwise the order is kept.
func (b *Builtin) orderByDependencies(ctx context.Context, p *Project, cs []wslc.Container) []wslc.Container {
	proj, err := Load(ctx, p)
	if err != nil {
		return cs
	}
	order, err := topoOrder(proj.Services)
	if err != nil {
		return cs
	}
	rank := map[string]int{}
	for i, n := range order {
		rank[n] = i + 1
	}
	sort.SliceStable(cs, func(i, j int) bool {
		return rank[labelsOf(cs[i])[LabelService]] < rank[labelsOf(cs[j])[LabelService]]
	})
	return cs
}

// Down stops and removes the project's containers and networks; volumes
// only with RemoveVolumes.
// Side effects: removes every wslc container and network labelled with the
// project, whichever Compose files still exist.
func (b *Builtin) Down(ctx context.Context, p *Project, o DownOptions, progress Progress) error {
	cs, err := b.containers(ctx, p, nil)
	if err != nil {
		return err
	}
	var running, all []string
	for _, c := range cs {
		all = append(all, c.ID)
		if c.State.Running {
			running = append(running, c.ID)
			progress.emit(labelsOf(c)[LabelService], StepStop, "stopping %s", c.DisplayName())
		}
	}
	if len(running) > 0 {
		if err := b.Engine.Stop(ctx, -1, running...); err != nil {
			return err
		}
	}
	for _, c := range cs {
		progress.emit(labelsOf(c)[LabelService], StepRemove, "removing %s", c.DisplayName())
	}
	if len(all) > 0 {
		if err := b.Engine.Remove(ctx, true, false, all...); err != nil {
			return err
		}
	}
	nets, err := b.Engine.Networks(ctx)
	if err != nil {
		return err
	}
	var rm []string
	for _, n := range nets {
		if n.Labels[LabelProject] == p.Name && !n.Builtin() {
			progress.emit("", StepNetwork, "removing network %s", n.Name)
			rm = append(rm, n.Name)
		}
	}
	if len(rm) > 0 {
		if err := b.Engine.RemoveNetworks(ctx, rm...); err != nil {
			return err
		}
	}
	if !o.RemoveVolumes {
		return nil
	}
	vols, err := b.Engine.Volumes(ctx, projectFilter(p.Name))
	if err != nil {
		return err
	}
	var vrm []string
	for _, v := range vols {
		if v.Labels[LabelProject] == p.Name {
			progress.emit("", StepVolume, "removing volume %s", v.Name)
			vrm = append(vrm, v.Name)
		}
	}
	if len(vrm) > 0 {
		return b.Engine.RemoveVolumes(ctx, vrm...)
	}
	return nil
}

// Start starts the services' stopped containers, dependencies first.
func (b *Builtin) Start(ctx context.Context, p *Project, services []string, progress Progress) error {
	cs, err := b.containers(ctx, p, services)
	if err != nil {
		return err
	}
	for _, c := range b.orderByDependencies(ctx, p, cs) {
		if c.State.Running {
			continue
		}
		progress.emit(labelsOf(c)[LabelService], StepStart, "starting %s", c.DisplayName())
		if err := b.Engine.Start(ctx, c.ID); err != nil {
			return err
		}
	}
	return nil
}

// Stop stops the services' running containers, dependents first.
func (b *Builtin) Stop(ctx context.Context, p *Project, services []string, progress Progress) error {
	cs, err := b.containers(ctx, p, services)
	if err != nil {
		return err
	}
	cs = b.orderByDependencies(ctx, p, cs)
	for i := len(cs) - 1; i >= 0; i-- {
		c := cs[i]
		if !c.State.Running {
			continue
		}
		progress.emit(labelsOf(c)[LabelService], StepStop, "stopping %s", c.DisplayName())
		if err := b.Engine.Stop(ctx, -1, c.ID); err != nil {
			return err
		}
	}
	return nil
}

// Restart restarts the services' containers, dependencies first.
func (b *Builtin) Restart(ctx context.Context, p *Project, services []string, progress Progress) error {
	cs, err := b.containers(ctx, p, services)
	if err != nil {
		return err
	}
	for _, c := range b.orderByDependencies(ctx, p, cs) {
		progress.emit(labelsOf(c)[LabelService], StepRestart, "restarting %s", c.DisplayName())
		if err := b.Engine.Restart(ctx, c.ID); err != nil {
			return err
		}
	}
	return nil
}

// Ps lists the project's containers as wslc reports them.
func (b *Builtin) Ps(ctx context.Context, p *Project) ([]ContainerStatus, error) {
	cs, err := b.containers(ctx, p, nil)
	if err != nil {
		return nil, err
	}
	res := make([]ContainerStatus, 0, len(cs))
	for _, c := range cs {
		l := labelsOf(c)
		n, _ := strconv.Atoi(l[LabelNumber])
		st := ContainerStatus{Service: l[LabelService], Name: c.DisplayName(), ID: c.ID, Number: n,
			Image: c.Config.Image, State: c.State.Status, Exit: c.State.ExitCode, Backend: l[LabelBackend]}
		if c.State.Health != nil {
			st.Health = c.State.Health.Status
		}
		res = append(res, st)
	}
	return res, nil
}

// Logs streams the services' logs to w, each line prefixed with
// "<container> | ". With Follow it returns when ctx ends.
func (b *Builtin) Logs(ctx context.Context, p *Project, o LogsOptions, w io.Writer) error {
	cs, err := b.containers(ctx, p, o.Services)
	if err != nil {
		return err
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make([]error, len(cs))
	for i, c := range cs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pw := &prefixWriter{mu: &mu, w: w, prefix: c.DisplayName() + " | "}
			err := b.Engine.Logs(ctx, c.ID, wslc.LogOptions{Follow: o.Follow, Tail: o.Tail, Timestamps: o.Timestamps}, pw)
			pw.Flush()
			if err != nil && ctx.Err() == nil {
				errs[i] = fmt.Errorf("%s: %w", c.DisplayName(), err)
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// prefixWriter writes complete lines to a shared writer with a prefix.
type prefixWriter struct {
	mu     *sync.Mutex
	w      io.Writer
	prefix string
	buf    []byte
}

// Write buffers p and writes every complete line under the shared lock.
func (pw *prefixWriter) Write(p []byte) (int, error) {
	pw.buf = append(pw.buf, p...)
	for {
		i := bytes.IndexByte(pw.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		if err := pw.line(pw.buf[:i+1]); err != nil {
			return len(p), err
		}
		pw.buf = pw.buf[i+1:]
	}
}

// Flush writes a final line without a newline.
func (pw *prefixWriter) Flush() {
	if len(pw.buf) > 0 {
		pw.line(append(pw.buf, '\n'))
		pw.buf = nil
	}
}

// line writes one prefixed line.
func (pw *prefixWriter) line(l []byte) error {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	_, err := pw.w.Write(append([]byte(pw.prefix), l...))
	return err
}
