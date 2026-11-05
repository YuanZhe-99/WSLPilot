package wslc

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/YuanZhe-99/WSLPilot/internal/procexec"
)

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/3.0.3.0/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return procexec.Decode(b)
}

func TestParseFixtures(t *testing.T) {
	cs, err := ParseLines[ContainerSummary](read(t, "containers.jsonl"))
	if err != nil || len(cs) != 1 || !cs[0].Running() || cs[0].Names != "wslpilot-test-c1" {
		t.Fatalf("containers: %+v %v", cs, err)
	}
	ins, err := ParseArray[Container](read(t, "c-inspect.json"))
	if err != nil || len(ins) != 1 {
		t.Fatal(err)
	}
	c := ins[0]
	if c.DisplayName() != "wslpilot-test-c1" || c.Labels["com.docker.compose.project"] != "demo" ||
		!c.State.Running || c.Mounts[0].Name != "wslpilot-test-vol" ||
		!slices.Contains(c.NetworkSettings.Networks["wslpilot-test-net"].Aliases, "web") ||
		c.Ports["80/tcp"][0].HostPort != "18080" {
		t.Errorf("inspect: %+v", c)
	}
	vs, err := ParseArray[Volume](read(t, "v-inspect.json"))
	if err != nil || len(vs) != 2 || vs[1].Driver != "vhd" || !strings.HasSuffix(vs[1].Status.HostPath, "wslpilot-test-vhd.vhdx") {
		t.Errorf("volumes: %+v %v", vs, err)
	}
	ns, err := ParseArray[Network](read(t, "n-inspect.json"))
	if err != nil || ns[0].IPAM.Config[0].Subnet != "172.18.0.0/16" || ns[0].Builtin() {
		t.Errorf("networks: %+v %v", ns, err)
	}
	nl, err := ParseLines[struct{ Name string }](read(t, "networks.jsonl"))
	if err != nil || len(nl) != 4 {
		t.Errorf("network list: %+v %v", nl, err)
	}
	is, err := ParseLines[Image](read(t, "images.jsonl"))
	if err != nil || is[0].Ref() != "alpine:3.20" {
		t.Errorf("images: %+v %v", is, err)
	}
	st, err := ParseLines[Stats](read(t, "stats.jsonl"))
	if err != nil || st[0].PIDs != 1 {
		t.Errorf("stats: %+v %v", st, err)
	}
	ev, err := ParseLines[Event](read(t, "events.jsonl"))
	if err != nil || len(ev) != 7 || ev[3].Action != "die" || ev[3].Actor.Attributes["exitCode"] != "137" || ev[0].At().Year() != 2026 {
		t.Errorf("events: %+v %v", ev, err)
	}
	img, err := ParseArray[ImageDetail](read(t, "i-inspect.json"))
	if err != nil || img[0].Architecture != "arm64" || img[0].Config.Cmd[0] != "/bin/sh" {
		t.Errorf("image inspect: %+v %v", img, err)
	}
}

func TestCreateArgs(t *testing.T) {
	to := 5
	got := CreateArgs(ContainerSpec{
		Name: "p-web-1", Image: "nginx:1", Command: []string{"nginx", "-g", "daemon off;"},
		Env:     []string{"A=1"},
		Labels:  map[string]string{"z": "1", "a": "2"},
		Ports:   []PortBinding{{HostPort: "8080", ContainerPort: "80"}, {HostIP: "0.0.0.0", HostPort: "53", ContainerPort: "53", Protocol: "udp"}},
		Mounts:  []MountSpec{{Type: "bind", Source: `C:\My Data`, Target: "/data", ReadOnly: true}, {Type: "volume", Source: "db", Target: "/var/lib/db"}, {Type: "tmpfs", Target: "/run"}},
		Network: "p_default", Aliases: []string{"web"}, StopTimeout: &to,
		Health: &Health{Cmd: "curl -f http://localhost", Retries: 3},
	})
	want := []string{"container", "create", "--name", "p-web-1", "--env", "A=1",
		"--label", "a=2", "--label", "z=1",
		"--publish", "8080:80", "--publish", "0.0.0.0:53:53/udp",
		"--volume", `C:\My Data:/data:ro`, "--volume", "db:/var/lib/db", "--tmpfs", "/run",
		"--network", "p_default", "--network-alias", "web", "--stop-timeout", "5",
		"--health-cmd", "curl -f http://localhost", "--health-retries", "3",
		"nginx:1", "nginx", "-g", "daemon off;"}
	if !slices.Equal(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestInspectMissingIsNotFound(t *testing.T) {
	c := &Client{Run: func(ctx context.Context, name string, args ...string) (string, error) {
		return "[]", &procexec.ExitError{Name: name, Args: args, Code: 1}
	}}
	if _, err := c.InspectContainers(context.Background(), "nope"); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionOption(t *testing.T) {
	var got []string
	c := &Client{Session: "s1", Run: func(ctx context.Context, name string, args ...string) (string, error) {
		got = args
		return "", nil
	}}
	c.Images(context.Background())
	if got[0] != "--session" || got[1] != "s1" || got[2] != "image" {
		t.Errorf("args = %q", got)
	}
}
