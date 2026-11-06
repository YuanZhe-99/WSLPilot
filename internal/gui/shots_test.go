package gui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/capability"
	"github.com/YuanZhe-99/WSLPilot/internal/wsl"
)

// TestScreenshots renders every page, the settings sheet and dark mode
// with fixture data into $WSLPILOT_SHOTS, for checking the layout by eye
// against design/.
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
	tt := ui.NewTester(a.View, 1280, 800)
	shot := func(name string) {
		for i := 0; i < 30; i++ {
			drain()
			tt.Frame()
		}
		f, _ := os.Create(filepath.Join(dir, name+".png"))
		png.Encode(f, tt.Image())
		f.Close()
	}
	pages := func(suffix string) {
		a.page = PageDashboard
		shot("dashboard" + suffix)
		a.page = PageWSL
		settle(t, tt, func() bool { return len(a.wsl.distros) > 0 })
		a.selectDistro("Debian")
		shot("wsl" + suffix)
		a.wsl.compactOpen = true
		shot("wsl-compact" + suffix)
		a.wsl.compactOpen = false
		a.wsl.settingsFor = "Debian"
		shot("distro-settings" + suffix)
		a.wsl.settingsFor = ""
		a.page = PageWSLc
		settle(t, tt, func() bool { return a.ctr.loaded })
		a.ctr.csel = 0
		shot("containers" + suffix)
		a.ctr.tab = 1
		shot("images" + suffix)
		a.ctr.tab = 0
		a.page = PageCompose
		shot("compose" + suffix)
		a.page = PageVolumes
		settle(t, tt, func() bool { return a.vol.loaded })
		a.vol.sel = 1
		shot("volumes" + suffix)
		a.page = PageBackups
		settle(t, tt, func() bool { return a.bk.loaded })
		shot("backups" + suffix)
		a.bk.view = 1
		shot("plans" + suffix)
		a.bk.view = 0
		a.page = PageTerminal
		shot("terminal" + suffix)
		a.page = PageTasks
		shot("tasks" + suffix)
		a.page = PageDashboard
		for _, s := range []settingsSection{setGeneral, setBackups, setContainers, setAbout} {
			a.openSettings(s)
			shot("settings-" + itoa(int(s)) + suffix)
		}
		a.settingsOpen = false
	}
	pages("")
	tt.SetDark(true)
	pages("-dark")
}
