package gui

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

// Type scale and layout tokens (doc/en-us/gui.md: body 14, nothing below 12).
const (
	sizeSmall   float32 = 12
	sizeBody    float32 = 14
	sizeTitle   float32 = 22
	sizeHeading float32 = 15
	sizeStat    float32 = 24

	gap      float32 = 12
	smallGap float32 = 6

	cardRadius  float32 = 10
	cardPadding float32 = 16

	sidebarWidth float32 = 232
	navRowH      float32 = 38
	navIconSize  float32 = 16
	appIconSize  float32 = 26

	paneMargin  float32 = 8  // around the pane, from the window's edges
	paneRadius  float32 = 12 // the pane's corners
	paneHeaderH float32 = 52 // the pane's header, which is the window's title bar
	panePadX    float32 = 24 // the pane's body, from its sides
	paneButton  float32 = 30 // an icon button in the header or sidebar

	winButtonW float32 = 44 // a window button the view draws on Windows
	winButtonH float32 = 32
	winGlyph   float32 = 10

	detailWidth  float32 = 400
	dialogWidth  float32 = 540
	editorHeight float32 = 360
	tableRowH    float32 = 40
	barH         float32 = 8
)

// Shadows and fixed colors.
var (
	paneShadow     = ui.RGBA(16, 24, 40, 0.07)
	closeHover     = ui.RGB(196, 43, 28)
	closeHoverText = ui.RGB(255, 255, 255)
	sheetScrim     = ui.RGBA(0, 0, 0, 0.4)
	sheetShadow    = ui.RGBA(0, 0, 0, 0.3)
	scrimGlyph     = ui.RGB(244, 245, 247)
	scrimHover     = ui.RGBA(255, 255, 255, 0.14)
	reclaimColor   = ui.Hex("#F2C14E")
	consoleBg      = ui.Hex("#17181C")
	consoleText    = ui.Hex("#E6E7EA")
	consoleWarn    = ui.Hex("#F2B04E")
	consoleErr     = ui.Hex("#FF7B72")
)

// ownWindowButtons: on Windows the view draws minimize, maximize and close
// in the pane's header (the system title bar is hidden).
var ownWindowButtons = runtime.GOOS == "windows"

// windowBase is the window's color where it has no material.
func windowBase(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Hex("#111113")
	}
	return t.Surface
}

// tileBackground is the face of an icon tile: a shade off what it sits on.
func tileBackground(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(255, 255, 255, 0.07)
	}
	return ui.RGBA(16, 24, 40, 0.05)
}

// tone is the meaning of a colored element.
type tone int

const (
	toneNeutral tone = iota
	toneGood
	toneWarn
	toneBad
	toneAccent
	// toneOnAccent is text over the accent fill of a selected row.
	toneOnAccent
)

// toneColor maps a tone to the theme.
func toneColor(t *ui.Theme, k tone) ui.Color {
	switch k {
	case toneGood:
		return t.Success
	case toneWarn:
		return t.Warning
	case toneBad:
		return t.Danger
	case toneAccent:
		return t.Accent
	case toneOnAccent:
		return t.AccentText
	}
	return t.TextMuted
}

// card draws a titled panel around the content body builds.
func card(c *ui.Context, title string, body func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).FillWidth().Gap(10).AlignItems(ui.Start).Padding(cardPadding).Radius(cardRadius).
		Background(t.Background).Border(1, t.Border).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(sizeHeading).Bold()
		}
		body()
	})
}

// cardHead is a card's title row with something on its right.
func cardHead(c *ui.Context, title string, right func()) {
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(sizeHeading).Bold().Grow(1)
		if right != nil {
			right()
		}
	})
}

// muted draws secondary text.
func muted(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(c.Theme().TextMuted)
}

// small draws small secondary text.
func small(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(sizeSmall).TextColor(c.Theme().TextMuted)
}

// errorText draws an error line.
func errorText(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(c.Theme().Danger)
}

// dangerButton is a button in the danger color.
func dangerButton(c *ui.Context, label string) ui.Element {
	t := c.Theme()
	return ui.Button(c, label).TextColor(t.Danger).Border(1, t.Danger.Alpha(0.45))
}

// pill is a small rounded label in a tone, as the design's "Ready".
func pill(c *ui.Context, text string, k tone) {
	col := toneColor(c.Theme(), k)
	ui.Box(c).Padding(2, 10).Radius(999).Background(col.Alpha(0.13)).Shrink(0).Children(func() {
		ui.Text(c, text).FontSize(sizeSmall).Bold().TextColor(col).NoWrap()
	})
}

// dot is a filled circle.
func dot(c *ui.Context, col ui.Color, d float32) {
	ui.Box(c).Size(d, d).Radius(d / 2).Background(col).Shrink(0)
}

