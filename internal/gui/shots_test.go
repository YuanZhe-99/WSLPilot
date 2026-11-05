package gui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/capability"
	"github.com/YuanZhe-99/WSLPilot/internal/wsl"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// TestScreenshots renders every page with fixture data into
// $WSLPILOT_SHOTS, for checking the layout by eye.
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("WSLPILOT_SHOTS")
	if dir == "" {
		t.Skip("set WSLPILOT_SHOTS to a folder")
	}
	os.MkdirAll(dir, 0o755)
	a, _, _ := testApp(t)
	r := capability.Report{WSL: wsl.Version{WSL: "3.0.3.0", Kernel: "6.18.54.1-1"}, Distros: []string{"Debian", "wslpilot-test-d1"}}
	r.WSLc.Client.Version = "3.0.3.0"
	a.report = &r
	tt := ui.NewTester(a.View, 1200, 780)
	shot := func(name string) {
		for i := 0; i < 30; i++ {
			tt.Frame()
		}
		f, _ := os.Create(filepath.Join(dir, name+".png"))
		png.Encode(f, tt.Image())
		f.Close()
	}
	shot("dashboard")
	tt.Click(L.NavWSL())
	settle(t, tt, func() bool { return len(a.wsl.distros) > 0 })
	a.wsl.sel = 0
	shot("wsl")
	a.wsl.tab = 1
	shot("wsl-conf")
	a.wsl.view = 1
	shot("wslconfig")
	tt.Click(L.NavWSLc())
	settle(t, tt, func() bool { return a.ctr.loaded })
	a.ctr.csel = 0
	shot("containers")
	tt.Click(L.NavVolumes())
	settle(t, tt, func() bool { return a.vol.loaded })
	a.vol.sel = 1
	shot("volumes")
	tt.Click(L.NavBackups())
	settle(t, tt, func() bool { return a.bk.loaded })
	shot("backups")
	tt.Click(L.NavSettings())
	shot("settings")
	tt.SetDark(true)
	tt.Click(L.NavDashboard())
	shot("dashboard-dark")
}
