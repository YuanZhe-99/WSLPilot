// Package gui is WSLPilot's native interface: a sidebar of pages and the
// views of each. Views only read App state and call domain packages; every
// probe or command runs on a goroutine and publishes its result through the
// window's Update (see doc/en-us/architecture.md).
package gui

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/capability"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// Page IDs, also the sidebar item IDs.
const (
	PageDashboard = "dashboard"
	PageWSL       = "wsl"
	PageWSLc      = "wslc"
	PageCompose   = "compose"
	PageVolumes   = "volumes"
	PageBackups   = "backups"
	PageTerminal  = "terminal"
	PageSettings  = "settings"
)

// Layout tokens.
const (
	sidebarWidth = 220
	pagePadding  = 24
	cardPadding  = 16
	cardRadius   = 8
	gap          = 12
	titleSize    = 24
)

// probeTimeout bounds one capability probe; wslc may need to start its
// session manager on the first call.
const probeTimeout = 30 * time.Second

// App is the GUI state. Its fields are read and written on the UI thread
// only; background work hands results back through update.
type App struct {
	page string

	probing bool
	report  *capability.Report

	probe  func(context.Context) capability.Report
	update func(func())
}

// New returns the GUI state.
//
// Inputs: probe runs the capability probe (capability.Prober.Probe in the
// app, a fake in tests); update runs a function on the UI thread
// (mygo.Window.Update in the app).
func New(probe func(context.Context) capability.Report, update func(func())) *App {
	return &App{page: PageDashboard, probe: probe, update: update}
}

// SetUpdate replaces the UI-thread hand-off, once the window exists.
func (a *App) SetUpdate(update func(func())) { a.update = update }

// Refresh starts a capability probe unless one is running.
//
// Side effects: runs wsl.exe and wslc.exe on a goroutine; the report reaches
// the view through update.
func (a *App) Refresh() {
	if a.probing {
		return
	}
	a.probing = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		r := a.probe(ctx)
		a.update(func() {
			a.report = &r
			a.probing = false
		})
	}()
}

// View builds the window: the sidebar and the chosen page.
func (a *App) View(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Fill().Children(func() {
		ui.Sidebar(c, &a.page, func() {
			ui.SidebarSection(c, L.AppName(), nil, func() {
				ui.SidebarItem(c, PageDashboard, nil, L.NavDashboard())
				ui.SidebarItem(c, PageWSL, nil, L.NavWSL())
				ui.SidebarItem(c, PageWSLc, nil, L.NavWSLc())
				ui.SidebarItem(c, PageCompose, nil, L.NavCompose())
				ui.SidebarItem(c, PageVolumes, nil, L.NavVolumes())
				ui.SidebarItem(c, PageBackups, nil, L.NavBackups())
				ui.SidebarItem(c, PageTerminal, nil, L.NavTerminal())
				ui.SidebarItem(c, PageSettings, nil, L.NavSettings())
			})
		}).Width(sidebarWidth)
		ui.Column(c).Grow(1).Padding(pagePadding).Gap(gap).Children(func() {
			switch a.page {
			case PageDashboard:
				a.dashboard(c)
			default:
				ui.Text(c, pageTitle(a.page)).FontSize(titleSize).Bold()
				ui.Text(c, L.PageComingSoon()).TextColor(t.TextMuted)
			}
		})
	})
}

// pageTitle returns the localized title of a page ID.
func pageTitle(id string) string {
	switch id {
	case PageWSL:
		return L.NavWSL()
	case PageWSLc:
		return L.NavWSLc()
	case PageCompose:
		return L.NavCompose()
	case PageVolumes:
		return L.NavVolumes()
	case PageBackups:
		return L.NavBackups()
	case PageTerminal:
		return L.NavTerminal()
	case PageSettings:
		return L.NavSettings()
	}
	return L.NavDashboard()
}

// dashboard shows what the last probe found.
func (a *App) dashboard(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(gap).Children(func() {
		ui.Text(c, L.NavDashboard()).FontSize(titleSize).Bold().Grow(1)
		if ui.Button(c, L.DashRefresh()).Disabled(a.probing).Clicked() {
			a.Refresh()
		}
	})
	r := a.report
	if r == nil {
		ui.Text(c, L.DashChecking()).TextColor(t.TextMuted)
		return
	}
	card(c, L.DashWSL(), func() {
		if !r.HasWSL() {
			ui.Text(c, L.DashNotFound()).TextColor(t.TextMuted)
			return
		}
		ui.Text(c, L.DashWSLVersion(r.WSL.WSL, r.WSL.Kernel))
		ui.Text(c, L.DashDistros(len(r.Distros))).TextColor(t.TextMuted)
	})
	card(c, L.DashWSLc(), func() {
		if !r.HasWSLc() {
			ui.Text(c, L.DashNotFound()).TextColor(t.TextMuted)
			return
		}
		ui.Text(c, L.DashWSLcVersion(r.WSLc.Client.Version))
	})
	card(c, L.DashCompose(), func() {
		if r.NativeCompose {
			ui.Text(c, L.DashComposeNative())
		} else {
			ui.Text(c, L.DashComposeBuiltin())
		}
	})
}

// card draws a titled panel around the content body builds.
func card(c *ui.Context, title string, body func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(6).Padding(cardPadding).Radius(cardRadius).
		Background(t.Surface).Border(1, t.Border).Children(func() {
		ui.Text(c, title).Bold()
		body()
	})
}
