package gui

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/compose"
	"github.com/YuanZhe-99/WSLPilot/internal/volume"
	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

func ctrWith(running bool, labels map[string]string, vols ...string) wslc.Container {
	var c wslc.Container
	c.State.Running = running
	c.Labels = labels
	for _, v := range vols {
		c.Mounts = append(c.Mounts, wslc.Mount{Type: "volume", Name: v})
	}
	return c
}

// A cleanup keeps volumes a running container uses and those of added
// Compose projects (even down), and takes those only stopped containers
// use when it removes the stopped containers too.
func TestCleanupVolumes(t *testing.T) {
	vols := []wslc.Volume{
		{Name: "in-use"},
		{Name: "stopped-only"},
		{Name: "loose"},
		{Name: "proj_db", Labels: map[string]string{compose.LabelProject: "proj"}},
		{Name: "gone_db", Labels: map[string]string{compose.LabelProject: "gone"}},
		{Name: "helped"},
	}
	cs := []wslc.Container{
		ctrWith(true, nil, "in-use"),
		ctrWith(false, nil, "stopped-only"),
		ctrWith(true, map[string]string{volume.HelperLabel: "1"}, "helped"),
	}
	owned := func(p string) bool { return p == "proj" }
	if got := cleanupVolumes(vols, cs, true, owned); !slices.Equal(got, []string{"stopped-only", "loose", "gone_db", "helped"}) {
		t.Errorf("with stopped removed: %v", got)
	}
	if got := cleanupVolumes(vols, cs, false, owned); !slices.Equal(got, []string{"loose", "gone_db", "helped"}) {
		t.Errorf("stopped kept: %v", got)
	}
}

// The dialog runs the chosen prunes in order and leaves volumes alone
// unless asked.
func TestCleanupRunsChosenPrunes(t *testing.T) {
	a, cmd, _ := testApp(t)
	for _, k := range []string{"container prune --force", "network prune --force", "image prune --force --all"} {
		cmd.out[k] = "Deleted:\n"
	}
	tt := ui.NewTester(a.View, 1300, 820)
	tt.Click(L.NavWSLc())
	settle(t, tt, func() bool { return a.ctr.loaded })
	a.openCleanup()
	settle(t, tt, func() bool { return tt.HasText(L.CtrCleanupTitle()) })
	if err := tt.Click(L.CtrCleanupImagesAll()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Clean up"); err != nil {
		t.Fatal(err)
	}
	settle(t, tt, func() bool { return cmd.ran("image prune --force --all") })
	for _, k := range []string{"container prune --force", "network prune --force"} {
		if !cmd.ran(k) {
			t.Errorf("did not run %s", k)
		}
	}
	if cmd.ran("volume prune") || cmd.ran("volume remove") {
		t.Error("touched volumes without being asked")
	}
}
