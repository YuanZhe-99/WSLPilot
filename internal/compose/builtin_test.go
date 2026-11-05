package compose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/YuanZhe-99/WSLPilot/internal/store"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// fakeEngine is an in-memory wslc: containers, networks, volumes, images.
type fakeEngine struct {
	mu         sync.Mutex
	containers map[string]*wslc.Container
	networks   map[string]wslc.Network
	volumes    map[string]wslc.Volume
	images     map[string]string // ref -> ID
	calls      []string
	next       int
	// health is the health status reported for running containers with a
	// healthcheck.
	health string
}

// newFake returns an empty fake engine that knows alpine:3.20.
func newFake() *fakeEngine {
	return &fakeEngine{containers: map[string]*wslc.Container{}, networks: map[string]wslc.Network{},
		volumes: map[string]wslc.Volume{}, images: map[string]string{"alpine:3.20": "sha256:alpine"}, health: "healthy"}
}

func (f *fakeEngine) log(format string, a ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, a...))
}

func (f *fakeEngine) Info(context.Context) (wslc.Info, error) {
	var i wslc.Info
	i.Client.Version = "3.0.3.0"
	return i, nil
}

func (f *fakeEngine) ContainersDetailed(_ context.Context, filters ...string) ([]wslc.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var res []wslc.Container
	for _, c := range f.containers {
		ok := true
		for _, flt := range filters {
			kv := strings.TrimPrefix(flt, "label=")
			k, v, _ := strings.Cut(kv, "=")
			ok = ok && c.Config.Labels[k] == v
		}
		if ok {
			res = append(res, *c)
		}
	}
	slices.SortFunc(res, func(a, b wslc.Container) int { return strings.Compare(a.Name, b.Name) })
	return res, nil
}

func (f *fakeEngine) InspectContainers(_ context.Context, ids ...string) ([]wslc.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var res []wslc.Container
	for _, id := range ids {
		c, ok := f.containers[id]
		if !ok {
			return nil, wslc.ErrNotFound
		}
		res = append(res, *c)
	}
	return res, nil
}

func (f *fakeEngine) Create(_ context.Context, s wslc.ContainerSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.containers {
		if c.DisplayName() == s.Name {
			return "", fmt.Errorf("name %s in use", s.Name)
		}
	}
	f.next++
	id := fmt.Sprintf("id%02d", f.next)
	c := &wslc.Container{ID: id, Name: "/" + s.Name, ImageID: f.images[s.Image]}
	c.Config.Image = s.Image
	c.Config.Labels = s.Labels
	c.Config.Env = s.Env
	c.State.Status = "created"
	if s.Health != nil && !s.Health.Disable {
		c.Config.Healthcheck = &struct {
			Test []string `json:"Test"`
		}{Test: []string{"CMD-SHELL", s.Health.Cmd}}
	}
	f.containers[id] = c
	f.log("create %s", s.Name)
	return id, nil
}

func (f *fakeEngine) Start(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.containers[id]
	c.State.Running, c.State.Status = true, "running"
	if c.Config.Healthcheck != nil {
		c.State.Health = &struct {
			Status string `json:"Status"`
		}{Status: f.health}
	}
	f.log("start %s", c.DisplayName())
	return nil
}

func (f *fakeEngine) Stop(_ context.Context, _ int, ids ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range ids {
		c := f.containers[id]
		c.State.Running, c.State.Status = false, "exited"
		f.log("stop %s", c.DisplayName())
	}
	return nil
}

func (f *fakeEngine) Restart(_ context.Context, ids ...string) error {
	for _, id := range ids {
		f.log("restart %s", f.containers[id].DisplayName())
	}
	return nil
}

func (f *fakeEngine) Remove(_ context.Context, _, _ bool, ids ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range ids {
		f.log("remove %s", f.containers[id].DisplayName())
		delete(f.containers, id)
	}
	return nil
}

func (f *fakeEngine) Logs(_ context.Context, id string, _ wslc.LogOptions, w io.Writer) error {
	_, err := io.WriteString(w, "hello from "+id+"\npartial")
	return err
}

