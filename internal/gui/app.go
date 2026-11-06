// Package gui is WSLPilot's native interface: a sidebar of pages and the
// views of each. Views only read App state and call domain packages; every
// probe or command runs on a goroutine and publishes its result through the
// window's Update (see doc/en-us/architecture.md).
package gui

import (
	"context"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/capability"
	"github.com/YuanZhe-99/WSLPilot/internal/core"
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
	PageTasks     = "tasks"
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

	toasts  []toast
	confirm confirmState

	// settingsOpen shows the Settings sheet at settingsAt.
	settingsOpen bool
	settingsAt   settingsSection
	settingsQ    string
	focus        settingsFocus

	upd updateState

	// hidden is true while the window is closed into the tray.
	hidden bool

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
	// Lists start with nothing selected, whichever page loads them first.
	a.wsl.list.Selected = &a.wsl.sel
	a.ctr.cl.Selected, a.ctr.il.Selected, a.ctr.nl.Selected = &a.ctr.csel, &a.ctr.isel, &a.ctr.nsel
	a.ctr.csel, a.ctr.isel, a.ctr.nsel = -1, -1, -1
	a.cmp.pl.Selected, a.cmp.sel = &a.cmp.sel, -1
	a.vol.ls.Selected, a.vol.sel = &a.vol.sel, -1
	a.vol.imp = volInput{newVolume: true, driver: "guest", check: true}
	a.bk.gl.Selected, a.bk.vl.Selected, a.bk.pl.Selected = &a.bk.gsel, &a.bk.vsel, &a.bk.psel
	a.bk.gsel, a.bk.vsel, a.bk.psel = -1, -1, -1
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

// SetHidden records whether the window is closed into the tray.
func (a *App) SetHidden(h bool) { a.hidden = h }

// Prefs returns the stored preferences (theme, close behavior, updates).
func (a *App) Prefs() Prefs {
	a.loadPrefs()
	return a.settings.prefs
}

// TakeRelaunchHidden reports whether the window should stay hidden after
// an update relaunch, and clears the flag.
func (a *App) TakeRelaunchHidden() bool {
	a.loadPrefs()
	h := a.settings.prefs.RelaunchHidden
	if h {
		a.settings.prefs.RelaunchHidden = false
		a.k.SavePrefs(a.settings.prefs)
	}
	return h
}

// CloseHint reports whether to tell the user once that closing the window
// keeps WSLPilot in the tray, and records that it was told.
func (a *App) CloseHint() bool {
	a.loadPrefs()
	p := &a.settings.prefs
	if p.CloseHinted || p.CloseQuits {
		return false
	}
	p.CloseHinted = true
	a.k.SavePrefs(*p)
	return true
}

// OpenUpdates opens Settings at About & updates and checks now (the tray's
// "Check for updates").
func (a *App) OpenUpdates() {
	a.openSettings(setAbout)
	if a.host != nil && a.host.UpdatesEnabled() {
		go a.checkUpdate(context.Background(), true)
	}
}
