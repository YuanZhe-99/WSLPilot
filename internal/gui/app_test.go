package gui

import (
	"context"
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/capability"
	"github.com/YuanZhe-99/WSLPilot/internal/wsl"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// syncApp returns an App whose probe result is applied by a channel the
// test drains, standing in for Window.Update.
func syncApp(r capability.Report) (*App, chan func()) {
	updates := make(chan func(), 1)
	a := New(func(context.Context) capability.Report { return r },
		func(fn func()) { updates <- fn })
	return a, updates
}

func TestDashboardShowsProbe(t *testing.T) {
	r := capability.Report{
		WSL:     wsl.Version{WSL: "3.0.3.0", Kernel: "6.18.54.1-1"},
		Distros: []string{"Debian"},
		WSLcErr: errors.New("not installed"),
	}
	a, updates := syncApp(r)
	tt := ui.NewTester(a.View, 1000, 700)
	if !tt.HasText(L.DashChecking()) {
		t.Fatalf("before a probe: texts %q", tt.Texts())
	}
	a.Refresh()
	(<-updates)()
	tt.Frame()
	for _, want := range []string{L.DashWSLVersion("3.0.3.0", "6.18.54.1-1"), L.DashDistros(1), L.DashNotFound(), L.DashComposeBuiltin()} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestRefreshRunsOneProbeAtATime(t *testing.T) {
	calls := 0
	updates := make(chan func(), 2)
	a := New(func(context.Context) capability.Report { calls++; return capability.Report{} },
		func(fn func()) { updates <- fn })
	a.Refresh()
	a.Refresh() // ignored: the first probe has not reported yet
	(<-updates)()
	if calls != 1 || a.probing {
		t.Errorf("calls %d, probing %v", calls, a.probing)
	}
}

func TestSidebarNavigates(t *testing.T) {
	a, _ := syncApp(capability.Report{})
	tt := ui.NewTester(a.View, 1000, 700)
	if err := tt.Click(L.NavBackups()); err != nil {
		t.Fatal(err)
	}
	if a.page != PageBackups || !tt.HasText(L.PageComingSoon()) {
		t.Errorf("page %q, texts %q", a.page, tt.Texts())
	}
}
