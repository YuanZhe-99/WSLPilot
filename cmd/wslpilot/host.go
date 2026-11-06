package main

import (
	"context"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/gui"
)

// host implements gui.Host with MyGo.
type host struct{ win *mygo.Window }

// PickFiles shows the system open dialog.
func (h *host) PickFiles(title string, folders bool, exts ...string) []string {
	opts := mygo.OpenDialogOptions{Parent: h.win, Title: title, Directory: folders, CreateDirectories: true}
	if len(exts) > 0 && !folders {
		opts.Filters = []mygo.FileFilter{{Name: title, Extensions: exts}, {Name: "*", Extensions: []string{"*"}}}
	}
	ps, err := mygo.Dialog.Open(opts)
	if err != nil {
		return nil
	}
	return ps
}

// PickSave shows the system save dialog.
func (h *host) PickSave(title, name string) string {
	p, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: h.win, Title: title, DefaultPath: name, CreateDirectories: true})
	if err != nil {
		return ""
	}
	return p
}

// OpenPath opens a file or folder in Explorer.
func (h *host) OpenPath(path string) { mygo.Shell.OpenPath(path) }

// OpenURL opens a link.
func (h *host) OpenURL(url string) { mygo.Shell.OpenExternal(url) }

// Notify shows a toast notification of Windows.
func (h *host) Notify(title, body string) {
	mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body}).Show()
}

// SetOpenAtLogin turns starting at sign-in on or off.
func (h *host) SetOpenAtLogin(on bool) error { return mygo.App.SetOpenAtLogin(on) }

// OpenAtLogin reports the sign-in setting.
func (h *host) OpenAtLogin() bool { return mygo.App.OpenAtLogin() }

// SetTheme applies "system", "light" or "dark".
func (h *host) SetTheme(mode string) {
	switch mode {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// Terminal starts argv (nil: PowerShell) in Ghostty's terminal emulator.
func (h *host) Terminal(title string, argv []string) (gui.Term, error) {
	t, err := terminal.New(terminal.Options{Command: argv})
	if err != nil {
		return nil, err
	}
	return &term{t: t}, nil
}

// term adapts terminal.Terminal to gui.Term.
type term struct{ t *terminal.Terminal }

// View draws the terminal.
func (t *term) View(c *ui.Context) ui.Element { return terminal.View(c, t.t) }

// Done reports whether the program exited.
func (t *term) Done() bool {
	select {
	case <-t.t.Done():
		return true
	default:
		return false
	}
}

// Close ends the program.
func (t *term) Close() { t.t.Close() }

// Window minimizes, toggles maximized or closes the main window.
func (h *host) Window(op gui.WindowOp) {
	if h.win == nil {
		return
	}
	switch op {
	case gui.WindowMinimize:
		h.win.Minimize()
	case gui.WindowToggleMaximize:
		if h.win.IsMaximized() {
			h.win.Unmaximize()
		} else {
			h.win.Maximize()
		}
	case gui.WindowClose:
		h.win.Close()
	}
}

// Maximized reports whether the main window is maximized.
func (h *host) Maximized() bool { return h.win != nil && h.win.IsMaximized() }

// UpdatesEnabled reports whether this copy can update itself.
func (h *host) UpdatesEnabled() bool { return mygo.Updater.Enabled() }

// CheckUpdate asks the release feed (mygo.json "updates") for a newer
// signed version.
func (h *host) CheckUpdate(ctx context.Context) (*gui.AppUpdate, error) {
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return nil, err
	}
	return &gui.AppUpdate{Version: up.Version, Notes: up.Notes, Install: up.Install}, nil
}

// Relaunch restarts WSLPilot into the installed update.
func (h *host) Relaunch() { mygo.App.Relaunch() }

// IdleSeconds is how long the computer has not been used.
func (h *host) IdleSeconds() float64 { return mygo.Power.IdleTime().Seconds() }
