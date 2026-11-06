package gui

import (
	"github.com/egoist/mygo/ui"
)

// Layout of pages built from rows (Settings, the distribution settings).
const (
	pageColumnW float32 = 760 // a page's column, at most
	listRadius  float32 = 10  // a list of rows
	rowMinH     float32 = 56  // a row
	rowControlW float32 = 280 // a field on a row's right
	blockIndent float32 = 62  // a block under its row, lined up with its words
)

// pageColumn is a page's body: it scrolls, its column centred and no wider
// than pageColumnW.
func pageColumn(c *ui.Context, key string, fn func()) {
	ui.Scroll(c.Key(key)).Grow(1).FillWidth().MinHeight(0).Padding(8, 28, 28, 28).AlignItems(ui.Center).Children(func() {
		ui.Column(c.Key(key + "-col")).FillWidth().MaxWidth(pageColumnW).Children(fn)
	})
}

// pageIntro is a page's title and its one sentence on what it is for.
func pageIntro(c *ui.Context, title, about string) {
	ui.Text(c, title).FontSize(20).Bold().Margin(16, 0, 0, 0)
	if about != "" {
		muted(c, about).Margin(4, 0, 0, 0)
	}
}

// rows adds a list's rows, a line between each; hit, when set, tells
// whether a row is the setting a search leads to, to scroll it into view
// and tint it.
type rows struct {
	n   int
	hit func(title string) (tint, scroll bool)
}

// section is a titled group of rows: its muted head with aside on the
// right, then its rows in one rounded list.
func (r *rows) section(c *ui.Context, key, title string, aside func(), body func(r *rows)) {
	t := c.Theme()
	head := ui.Row(c.Key(key+"-head")).FillWidth().MinHeight(28).AlignItems(ui.Center).Padding(20, 4, 8, 4).Gap(gap)
	if r.hit != nil {
		if _, scroll := r.hit(title); scroll {
			head.ScrollIntoView()
		}
	}
	head.Children(func() {
		ui.Text(c, title).FontSize(sizeSmall).Bold().TextColor(t.TextMuted).Grow(1)
		if aside != nil {
			aside()
		}
	})
	bg := t.Background
	if t.Dark {
		bg = t.Surface
	}
	ui.Column(c.Key(key)).FillWidth().Radius(listRadius).Border(1, t.Border).Background(bg).Clip().Children(func() {
		body(&rows{hit: r.hit})
	})
}

// line draws the line above every row but the first.
func (r *rows) line(c *ui.Context) {
	if r.n > 0 {
		ui.Box(c).Height(1).FillWidth().Shrink(0).Background(c.Theme().Border)
	}
	r.n++
}

// row is one setting: its icon in a tile (nil for none), its title, one
// sentence, and its control on the right (nil for none).
func (r *rows) row(c *ui.Context, key string, icon *ui.SVG, title, about string, control func()) {
	r.rowTone(c, key, icon, title, about, toneNeutral, control)
}

// rowTone is row with its sentence in a tone (warning, danger…).
func (r *rows) rowTone(c *ui.Context, key string, icon *ui.SVG, title, about string, k tone, control func()) {
	t := c.Theme()
	r.line(c)
	e := ui.Row(c.Key(key)).FillWidth().MinHeight(rowMinH).Padding(10, 16).Gap(14).AlignItems(ui.Center)
	if r.hit != nil {
		if tint, scroll := r.hit(title); tint || scroll {
			if tint {
				e.Background(t.Accent.Alpha(0.1))
			}
			if scroll {
				e.ScrollIntoView()
			}
		}
	}
	e.Children(func() {
		if icon != nil {
			iconTile(c, icon, toneNeutral)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontWeight(500)
			if about != "" {
				col := t.TextMuted
				if k != toneNeutral {
					col = toneColor(t, k)
				}
				ui.Text(c, about).FontSize(sizeSmall).TextColor(col)
			}
		})
		if control != nil {
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(control)
		}
	})
}

// block is free content under the row before it, lined up with its words.
func (r *rows) block(c *ui.Context, key string, body func()) {
	ui.Column(c.Key(key)).FillWidth().Padding(0, 16, 14, blockIndent).Gap(8).Children(body)
}
