package compose

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// liveProject is the only project name the live test uses; the behavior
// contract allows live tests to touch wslpilot-test-* resources only.
const liveProject = "wslpilot-test-compose"

const liveCompose = `
services:
  db:
    image: alpine:3.20
    command: ["sleep", "3600"]
    healthcheck:
      test: ["CMD-SHELL", "test -d /data"]
      interval: 1s
      timeout: 2s
      retries: 5
    volumes: [data:/data]
    networks:
      back:
        aliases: [database]
    stop_grace_period: 1s
  app:
    image: alpine:3.20
    command: ["sleep", "3600"]
    environment:
      MODE: "%s"
    mem_limit: 64m
    restart: unless-stopped
    depends_on:
      db: {condition: service_healthy}
    networks: [back]
    stop_grace_period: 1s
volumes:
  data: {}
networks:
  back: {}
`

// TestLiveBuiltin runs the builtin backend against the real wslc.exe.
// Run with WSLPILOT_LIVE=1; needs alpine:3.20 (pulled when missing).
func TestLiveBuiltin(t *testing.T) {
	if os.Getenv("WSLPILOT_LIVE") != "1" {
		t.Skip("set WSLPILOT_LIVE=1 to run against wslc.exe")
	}
	if !strings.HasPrefix(liveProject, "wslpilot-test-") {
		t.Fatal("live tests may only use wslpilot-test-* names")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client := wslc.New()
	b := &Builtin{Engine: client}
	file := filepath.Join(t.TempDir(), "compose.yaml")
	write := func(mode string) {
		if err := os.WriteFile(file, []byte(fmt.Sprintf(liveCompose, mode)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("one")
	p := metaFor(t, liveProject, file)
	if !strings.HasPrefix(p.Name, "wslpilot-test-") {
		t.Fatal("project name must start with wslpilot-test-")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := b.Down(ctx, p, DownOptions{RemoveVolumes: true}, nil); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	logf := func(e Event) { t.Logf("[%s] %s: %s", e.Step, e.Service, e.Message) }

	if err := b.Up(ctx, p, UpOptions{}, logf); err != nil {
		t.Fatalf("up: %v", err)
	}
	ps, err := b.Ps(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("ps = %+v", ps)
	}
	ids := map[string]string{}
	for _, c := range ps {
		ids[c.Service] = c.ID
		if c.State != "running" || c.Backend != "builtin" || c.Name != ContainerName(liveProject, c.Service, 1) {
			t.Errorf("container %+v", c)
		}
	}
	cs, err := client.InspectContainers(ctx, ids["app"], ids["db"])
	if err != nil {
		t.Fatal(err)
	}
	if l := labelsOf(cs[0]); l[LabelProject] != liveProject || l[LabelService] != "app" || l[LabelConfigHash] == "" || l[LabelRestart] != "unless-stopped" {
		t.Errorf("app labels = %v", l)
	}
	if cs[0].HostConfig.Memory != 64<<20 {
		t.Errorf("app memory = %d, want 64 MiB", cs[0].HostConfig.Memory)
	}
	if h := cs[1].State.Health; h == nil || h.Status != "healthy" {
		t.Errorf("db health = %+v", h)
	}

	var steps []string
	record := func(e Event) {
		logf(e)
		steps = append(steps, string(e.Step)+":"+e.Service)
	}
	if err := b.Up(ctx, p, UpOptions{}, record); err != nil {
		t.Fatalf("second up: %v", err)
	}
	for _, s := range steps {
		if strings.HasPrefix(s, "create") || strings.HasPrefix(s, "recreate") || strings.HasPrefix(s, "remove") {
			t.Errorf("second up changed something: %v", steps)
		}
	}

	write("two")
	steps = nil
	if err := b.Up(ctx, p, UpOptions{}, record); err != nil {
		t.Fatalf("third up: %v", err)
	}
	recreated := []string{}
	for _, s := range steps {
		if strings.HasPrefix(s, "recreate:") || strings.HasPrefix(s, "create:") {
			recreated = append(recreated, s)
		}
	}
	if len(recreated) != 1 || recreated[0] != "recreate:app" {
		t.Errorf("env change recreated %v, want only app", recreated)
	}
	ps, _ = b.Ps(ctx, p)
	for _, c := range ps {
		if c.Service == "db" && c.ID != ids["db"] {
			t.Error("db was recreated")
		}
		if c.Service == "app" && c.ID == ids["app"] {
			t.Error("app was not recreated")
		}
	}

	if err := b.Down(ctx, p, DownOptions{}, logf); err != nil {
		t.Fatalf("down: %v", err)
	}
	left, err := client.ContainersDetailed(ctx, projectFilter(liveProject))
	if err != nil || len(left) != 0 {
		t.Errorf("containers left: %v %v", left, err)
	}
	nets, _ := client.Networks(ctx)
	for _, n := range nets {
		if n.Labels[LabelProject] == liveProject {
			t.Errorf("network left: %s", n.Name)
		}
	}
	vols, _ := client.Volumes(ctx, projectFilter(liveProject))
	if len(vols) != 1 || vols[0].Name != liveProject+"_data" {
		t.Errorf("down without RemoveVolumes: volumes = %v", vols)
	}
	if err := b.Down(ctx, p, DownOptions{RemoveVolumes: true}, logf); err != nil {
		t.Fatal(err)
	}
	if vols, _ := client.Volumes(ctx, projectFilter(liveProject)); len(vols) != 0 {
		t.Errorf("volumes left: %v", vols)
	}
}
