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
	"github.com/YuanZhe-99/WSLPilot/internal/core"
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
	smallGap     = 6
	titleSize    = 24
	sectionSize  = 16
	dialogWidth  = 520
	detailWidth  = 360
	jobsWidth    = 360
	editorHeight = 360
)

// probeTimeout bounds one capability probe; wslc may need to start its
// session manager on the first call.
const probeTimeout = 30 * time.Second

// commandTimeout bounds one short list or inspect command.
const commandTimeout = 60 * time.Second

// App is the GUI state. Its fields are read and written on the UI thread
// only; background work hands results back through update.
type App struct {
	page string

	probing bool
	report  *capability.Report

	k      *core.Core
	host   Host
	probe  func(context.Context) capability.Report
	update func(func())

	toasts   []toast
	confirm  confirmState
	jobsOpen bool

	wsl      wslPage
	ctr      containersPage
	vol      volumesPage
	bk       backupsPage
	cmp      composePage
	term     terminalPage
	settings settingsPage
}

// toast is a message queued for the next frame.
type toast struct {
	text  string
	error bool
}

// New returns the GUI state.
//
// Inputs: k holds the services (fakes in tests); host gives the window's
// system services (file dialogs, terminals); update runs a function on
// the UI thread (mygo.Window.Update in the app).
func New(k *core.Core, host Host, update func(func())) *App {
	a := &App{page: PageDashboard, k: k, host: host, update: update}
	a.probe = k.Prober.Probe
	k.Jobs.OnChange = func() { a.update(func() {}) }
	k.Jobs.OnFinish = a.jobFinished
	return a
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

// async runs fn on a goroutine with a command timeout; the function fn
// returns is applied on the UI thread.
func (a *App) async(fn func(ctx context.Context) func()) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		apply := fn(ctx)
		if apply != nil {
			a.update(apply)
		}
	}()
}

// notify queues a toast for the next frame.
func (a *App) notify(text string, isError bool) {
	a.toasts = append(a.toasts, toast{text, isError})
}

// View builds the window: the sidebar, the chosen page, the job panel and
// any dialog.
func (a *App) View(c *ui.Context) {
	for _, t := range a.toasts {
		if t.error {
			c.AddToast(ui.Toast{Title: t.text, Type: "error"})
		} else {
			c.Toast(t.text)
		}
	}
	a.toasts = a.toasts[:0]
	ui.Row(c).Fill().Children(func() {
		ui.Sidebar(c, &a.page, func() {
			ui.SidebarSection(c, L.AppName(), nil, func() {
				ui.SidebarItem(c, PageDashboard, nil, L.NavDashboard())
				ui.SidebarItem(c, PageWSL, nil, L.NavWSL())
				ui.SidebarItem(c, PageWSLc, nil, L.NavWSLc())
				ui.SidebarItem(c, PageCompose, nil, L.NavCompose())
				ui.SidebarItem(c, PageVolumes, nil, L.NavVolumes())
				ui.SidebarItem(c, PageBackups, nil, L.NavBackups())
				ui.SidebarItem(c, PageTerminal, nil, L.NavTerminal()).Children(func() {
					if n := len(a.term.sessions); n > 0 {
						ui.Badge(c, itoa(n))
					}
				})
				ui.SidebarItem(c, PageSettings, nil, L.NavSettings())
			})
		}).Width(sidebarWidth).FillHeight()
		ui.Column(c).Grow(1).FillHeight().Padding(pagePadding).Gap(gap).Children(func() {
			switch a.page {
			case PageDashboard:
				a.dashboard(c)
			case PageWSL:
				a.wslView(c)
			case PageWSLc:
				a.containersView(c)
			case PageCompose:
				a.composeView(c)
			case PageVolumes:
				a.volumesView(c)
			case PageBackups:
				a.backupsView(c)
			case PageTerminal:
				a.terminalView(c)
			case PageSettings:
				a.settingsView(c)
			}
		})
		if a.jobsOpen {
			a.jobsPanel(c)
		}
	})
	a.confirmDialog(c)
}

// header draws a page title with actions on the right.
func (a *App) header(c *ui.Context, title string, actions func()) {
	ui.Row(c).FillWidth().Gap(smallGap).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(titleSize).Bold().Grow(1)
		if actions != nil {
			actions()
		}
		if n := a.k.Jobs.Active(); n > 0 || len(a.k.Jobs.List()) > 0 {
			label := L.JobsButton(n)
			ui.Button(c, label).OnClick(func() { a.jobsOpen = !a.jobsOpen })
		}
	})
}

// card draws a titled panel around the content body builds.
func card(c *ui.Context, title string, body func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(smallGap).AlignItems(ui.Start).Padding(cardPadding).Radius(cardRadius).
		Background(t.Surface).Border(1, t.Border).Children(func() {
		if title != "" {
			ui.Text(c, title).Bold()
		}
		body()
	})
}

// muted draws secondary text.
func muted(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(c.Theme().TextMuted)
}

// errorText draws an error line.
func errorText(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(c.Theme().Danger)
}

// dangerButton is a button in the danger color.
func dangerButton(c *ui.Context, label string) ui.Element {
	t := c.Theme()
	return ui.Button(c, label).TextColor(t.Danger)
}

// actions lays buttons out in a row that wraps.
func actions(c *ui.Context, body func()) {
	ui.Row(c).FillWidth().Wrap().Gap(smallGap).Children(body)
}