// stateText is a status dot and its label, as the tables' State column.
func stateText(c *ui.Context, label string, k tone) {
	col := toneColor(c.Theme(), k)
	ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
		dot(c, col, 8)
		txt := ui.Text(c, label).SingleLine()
		if k != toneNeutral {
			txt.TextColor(col).FontWeight(600)
		} else {
			txt.TextColor(c.Theme().TextMuted)
		}
	})
}

// bar is a horizontal bar of segments (fractions of the whole) over a
// track, as the dashboard's disk usage.
func bar(c *ui.Context, h float32, segs ...barSeg) {
	t := c.Theme()
	ui.Box(c).FillWidth().Height(h).Radius(h / 2).Background(t.SurfaceHover).Clip().Draw(func(p *ui.Painter, r ui.Rect) {
		x := r.X
		for _, s := range segs {
			w := float32(min(max(s.frac, 0), 1)) * r.W
			if w <= 0 {
				continue
			}
			p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: r.H}, s.col, 0)
			x += w
		}
	})
}

// barSeg is one segment of a bar.
type barSeg struct {
	frac float64
	col  ui.Color
}

// legend is a colored square and a label.
func legend(c *ui.Context, col ui.Color, label string) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(10, 10).Radius(2).Background(col)
		small(c, label)
	})
}

// iconButton is a borderless icon button (header, sidebar), named for
// assistive technology by its tooltip; on marks the current page.
// Returns: whether it was clicked.
func iconButton(c *ui.Context, key string, icon *ui.SVG, label string, on bool) bool {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(paneButton, paneButton).Radius(7).Center().Shrink(0).Label(label).Tooltip(label)
	fg := t.TextMuted
	if on || b.Hovered() {
		b.Background(t.SurfaceHover)
		fg = t.Text
	}
	b.Children(func() { ui.Icon(c, icon).Size(navIconSize, navIconSize).TextColor(fg) })
	return b.Clicked()
}

// actionButton is a button with an icon before its label.
// Returns: the button (Clicked, Disabled…).
func actionButton(c *ui.Context, icon *ui.SVG, label string, primary bool) ui.Element {
	var b ui.Element
	if primary {
		b = ui.PrimaryButton(c, "")
	} else {
		b = ui.Button(c, "")
	}
	b.Label(label).Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ico := ui.Icon(c, icon).Size(14, 14)
			txt := ui.Text(c, label).NoWrap()
			if primary {
				ico.TextColor(c.Theme().AccentText)
				txt.TextColor(c.Theme().AccentText).FontWeight(600)
			}
		})
	})
	return b
}

// iconTile is the rounded square with an icon a row starts with.
func iconTile(c *ui.Context, icon *ui.SVG, k tone) {
	t := c.Theme()
	bg, fg := tileBackground(t), t.TextMuted
	if k != toneNeutral {
		col := toneColor(t, k)
		bg, fg = col.Alpha(0.13), col
	}
	ui.Box(c).Size(32, 32).Shrink(0).Radius(8).Center().Background(bg).Children(func() {
		ui.Icon(c, icon).Size(navIconSize, navIconSize).TextColor(fg)
	})
}

// callout is a tinted strip with an icon, words and actions: the accent for
// something to decide, the warning color for a problem.
func callout(c *ui.Context, key string, k tone, icon *ui.SVG, msg string, actions func()) {
	col := toneColor(c.Theme(), k)
	ui.Row(c.Key(key)).FillWidth().Shrink(0).Padding(10, 12, 10, 14).Radius(10).Border(1, col.Alpha(0.5)).Background(col.Alpha(0.09)).
		Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon).Size(navIconSize, navIconSize).TextColor(col)
		ui.Text(c, msg).Grow(1).MinWidth(0)
		if actions != nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(actions)
		}
	})
}

// emptyState is a centered icon, title and sentence where a list is empty.
func emptyState(c *ui.Context, icon *ui.SVG, title, about string, action func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Padding(48, 24).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(56, 56).Radius(28).Center().Background(tileBackground(t)).Children(func() {
			ui.Icon(c, icon).Size(24, 24).TextColor(t.TextMuted)
		})
		ui.Text(c, title).FontSize(sizeHeading).Bold()
		if about != "" {
			muted(c, about).TextAlign(ui.Center).MaxWidth(440)
		}
		if action != nil {
			action()
		}
	})
}

// actions lays buttons out in a row that wraps.
func actions(c *ui.Context, body func()) {
	ui.Row(c).FillWidth().Wrap().Gap(8).Children(body)
}

// kv draws a label and a value.
func kv(c *ui.Context, k, v string) {
	ui.Row(c.Key(k)).FillWidth().Gap(smallGap).Children(func() {
		muted(c, k).Width(130).Shrink(0)
		ui.Text(c, v).Grow(1).MinWidth(0).Selectable()
	})
}

// selTone is k, or neutral on a selected table row, whose accent fill
// carries its own text color: a colored label there would not be readable.
func selTone(k tone, selected bool) tone {
	if selected {
		return toneOnAccent
	}
	return k
}
