package gui

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// TitleBarOptions hides the system title bar: the pane's header takes its
// place (doc/en-us/gui.md, "Window"). On Windows the view draws minimize,
// maximize and close at the end of the header; the window keeps its
// resizing edges, shadow and corners, and Mica shows around the pane.
// Inputs: o, the window's other options.
// Returns: o with the title-bar fields set.
func TitleBarOptions(o mygo.WindowOptions) mygo.WindowOptions {
	o.TitleBarStyle = mygo.TitleBarHidden
	o.TitleBarHeight = int(2*paneMargin + paneHeaderH)
	o.HideWindowButtons = ownWindowButtons
	o.Vibrancy = mygo.VibrancyMica
	return o
}

// sidebarItem is one page in the sidebar.
type sidebarItem struct {
	page  string
	icon  *ui.SVG
	label func() string
}

// mainPages are the sidebar's pages, in order.
var mainPages = []sidebarItem{
	{PageDashboard, iconDashboard, L.NavDashboard},
	{PageWSL, iconWSL, L.NavWSL},
	{PageWSLc, iconBox, L.NavWSLc},
	{PageCompose, iconLayers, L.NavCompose},
	{PageVolumes, iconDatabase, L.NavVolumes},
	{PageBackups, iconHistory, L.NavBackups},
	{PageTerminal, iconTerminal, L.NavTerminal},
}

// View builds the window: the sidebar on the window's material and beside
// it one pane whose header is the title bar; the Settings sheet and any
// dialog go over everything.
func (a *App) View(c *ui.Context) {
	t := c.Theme()
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	} else {
		c.Root().Background(windowBase(t))
	}
	for _, m := range a.toasts {
		if m.error {
			c.AddToast(ui.Toast{Title: m.text, Type: "error"})
		} else {
			c.Toast(m.text)
		}
	}
	a.toasts = a.toasts[:0]
	ui.Row(c.Key("shell")).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c)
		ui.Column(c.Key("main")).Grow(1).MinWidth(0).MinHeight(0).Padding(paneMargin, paneMargin, paneMargin, 0).Children(func() {
			ui.Column(c.Key("pane")).Grow(1).FillWidth().MinHeight(0).Radius(paneRadius).Clip().
				Background(t.Background).Border(1, t.Border).Shadow(0, 6, 18, 0, paneShadow).Children(func() {
				switch a.page {
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
				case PageTasks:
					a.tasksView(c)
				default:
					a.dashboard(c)
				}
			})
		})
	})
	a.settingsSheet(c)
	a.confirmDialog(c)
}

// sidebar is the app's name, its pages, and at the foot Tasks, the
// appearance and Settings.
func (a *App) sidebar(c *ui.Context) {
	t := c.Theme()
	ui.Column(c.Key("sidebar")).Width(sidebarWidth).Shrink(0).FillHeight().Padding(10, 10, 12, 12).Gap(2).Children(func() {
		ui.Row(c.Key("sb-top")).Height(paneHeaderH).Shrink(0).Gap(10).Padding(0, 8).AlignItems(ui.Center).DragWindow().Children(func() {
			if appIcon != nil {
				ui.Image(c, appIcon).Size(appIconSize, appIconSize)
			}
			ui.Text(c, L.AppName()).FontSize(16).Bold()
		})
		ui.Box(c).Height(6).Shrink(0)
		for _, it := range mainPages {
			a.navRow(c, it.page, it.icon, it.label(), nil)
		}
		ui.Box(c.Key("sb-space")).Grow(1).FillWidth().DragWindow()
		n := a.k.Jobs.Active()
		a.navRow(c, PageTasks, iconTasks, L.NavTasks(), func() {
			if n > 0 {
				ui.Box(c).Padding(1, 8).Radius(10).Background(t.Accent).Children(func() {
					ui.Text(c, itoa(n)).FontSize(sizeSmall).Bold().TextColor(t.AccentText)
				})
			}
		})
		ui.Row(c.Key("sb-foot")).Shrink(0).Gap(2).Padding(4, 0, 0, 2).AlignItems(ui.Center).Children(func() {
			a.themeButton(c)
			if iconButton(c, "sb-settings", iconSettings, L.NavSettings(), a.settingsOpen) {
				a.openSettings(setGeneral)
			}
			ui.Box(c).Grow(1)
			small(c, "v"+appVersion())
		})
	})
}

// navRow is one page in the sidebar: its icon, its name and, on the right,
// what trail draws (a count).
func (a *App) navRow(c *ui.Context, page string, icon *ui.SVG, label string, trail func()) {
	t := c.Theme()
	row := ui.ButtonBase(c.Key("nav-"+page)).Height(navRowH).FillWidth().Radius(7).Padding(0, 12).Label(label)
	on := a.page == page
	fg := t.Text
	switch {
	case on:
		row.Background(t.Accent.Alpha(0.13))
		fg = t.Accent
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(navIconSize, navIconSize).TextColor(fg)
			txt := ui.Text(c, label).TextColor(fg).SingleLine().Grow(1)
			if on {
				txt.FontWeight(600)
			}
			if trail != nil {
				trail()
			}
		})
	})
	if row.Clicked() {
		a.page = page
	}
}

