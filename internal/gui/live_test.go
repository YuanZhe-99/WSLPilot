package gui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/YuanZhe-99/WSLPilot/internal/core"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// TestLiveRecreate edits a wslpilot-test-* container: the old one is
// exported to Safety, replaced, and the new one has the new settings.
func TestLiveRecreate(t *testing.T) {
	if os.Getenv("WSLPILOT_LIVE") != "1" {
		t.Skip("set WSLPILOT_LIVE=1")
	}
	t.Setenv("WSLPILOT_DATA", t.TempDir())
	k, err := core.New()
	if err != nil {
		t.Fatal(err)
	}
	k.WireCompose(false)
	a := New(k, &fakeHost{}, func(fn func()) { fn() })
	a.settings.backupDir = t.TempDir()
	ctx := context.Background()
	const name = "wslpilot-test-edit"
	defer k.WSLc.Remove(context.Background(), true, false, name)
	id, err := k.WSLc.Create(ctx, wslc.ContainerSpec{Name: name, Image: "alpine:3.20", Command: []string{"sleep", "600"}, Env: []string{"A=1"}})
	if err != nil {
		t.Fatal(err)
	}
	k.WSLc.Start(ctx, id)
	ds, _ := k.WSLc.InspectContainers(ctx, id)
	in := editInput(ds[0])
	in.env += "\nB=2"
	a.recreate(ds[0].ID, runSpec(in))
	for _, j := range k.Jobs.List() {
		if s := k.Jobs.Get(j.ID).Wait(); s.Err != nil {
			t.Fatalf("job: %v\n%s", s.Err, s.Log)
		}
	}
	nd, err := k.WSLc.InspectContainers(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range nd[0].Config.Env {
		found = found || e == "B=2"
	}
	if !found || nd[0].ID == ds[0].ID || !nd[0].State.Running {
		t.Errorf("new container %+v", nd[0].Config.Env)
	}
	safety, _ := filepath.Glob(filepath.Join(a.settings.backupDir, "Safety", "container-"+name+"-*.tar"))
	if len(safety) != 1 {
		t.Errorf("safety export %v", safety)
	}
	if left, _ := k.WSLc.Containers(ctx, "name="+name+"-wslpilot-check"); len(left) != 0 {
		t.Errorf("probe container left: %+v", left)
	}
}