func (f *fakeEngine) Pull(_ context.Context, ref string, _ io.Writer) error {
	f.log("pull %s", ref)
	f.images[ref] = "sha256:" + ref
	return nil
}

func (f *fakeEngine) Build(_ context.Context, o wslc.BuildOptions, _ io.Writer) error {
	f.log("build %s", o.Tags[0])
	f.images[o.Tags[0]] = "sha256:built-" + o.Tags[0]
	return nil
}

func (f *fakeEngine) InspectImage(_ context.Context, ref string) (wslc.ImageDetail, error) {
	id, ok := f.images[ref]
	if !ok {
		return wslc.ImageDetail{}, wslc.ErrNotFound
	}
	return wslc.ImageDetail{ID: id}, nil
}

func (f *fakeEngine) CreateVolume(_ context.Context, name, driver string, _, labels map[string]string) error {
	f.volumes[name] = wslc.Volume{Name: name, Driver: driver, Labels: labels}
	f.log("volume create %s", name)
	return nil
}

func (f *fakeEngine) Volumes(_ context.Context, filters ...string) ([]wslc.Volume, error) {
	var res []wslc.Volume
	for _, v := range f.volumes {
		res = append(res, v)
	}
	return res, nil
}

func (f *fakeEngine) RemoveVolumes(_ context.Context, names ...string) error {
	for _, n := range names {
		delete(f.volumes, n)
		f.log("volume remove %s", n)
	}
	return nil
}

func (f *fakeEngine) CreateNetwork(_ context.Context, name string, o wslc.NetworkOptions) error {
	f.networks[name] = wslc.Network{Name: name, Labels: o.Labels}
	f.log("network create %s", name)
	return nil
}

func (f *fakeEngine) Networks(context.Context) ([]wslc.Network, error) {
	res := []wslc.Network{{Name: "bridge"}}
	for _, n := range f.networks {
		res = append(res, n)
	}
	return res, nil
}

func (f *fakeEngine) RemoveNetworks(_ context.Context, names ...string) error {
	for _, n := range names {
		delete(f.networks, n)
		f.log("network remove %s", n)
	}
	return nil
}

func (f *fakeEngine) Connect(_ context.Context, network, id string, aliases ...string) error {
	f.log("connect %s %s", network, f.containers[id].DisplayName())
	return nil
}

// takeCalls returns and clears the recorded calls.
func (f *fakeEngine) takeCalls() []string {
	c := f.calls
	f.calls = nil
	return c
}

const twoServices = `
services:
  db:
    image: alpine:3.20
    command: sleep infinity
    healthcheck:
      test: ["CMD", "true"]
      interval: 1s
    volumes: [data:/data]
    networks: [back]
  app:
    image: alpine:3.20
    command: sleep infinity
    environment:
      MODE: "%s"
    depends_on:
      db: {condition: service_healthy}
    networks: [back]
volumes:
  data: {}
networks:
  back: {}
`

