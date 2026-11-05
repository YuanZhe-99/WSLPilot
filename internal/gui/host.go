package gui

import (
	"strconv"

	"github.com/egoist/mygo/ui"
)

// Host is what the GUI needs from the window and the system. The app
// passes the MyGo implementation (cmd/wslpilot); tests pass a fake.
type Host interface {
	// PickFiles shows an open dialog and returns the chosen paths (nil when
	// cancelled). It blocks: call it from a goroutine.
	PickFiles(title string, folders bool, exts ...string) []string
	// PickSave shows a save dialog with a suggested name ("" when cancelled).
	PickSave(title, name string) string
	// OpenPath opens a file or folder in Explorer.
	OpenPath(path string)
	// OpenURL opens a link in the browser.
	OpenURL(url string)
	// Terminal starts argv in a pseudo-terminal.
	Terminal(title string, argv []string) (Term, error)
	// Notify shows a system notification.
	Notify(title, body string)
	// SetOpenAtLogin turns starting WSLPilot at sign-in on or off.
	SetOpenAtLogin(on bool) error
	// OpenAtLogin reports whether WSLPilot starts at sign-in.
	OpenAtLogin() bool
	// SetTheme chooses "system", "light" or "dark".
	SetTheme(mode string)
	// CheckForUpdates checks now and shows the update window.
	CheckForUpdates()
	// AutoUpdate reports whether updates are checked in the background.
	AutoUpdate() bool
	// SetAutoUpdate turns background update checks on or off.
	SetAutoUpdate(on bool)
}

// Term is a running terminal session.
type Term interface {
	// View draws the terminal.
	View(c *ui.Context) ui.Element
	// Done reports whether the program exited.
	Done() bool
	// Close ends the program.
	Close()
}

func itoa(n int) string { return strconv.Itoa(n) }
