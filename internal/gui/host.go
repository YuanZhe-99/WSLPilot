package gui

import (
	"context"
	_ "embed"
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/version"
)

//go:embed appicon.png
var appIconPNG []byte

// appIcon is the app's icon for the sidebar (nil if undecodable).
var appIcon, _ = ui.DecodeBitmap(appIconPNG)

// appVersion is the running version.
func appVersion() string { return version.Version }

// WindowOp is an operation on the main window.
type WindowOp int

// Window operations.
const (
	WindowMinimize WindowOp = iota
	WindowToggleMaximize
	WindowClose
)

// AppUpdate is a newer signed version found on the release feed.
type AppUpdate struct {
	Version string
	Notes   string
	// Install downloads it, checks its signature and replaces the app; the
	// new version runs after Relaunch.
	Install func(ctx context.Context, progress func(done, total int64)) error
}

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
	// Window minimizes, maximizes or restores, or closes the main window.
	Window(op WindowOp)
	// Maximized reports whether the main window is maximized.
	Maximized() bool
	// UpdatesEnabled reports whether this copy can update itself (an
	// installed release build).
	UpdatesEnabled() bool
	// CheckUpdate asks the release feed for a newer version (nil: none).
	CheckUpdate(ctx context.Context) (*AppUpdate, error)
	// Relaunch restarts the app (into an installed update).
	Relaunch()
	// IdleSeconds is how long the user has not used the computer (0 when
	// the system does not tell).
	IdleSeconds() float64
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