// writeProject writes the two-service file with a MODE value.
func writeProject(t *testing.T, file, mode string) {
	t.Helper()
	if err := os.WriteFile(file, []byte(fmt.Sprintf(twoServices, mode)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltinUpIdempotentRecreateDown(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	b := &Builtin{Engine: f, PollInterval: 1}
	file := filepath.Join(t.TempDir(), "compose.yaml")
	writeProject(t, file, "a")
	p := metaFor(t, "demo", file)

	var events []Event
	if err := b.Up(ctx, p, UpOptions{}, func(e Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	want := []string{"network create demo_back", "volume create demo_data", "create demo-db-1", "start demo-db-1", "create demo-app-1", "start demo-app-1"}
	if got := f.takeCalls(); !slices.Equal(got, want) {
		t.Errorf("first up:\n got %v\nwant %v", got, want)
	}
	if !slices.ContainsFunc(events, func(e Event) bool { return e.Step == StepWait && e.Service == "app" }) {
		t.Error("no wait event for service_healthy")
	}

	if err := b.Up(ctx, p, UpOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.takeCalls(); len(got) != 0 {
		t.Errorf("second up changed something: %v", got)
	}

	writeProject(t, file, "b")
	if err := b.Up(ctx, p, UpOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	want = []string{"stop demo-app-1", "remove demo-app-1", "create demo-app-1", "start demo-app-1"}
	if got := f.takeCalls(); !slices.Equal(got, want) {
		t.Errorf("env change:\n got %v\nwant %v", got, want)
	}

	ps, err := b.Ps(ctx, p)
	if err != nil || len(ps) != 2 || ps[0].Service != "app" || ps[1].Health != "healthy" || ps[0].Backend != "builtin" {
		t.Errorf("ps = %+v, %v", ps, err)
	}

	var logs bytes.Buffer
	if err := b.Logs(ctx, p, LogsOptions{Services: []string{"db"}}, &logs); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(logs.String(), "demo-db-1 | hello from ") || !strings.HasSuffix(logs.String(), "demo-db-1 | partial\n") {
		t.Errorf("logs = %q", logs.String())
	}

	if err := b.Down(ctx, p, DownOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.containers) != 0 || len(f.networks) != 0 {
		t.Errorf("down left %v %v", f.containers, f.networks)
	}
	if _, ok := f.volumes["demo_data"]; !ok {
		t.Error("down removed the volume without RemoveVolumes")
	}
	if err := b.Down(ctx, p, DownOptions{RemoveVolumes: true}, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.volumes) != 0 {
		t.Errorf("volumes left: %v", f.volumes)
	}
}

func TestBuiltinUpRefusesInvalid(t *testing.T) {
	f := newFake()
	b := &Builtin{Engine: f}
	file := filepath.Join(t.TempDir(), "compose.yaml")
	os.WriteFile(file, []byte("services:\n  s:\n    image: alpine:3.20\n    privileged: true\n"), 0o644)
	err := b.Up(context.Background(), metaFor(t, "demo", file), UpOptions{}, nil)
	var ve *ValidationError
	if !errors.As(err, &ve) || !hasIssue(ve.Report.Errors, "s", "privileged") {
		t.Fatalf("err = %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("invalid project touched wslc: %v", f.calls)
	}
}

func TestBuiltinUnhealthyDependencyFails(t *testing.T) {
	f := newFake()
	f.health = "unhealthy"
	b := &Builtin{Engine: f, PollInterval: 1}
	file := filepath.Join(t.TempDir(), "compose.yaml")
	writeProject(t, file, "a")
	err := b.Up(context.Background(), metaFor(t, "demo", file), UpOptions{}, nil)
	if err == nil || !strings.Contains(err.Error(), "unhealthy") {
		t.Fatalf("err = %v", err)
	}
	for _, c := range f.calls {
		if c == "create demo-app-1" {
			t.Error("dependent created although its dependency is unhealthy")
		}
	}
}

func TestBuiltinBuildAndExternal(t *testing.T) {
	f := newFake()
	b := &Builtin{Engine: f}
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	os.WriteFile(file, []byte("services:\n  w:\n    build: .\n    networks: [ext]\nnetworks:\n  ext:\n    external: true\n    name: shared\n"), 0o644)
	p := metaFor(t, "demo", file)
	r, err := b.Validate(context.Background(), p)
	if err != nil || !hasIssue(r.Errors, "", "networks.ext") {
		t.Fatalf("missing external network not reported: %v %v", r.Errors, err)
	}
	f.networks["shared"] = wslc.Network{Name: "shared"}
	if err := b.Up(context.Background(), p, UpOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.takeCalls(); !slices.Equal(got, []string{"build demo-w", "create demo-w-1", "start demo-w-1"}) {
		t.Errorf("calls = %v", got)
	}
}

func TestBuiltinOrphans(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	b := &Builtin{Engine: f}
	file := filepath.Join(t.TempDir(), "compose.yaml")
	os.WriteFile(file, []byte("services:\n  a:\n    image: alpine:3.20\n  b:\n    image: alpine:3.20\n"), 0o644)
	p := metaFor(t, "demo", file)
	if err := b.Up(ctx, p, UpOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(file, []byte("services:\n  a:\n    image: alpine:3.20\n"), 0o644)
	f.takeCalls()
	if err := b.Up(ctx, p, UpOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.takeCalls(); len(got) != 0 {
		t.Errorf("orphan removed without the option: %v", got)
	}
	if err := b.Up(ctx, p, UpOptions{RemoveOrphans: true}, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.takeCalls(); !slices.Equal(got, []string{"remove demo-b-1"}) {
		t.Errorf("calls = %v", got)
	}
}

func TestDiff(t *testing.T) {
	plan := &Plan{Project: "demo", Services: []ServicePlan{{Name: "a", Containers: []ContainerPlan{{Name: "demo-a-1", Number: 1, Hash: "h1"}}},
		{Name: "b", Containers: []ContainerPlan{{Name: "demo-b-1", Number: 1, Hash: "h2"}}},
		{Name: "c", Containers: []ContainerPlan{{Name: "demo-c-1", Number: 1, Hash: "h3"}}},
		{Name: "d", Containers: []ContainerPlan{{Name: "demo-d-1", Number: 1, Hash: "h4"}}}}}
	mk := func(name, svc, n, hash string, running bool) wslc.Container {
		c := wslc.Container{ID: name, Name: "/" + name}
		c.Config.Labels = map[string]string{LabelService: svc, LabelNumber: n, LabelConfigHash: hash}
		c.State.Running = running
		return c
	}
	actual := []wslc.Container{
		mk("demo-a-1", "a", "1", "h1", true),
		mk("demo-b-1", "b", "1", "old", true),
		mk("demo-c-1", "c", "1", "h3", false),
		mk("demo-c-2", "c", "2", "h3", true),
		mk("demo-gone-1", "gone", "1", "x", true),
	}
	kinds := func(acts []Action) string {
		var s []string
		for _, a := range acts {
			n := a.Name
			if n == "" {
				n = a.Want.Name
			}
			s = append(s, string(a.Kind)+":"+n)
		}
		return strings.Join(s, " ")
	}
	got := kinds(Diff(plan, actual, DiffOptions{}))
	want := "keep:demo-a-1 recreate:demo-b-1 start:demo-c-1 remove:demo-c-2 create:demo-d-1 keep:demo-gone-1"
	if got != want {
		t.Errorf("diff:\n got %s\nwant %s", got, want)
	}
	got = kinds(Diff(plan, actual, DiffOptions{RemoveOrphans: true, ImageIDs: map[string]string{"a": "sha256:new"}}))
	want = "recreate:demo-a-1 recreate:demo-b-1 start:demo-c-1 remove:demo-c-2 create:demo-d-1 remove:demo-gone-1"
	if got != want {
		t.Errorf("diff with orphans and image:\n got %s\nwant %s", got, want)
	}
}

// recordingBackend records calls for migration tests.
type recordingBackend struct {
	id        BackendID
	available bool
	failUp    bool
	calls     *[]string
}

func (r recordingBackend) ID() BackendID { return r.id }
func (r recordingBackend) Detect(context.Context) (Availability, error) {
	return Availability{Available: r.available}, nil
}
func (r recordingBackend) Validate(context.Context, *Project) (Report, error) {
	*r.calls = append(*r.calls, string(r.id)+" validate")
	return Report{}, nil
}
func (r recordingBackend) Up(_ context.Context, p *Project, _ UpOptions, _ Progress) error {
	*r.calls = append(*r.calls, string(r.id)+" up")
	if r.failUp {
		return errors.New("up failed")
	}
	return nil
}
func (r recordingBackend) Down(_ context.Context, _ *Project, o DownOptions, _ Progress) error {
	if o.RemoveVolumes {
		*r.calls = append(*r.calls, string(r.id)+" down -v")
	} else {
		*r.calls = append(*r.calls, string(r.id)+" down")
	}
	return nil
}
func (r recordingBackend) Start(context.Context, *Project, []string, Progress) error   { return nil }
func (r recordingBackend) Stop(context.Context, *Project, []string, Progress) error    { return nil }
func (r recordingBackend) Restart(context.Context, *Project, []string, Progress) error { return nil }
func (r recordingBackend) Ps(context.Context, *Project) ([]ContainerStatus, error)     { return nil, nil }
func (r recordingBackend) Logs(context.Context, *Project, LogsOptions, io.Writer) error {
	return nil
}

func TestMigrate(t *testing.T) {
	ctx := context.Background()
	projects := Projects{Store: &store.Store{Dir: t.TempDir()}}
	newProj := func() *Project {
		p := metaFor(t, "demo", "testdata/load/compose.yaml")
		if err := projects.Save(p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	var calls []string
	reg := Registry{
		BackendBuiltin: recordingBackend{id: BackendBuiltin, available: true, calls: &calls},
		BackendNative:  recordingBackend{id: BackendNative, available: true, calls: &calls},
	}

	p := newProj()
	if err := Migrate(ctx, reg, projects, p, BackendNative, false, nil); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("err = %v", err)
	}
	if len(calls) != 0 || p.Backend != BackendBuiltin {
		t.Fatalf("unconfirmed migration acted: %v, backend %s", calls, p.Backend)
	}

	if err := Migrate(ctx, reg, projects, p, BackendNative, true, nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"native validate", "builtin down", "native up"}; !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
	saved, _ := projects.Load(p.ID)
	if saved.Backend != BackendNative {
		t.Errorf("saved backend = %s", saved.Backend)
	}

	calls = nil
	reg[BackendNative] = recordingBackend{id: BackendNative, available: true, failUp: true, calls: &calls}
	p = newProj()
	if err := Migrate(ctx, reg, projects, p, BackendNative, true, nil); err == nil {
		t.Fatal("failed up did not fail the migration")
	}
	if want := []string{"native validate", "builtin down", "native up", "native down", "builtin up"}; !slices.Equal(calls, want) {
		t.Errorf("rollback calls = %v, want %v", calls, want)
	}
	if saved, _ := projects.Load(p.ID); saved.Backend != BackendBuiltin || p.Backend != BackendBuiltin {
		t.Errorf("backend changed after a failed migration")
	}
}

func TestMigrateToRealNativeIsUnavailable(t *testing.T) {
	reg := Registry{BackendBuiltin: &Builtin{Engine: newFake()}, BackendNative: &Native{}}
	p := metaFor(t, "demo", "testdata/load/compose.yaml")
	err := Migrate(context.Background(), reg, Projects{Store: &store.Store{Dir: t.TempDir()}}, p, BackendNative, true, nil)
	if err == nil || p.Backend != BackendBuiltin {
		t.Fatalf("err = %v backend %s", err, p.Backend)
	}
	if _, err := (&Native{HasCompose: true}).Ps(context.Background(), p); !errors.Is(err, ErrNativeUnavailable) {
		t.Errorf("native Ps err = %v", err)
	}
}

func TestProjectsStore(t *testing.T) {
	projects := Projects{Store: &store.Store{Dir: t.TempDir()}}
	file, _ := filepath.Abs("testdata/load/compose.yaml")
	p, err := NewProject("my-app", file)
	if err != nil {
		t.Fatal(err)
	}
	p.Volumes = map[string]VolumeChoice{"data": {Mode: VolumeBind, Path: filepath.Dir(file)}}
	if err := projects.Save(p); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(projects.Store.Path("projects/" + p.ID + "/project.json"))
	if !strings.Contains(string(b), `"backend": "builtin"`) && !strings.Contains(string(b), `"backend":"builtin"`) {
		t.Errorf("project.json = %s", b)
	}
	list, err := projects.List()
	if err != nil || len(list) != 1 || list[0].Name != "my-app" || list[0].Volumes["data"].Mode != VolumeBind {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := projects.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := projects.List(); len(list) != 0 {
		t.Errorf("delete left %v", list)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("delete touched the Compose file: %v", err)
	}
	for _, bad := range []*Project{
		{ID: testID, Name: "Upper", Backend: BackendBuiltin, Files: []string{file}},
		{ID: testID, Name: "ok", Backend: BackendBuiltin, Files: []string{"relative.yaml"}},
		{ID: "../x", Name: "ok", Backend: BackendBuiltin, Files: []string{file}},
		{ID: testID, Name: "ok", Backend: "other", Files: []string{file}},
	} {
		if err := projects.Save(bad); err == nil {
			t.Errorf("saved invalid project %+v", bad)
		}
	}
}
