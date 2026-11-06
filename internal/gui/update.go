package gui

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// The app update loop (doc/en-us/gui.md, "Updates"), after Easytier-MyGo.
const (
	updateCheckEvery = 24 * time.Hour
	updateLoopEvery  = 5 * time.Minute
	autoIdleAfter    = 15 * time.Minute
	autoRetryAfter   = 6 * time.Hour
	autoInstallIdle  = "idle"
	autoInstallDaily = "daily"
)

// updateState is the update shown in About & updates.
type updateState struct {
	found      *AppUpdate
	checking   bool
	installing bool
	progress   float64
	lastTry    time.Time

	// The bound controls of the Updates rows, filled from the prefs once.
	bound     bool
	daily     bool
	autoLabel string
	hourLabel string
}

// autoInstallDue reports whether an automatic install may start now.
// Inputs: mode and hour from the prefs; idle is the system's idle time (0
// when it does not tell); lastTry is the last automatic install attempt.
// Notes: a system that does not report idle time never counts as idle.
func autoInstallDue(mode string, hour int, now time.Time, idle time.Duration, lastTry time.Time) bool {
	if !lastTry.IsZero() && now.Sub(lastTry) < autoRetryAfter {
		return false
	}
	switch mode {
	case autoInstallIdle:
		return idle >= autoIdleAfter
	case autoInstallDaily:
		return now.Hour() == hour
	}
	return false
}

// UpdateLoop runs the daily update check while WSLPilot runs (the first
// shortly after launch when one is due) and installs a found version when
// the automatic install's time has come and no job runs.
// Side effects: network requests to the release feed; may relaunch the app.
func (a *App) UpdateLoop(ctx context.Context) {
	if a.host == nil || !a.host.UpdatesEnabled() {
		return // a portable copy or a development build
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
	}
	for {
		var due, found bool
		var mode string
		var hour int
		var lastTry time.Time
		done := make(chan struct{})
		a.update(func() {
			a.loadPrefs()
			p := a.settings.prefs
			due = !p.NoUpdateCheck && time.Since(p.LastUpdateCheck) >= updateCheckEvery
			found = a.upd.found != nil && !a.upd.installing && a.k.Jobs.Active() == 0
			mode, hour, lastTry = p.AutoInstall, p.AutoInstallHour, a.upd.lastTry
			close(done)
		})
		select {
		case <-done:
		case <-ctx.Done():
			return
		}
		if due {
			a.checkUpdate(ctx, false)
		}
		if found && mode != "" {
			idle := time.Duration(a.host.IdleSeconds() * float64(time.Second))
			if now := time.Now(); autoInstallDue(mode, hour, now, idle, lastTry) {
				a.update(func() {
					a.upd.lastTry = now
					a.installUpdate()
				})
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(updateLoopEvery):
		}
	}
}

// checkUpdate asks the release feed for a newer signed version.
// Inputs: manual is a check the user asked for: it reports "up to date"
// and failures.
// Side effects: records the check in the prefs; the automatic check
// notifies once per version.
func (a *App) checkUpdate(ctx context.Context, manual bool) {
	a.update(func() { a.upd.checking = true })
	cctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	up, err := a.host.CheckUpdate(cctx)
	a.update(func() {
		a.upd.checking = false
		if err != nil {
			if manual {
				a.notify(L.UpdCheckFailed(friendlyError(err)), true)
			}
			return
		}
		a.loadPrefs()
		p := &a.settings.prefs
		p.LastUpdateCheck = time.Now()
		a.upd.found = up
		switch {
		case up == nil && manual:
			a.notify(L.UpdUpToDate(), false)
		case up != nil && !manual && p.AnnouncedVersion != up.Version:
			p.AnnouncedVersion = up.Version
			a.host.Notify(L.AppName(), L.UpdAvailable(up.Version))
		}
		a.savePrefs()
	})
}

// installUpdate downloads the found version, checks its signature,
// replaces the app and relaunches into it.
// Side effects: a window closed into the tray stays closed after the
// relaunch (RelaunchHidden).
func (a *App) installUpdate() {
	up := a.upd.found
	if up == nil || a.upd.installing {
		return
	}
	a.upd.installing, a.upd.progress = true, 0
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		err := up.Install(ctx, func(done, total int64) {
			if total > 0 {
				a.update(func() { a.upd.progress = float64(done) / float64(total) })
			}
		})
		a.update(func() {
			if err != nil {
				a.upd.installing = false
				a.notify(L.UpdInstallFailed(friendlyError(err)), true)
				return
			}
			a.loadPrefs()
			a.settings.prefs.RelaunchHidden = a.hidden
			if serr := a.k.SavePrefs(a.settings.prefs); serr != nil {
				a.notify(friendlyError(serr), true)
			}
			a.host.Relaunch()
		})
	}()
}

