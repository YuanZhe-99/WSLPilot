package compose

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

var update = flag.Bool("update", false, "rewrite golden files")

// translateFixture loads and translates testdata/translate/compose.yaml.
func translateFixture(t *testing.T) (*Plan, string) {
	t.Helper()
	meta := metaFor(t, "demo", "testdata/translate/compose.yaml")
	p, err := Load(context.Background(), meta)
	if err != nil {
		t.Fatal(err)
	}
	plan, r := Translate(p, meta)
	if !r.OK() {
		t.Fatalf("errors: %v", r.Errors)
	}
	return plan, filepath.Dir(meta.Files[0])
}

// TestTranslateGolden compares the wslc argument vectors of every
// container with testdata/translate/<service>.golden (run with -update to
// rewrite). The fixture directory is written as $DIR.
func TestTranslateGolden(t *testing.T) {
	plan, dir := translateFixture(t)
	for _, s := range plan.Services {
		var b strings.Builder
		for _, c := range s.Containers {
			for _, a := range wslc.CreateArgs(c.Spec) {
				b.WriteString(strings.ReplaceAll(a, dir, "$DIR") + "\n")
			}
			for _, x := range c.Extra {
				b.WriteString("# connect " + x.Network + " " + strings.Join(x.Aliases, ",") + "\n")
			}
		}
		if s.Build != nil {
			b.WriteString("# build " + strings.ReplaceAll(s.Build.Context, dir, "$DIR") + " tags=" + strings.Join(s.Build.Tags, ",") + " args=" + strings.Join(s.Build.Args, ",") + "\n")
		}
		golden := filepath.Join("testdata", "translate", s.Name+".golden")
		if *update {
			if err := os.WriteFile(golden, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if got := b.String(); got != strings.ReplaceAll(string(want), "\r\n", "\n") {
			t.Errorf("%s args differ from %s:\n%s", s.Name, golden, got)
		}
	}
}

func TestTranslateNamingAndLabels(t *testing.T) {
	plan, _ := translateFixture(t)
	if got := []string{plan.Services[0].Name, plan.Services[1].Name}; got[0] != "db" || got[1] != "web" {
		t.Fatalf("order = %v, want db before web", got)
	}
	web := plan.Service("web")
	if web.Image != "demo-web" {
		t.Errorf("built image name = %q", web.Image)
	}
	c := web.Containers[0]
	if c.Name != "demo-web-1" {
		t.Errorf("container name = %q", c.Name)
	}
	for k, v := range map[string]string{
		LabelProject: "demo", LabelService: "web", LabelNumber: "1",
		LabelBackend: "builtin", LabelRestart: "on-failure", LabelConfigHash: c.Hash,
	} {
		if c.Spec.Labels[k] != v {
			t.Errorf("label %s = %q, want %q", k, c.Spec.Labels[k], v)
		}
	}
	if !strings.HasSuffix(c.Spec.Labels[LabelConfigFiles], "compose.yaml") {
		t.Errorf("config_files label = %q", c.Spec.Labels[LabelConfigFiles])
	}
	names := map[string]bool{}
	for _, n := range plan.Networks {
		names[n.Name] = true
		if n.Options.Labels[LabelProject] != "demo" || n.Options.Labels[LabelNetwork] != n.Key {
			t.Errorf("network %s labels = %v", n.Name, n.Options.Labels)
		}
	}
	if !names["demo_back"] || !names["demo_front"] || names["demo_default"] {
		t.Errorf("networks = %v (default is unused)", names)
	}
	if len(plan.Volumes) != 1 || plan.Volumes[0].Name != "demo_data" || plan.Volumes[0].Labels[LabelVolume] != "data" {
		t.Errorf("volumes = %+v", plan.Volumes)
	}
}

func TestEntrypointSplit(t *testing.T) {
	plan, _ := translateFixture(t)
	s := plan.Service("web").Containers[0].Spec
	if s.Entrypoint != "/bin/sh" || strings.Join(s.Command, "|") != "-c|exec httpd -f" {
		t.Errorf("entrypoint %q command %q", s.Entrypoint, s.Command)
	}
}

func TestConfigHashDeterministicAndScoped(t *testing.T) {
	a, _ := translateFixture(t)
	b, _ := translateFixture(t)
	for i := range a.Services {
		if a.Services[i].Containers[0].Hash != b.Services[i].Containers[0].Hash {
			t.Errorf("%s: hash differs between identical loads", a.Services[i].Name)
		}
	}
	p, meta := loadInline(t, "services:\n  a:\n    image: alpine:3.20\n    environment: {X: \"1\"}\n  b:\n    image: alpine:3.20\n")
	before, _ := Translate(p, meta)
	s := p.Services["a"]
	v := "2"
	s.Environment["X"] = &v
	p.Services["a"] = s
	after, _ := Translate(p, meta)
	if before.Service("a").Containers[0].Hash == after.Service("a").Containers[0].Hash {
		t.Error("changing a's environment did not change its hash")
	}
	if before.Service("b").Containers[0].Hash != after.Service("b").Containers[0].Hash {
		t.Error("changing a changed b's hash")
	}
}

func TestScaleAndContainerName(t *testing.T) {
	p, meta := loadInline(t, "services:\n  w:\n    image: alpine:3.20\n    scale: 3\n  x:\n    image: alpine:3.20\n    container_name: custom\n")
	plan, r := Translate(p, meta)
	if !r.OK() {
		t.Fatal(r.Errors)
	}
	w := plan.Service("w")
	if len(w.Containers) != 3 || w.Containers[2].Name != "demo-w-3" || w.Containers[2].Spec.Labels[LabelNumber] != "3" {
		t.Errorf("replicas = %+v", w.Containers)
	}
	if plan.Service("x").Containers[0].Name != "custom" {
		t.Errorf("container_name ignored")
	}
}

func TestTopoOrder(t *testing.T) {
	svc := func(deps ...string) types.ServiceConfig {
		s := types.ServiceConfig{DependsOn: types.DependsOnConfig{}}
		for _, d := range deps {
			s.DependsOn[d] = types.ServiceDependency{Condition: types.ServiceConditionStarted, Required: true}
		}
		return s
	}
	order, err := topoOrder(types.Services{"web": svc("api"), "api": svc("db", "cache"), "db": svc(), "cache": svc(), "zed": svc()})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order, ","); got != "cache,db,api,web,zed" && got != "cache,db,zed,api,web" {
		t.Errorf("order = %s", got)
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	if pos["api"] < pos["db"] || pos["web"] < pos["api"] {
		t.Errorf("dependency after dependent: %v", order)
	}
	_, err = topoOrder(types.Services{"a": svc("b"), "b": svc("c"), "c": svc("a"), "d": svc()})
	if err == nil || !strings.Contains(err.Error(), "a, b, c") {
		t.Errorf("cycle error = %v", err)
	}
}

func TestShellJoinAndFormat(t *testing.T) {
	if got := shellJoin([]string{"sh", "-c", "test -f '/x'"}); got != `sh -c 'test -f '\''/x'\'''` {
		t.Errorf("shellJoin = %s", got)
	}
	for n, want := range map[int64]string{1 << 30: "1G", 64 << 20: "64M", 3072: "3K", 1000: "1000"} {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestVolumeBindMode(t *testing.T) {
	p, meta := loadInline(t, "services:\n  s:\n    image: alpine:3.20\n    volumes: [data:/data]\nvolumes:\n  data: {}\n")
	folder := filepath.Join(t.TempDir(), "data dir")
	meta.Volumes = map[string]VolumeChoice{"data": {Mode: VolumeBind, Path: folder}}
	plan, r := Translate(p, meta)
	if !r.OK() {
		t.Fatal(r.Errors)
	}
	m := plan.Service("s").Containers[0].Spec.Mounts[0]
	if m.Type != "bind" || m.Source != folder || m.Target != "/data" {
		t.Errorf("mount = %+v", m)
	}
}