// themeButton cycles the appearance: the system's, light, dark.
func (a *App) themeButton(c *ui.Context) {
	a.loadPrefs()
	mode := a.settings.prefs.Theme
	icon, next := iconSunMoon, "light"
	switch mode {
	case "light":
		icon, next = iconSun, "dark"
	case "dark":
		icon, next = iconMoon, "system"
	}
	if iconButton(c, "sb-theme", icon, L.SetTheme()+": "+themeLabel(mode), false) {
		a.setTheme(next)
	}
}

// paneHead is what the pane's header shows: the page's icon, title and one
// line, a status pill, and the page's actions before the window buttons.
type paneHead struct {
	icon    *ui.SVG
	title   string
	sub     string
	crumb   string // a link above the title back to the parent page
	onCrumb func()
	badge   func()
	actions func()
}

// head draws the pane's header. Its title and the room after it drag the
// window (a double click maximizes it).
func (a *App) head(c *ui.Context, h paneHead) {
	t := c.Theme()
	ui.Row(c.Key("pane-head")).Height(paneHeaderH+12).Shrink(0).FillWidth().Padding(6, 8, 0, panePadX).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Column(c.Key("pane-title")).MinWidth(0).Shrink(1).Gap(1).DragWindow().Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if h.icon != nil && h.crumb == "" {
					ui.Icon(c, h.icon).Size(20, 20).TextColor(t.TextMuted)
				}
				if h.crumb != "" {
					link := ui.ButtonBase(c.Key("crumb")).Label(h.crumb)
					link.Children(func() {
						col := t.TextMuted
						if link.Hovered() {
							col = t.Accent
						}
						ui.Text(c, h.crumb).FontSize(sizeTitle).TextColor(col).SingleLine()
					})
					if link.Clicked() && h.onCrumb != nil {
						h.onCrumb()
					}
					ui.Icon(c, iconChevron).Size(16, 16).TextColor(t.TextMuted)
				}
				ui.Text(c, h.title).FontSize(sizeTitle).Bold().SingleLine().Shrink(0)
			})
			if h.badge != nil || h.sub != "" {
				ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
					if h.badge != nil {
						h.badge()
					}
					if h.sub != "" {
						small(c, h.sub).SingleLine().Shrink(1).MinWidth(0).Tooltip(h.sub)
					}
				})
			}
		})
		ui.Box(c.Key("pane-drag")).Grow(1).FillHeight().DragWindow()
		if h.actions != nil {
			ui.Row(c.Key("pane-actions")).Gap(8).Shrink(0).AlignItems(ui.Center).Children(h.actions)
		}
		a.windowButtons(c, "win", false)
	})
}

// body is the pane's content under its header, padded; it fills the rest.
func body(c *ui.Context, fn func()) ui.Element {
	return ui.Column(c.Key("pane-body")).Grow(1).FillWidth().MinHeight(0).Padding(12, panePadX, panePadX, panePadX).Gap(16).Children(fn)
}

// scrollBody is body that scrolls.
func scrollBody(c *ui.Context, fn func()) ui.Element {
	return ui.Scroll(c.Key("pane-scroll")).Grow(1).FillWidth().MinHeight(0).Padding(12, panePadX, panePadX, panePadX).Gap(16).Children(fn)
}

// windowButtons are minimize, maximize or restore, and close on Windows,
// after a separator at the end of the header; over the Settings sheet's
// dimming (onScrim) they have light glyphs and no separator.
func (a *App) windowButtons(c *ui.Context, key string, onScrim bool) {
	if !ownWindowButtons || a.host == nil {
		return
	}
	t := c.Theme()
	button := func(name string, glyph *ui.SVG, label string) ui.Element {
		b := ui.ButtonBase(c.Key(key+"-"+name)).Size(winButtonW, winButtonH).Radius(6).Center().Shrink(0).Label(label)
		fg, hover := t.TextMuted, t.SurfaceHover
		if onScrim {
			fg, hover = scrimGlyph, scrimHover
		}
		switch {
		case b.Hovered() && name == "close":
			b.Background(closeHover)
			fg = closeHoverText
		case b.Hovered():
			b.Background(hover)
			if !onScrim {
				fg = t.Text
			}
		}
		b.Children(func() { ui.Icon(c, glyph).Size(winGlyph, winGlyph).TextColor(fg) })
		return b
	}
	if !onScrim {
		ui.Box(c).Size(1, 18).Shrink(0).Margin(0, 4).Background(t.Border)
	}
	if button("min", winMinimize, L.WindowMinimize()).Tooltip(L.WindowMinimize()).Clicked() {
		a.host.Window(WindowMinimize)
	}
	glyph, label := winMaximize, L.WindowMaximize()
	if a.host.Maximized() {
		glyph, label = winRestore, L.WindowRestore()
	}
	if button("max", glyph, label).MaximizeButton().Clicked() {
		a.host.Window(WindowToggleMaximize)
	}
	if button("close", winClose, L.WindowClose()).Tooltip(L.WindowClose()).Clicked() {
		a.host.Window(WindowClose)
	}
}