// updateRows are About & updates' Updates rows: the state (installing,
// a version found with Install and restart, else the last check with
// Check for updates), the daily check, and the automatic install.
func (a *App) updateRows(c *ui.Context, r *rows) {
	if !a.host.UpdatesEnabled() {
		r.row(c, "upd", iconArrowUp, L.SetUpdates(), L.UpdUnavailable(), nil)
		return
	}
	s := &a.settings
	u := &a.upd
	switch {
	case u.installing:
		r.row(c, "upd", iconDownload, L.UpdInstalling(int(u.progress*100)), L.UpdInstallingAbout(), func() { ui.Spinner(c) })
	case u.found != nil:
		r.rowTone(c, "upd", iconDownload, L.UpdAvailable(u.found.Version), L.UpdInstalled(appVersion()), toneAccent, func() {
			if ui.PrimaryButton(c.Key("upd-install"), L.UpdInstallRestart()).Clicked() {
				a.installUpdate()
			}
		})
	default:
		about := L.UpdNeverChecked()
		if !s.prefs.LastUpdateCheck.IsZero() {
			about = L.UpdLastChecked(s.prefs.LastUpdateCheck.Local().Format("2006-01-02 15:04"))
		}
		r.row(c, "upd", iconCheck, L.UpdLatest(appVersion()), about, func() {
			if ui.Button(c.Key("upd-check"), L.UpdCheckNow()).Disabled(u.checking).Clicked() {
				go a.checkUpdate(context.Background(), true)
			}
		})
	}
	modes := []string{"", autoInstallIdle, autoInstallDaily}
	labels := []string{L.UpdAutoOff(), L.UpdAutoIdle(), L.UpdAutoDaily()}
	hours := make([]string, 24)
	for h := range hours {
		hours[h] = fmt.Sprintf("%02d:00", h)
	}
	if !u.bound {
		u.bound = true
		u.daily = !s.prefs.NoUpdateCheck
		u.autoLabel = labels[max(slices.Index(modes, s.prefs.AutoInstall), 0)]
		u.hourLabel = hours[min(max(s.prefs.AutoInstallHour, 0), 23)]
	}
	r.row(c, "upd-daily", iconRefresh, L.SetCheckDaily(), L.SetCheckDailyHelp(), func() {
		ui.Switch(c.Key("upd-daily-sw"), &u.daily).Label(L.SetCheckDaily()).OnChange(func() {
			s.prefs.NoUpdateCheck = !u.daily
			a.savePrefs()
		})
	})
	if !u.daily {
		return
	}
	r.row(c, "upd-auto", iconDownload, L.SetAutoInstall(), L.SetAutoInstallHelp(), func() {
		ui.Select(c.Key("upd-auto-sel"), &u.autoLabel, labels).Label(L.SetAutoInstall()).Width(170).OnChange(func() {
			s.prefs.AutoInstall = modes[max(slices.Index(labels, u.autoLabel), 0)]
			a.savePrefs()
		})
		if s.prefs.AutoInstall == autoInstallDaily {
			ui.Select(c.Key("upd-auto-hour"), &u.hourLabel, hours).Label(L.SetAutoInstall()).Width(110).OnChange(func() {
				s.prefs.AutoInstallHour = max(slices.Index(hours, u.hourLabel), 0)
				a.savePrefs()
			})
		}
	})
}
