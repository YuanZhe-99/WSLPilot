package gui

import (
	"github.com/egoist/mygo/ui"

	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// terminalPage holds the open terminal sessions.
type terminalPage struct {
	sessions []*termSession
	active   int
	nextID   int
}

// termSession is one tab.
type termSession struct {
	id    int
	title string
	argv  []string
	term  Term
}

// openTerminal starts argv in a new terminal tab and shows the page.
func (a *App) openTerminal(title string, argv []string) {
	t, err := a.host.Terminal(title, argv)
	if err != nil {
		a.notify(L.TermFailed(friendlyError(err)), true)
		return
	}
	p := &a.term
	p.nextID++
	p.sessions = append(p.sessions, &termSession{id: p.nextID, title: title, argv: argv, term: t})
	p.active = len(p.sessions) - 1
	a.page = PageTerminal
}

// closeTerminal ends a session.
func (a *App) closeTerminal(i int) {
	p := &a.term
	p.sessions[i].term.Close()
	p.sessions = append(p.sessions[:i], p.sessions[i+1:]...)
	if p.active >= len(p.sessions) {
		p.active = len(p.sessions) - 1
	}
}

// terminalView draws the Terminal page: a tab strip of sessions with a
// launcher, and the active session.
func (a *App) terminalView(c *ui.Context) {
	p := &a.term
	t := c.Theme()
	if !a.wsl.loaded {
		a.loadDistros()
	}
	a.head(c, paneHead{icon: iconTerminal, title: L.NavTerminal(), sub: L.TermSubtitle(), actions: func() {
		ui.PrimaryButton(c.Key("new"), L.TermNew()).Menu(func(m *ui.Menu) { a.terminalMenu(m) })
	}})
	if len(p.sessions) == 0 {
		body(c, func() {
			emptyState(c, iconTerminal, L.TermNone(), L.TermNoneHint(), func() {
				ui.Button(c.Key("new-empty"), L.TermNew()).Menu(func(m *ui.Menu) { a.terminalMenu(m) })
			})
		})
		return
	}
	ui.Column(c.Key("term")).Grow(1).FillWidth().MinHeight(0).Padding(4, panePadX, panePadX, panePadX).Gap(0).Children(func() {
		ui.Row(c.Key("tabs")).FillWidth().Gap(4).AlignItems(ui.End).Children(func() {
			for i, s := range p.sessions {
				idx := i
				b := ui.ButtonBase(c.Key("tab-"+itoa(s.id))).Padding(7, 8, 7, 12).Radius(8).Label(s.title)
				if i == p.active {
					b.Background(consoleBg)
				} else if b.Hovered() {
					b.Background(t.SurfaceHover)
				}
				b.Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						fg := t.TextMuted
						if i == p.active {
							fg = consoleText
						}
						ui.Icon(c, iconTerminal).Size(13, 13).TextColor(fg)
						title := s.title
						if s.term.Done() {
							title += " · " + L.TermEnded()
						}
						ui.Text(c, title).FontSize(13).TextColor(fg).NoWrap()
						x := ui.ButtonBase(c.Key("close-"+itoa(s.id))).Size(18, 18).Radius(4).Center().Label(L.Close()).Tooltip(L.Close())
						if x.Hovered() {
							x.Background(fg.Alpha(0.15))
						}
						x.Children(func() { ui.Icon(c, iconClose).Size(11, 11).TextColor(fg) })
						if x.Clicked() {
							a.closeTerminal(idx)
						}
					})
				})
				if b.Clicked() {
					p.active = idx
				}
			}
		})
		if p.active >= 0 && p.active < len(p.sessions) {
			s := p.sessions[p.active]
			ui.Column(c.Key("term-" + itoa(s.id))).Grow(1).FillWidth().MinHeight(0).Padding(8).Radius(cardRadius).Background(consoleBg).Children(func() {
				s.term.View(c).Fill().AutoFocus()
			})
		}
	})
}

// terminalMenu lists what a new terminal can open.
func (a *App) terminalMenu(m *ui.Menu) {
	for _, d := range a.wsl.distros {
		if m.Item(d.Name).Chosen() {
			a.openTerminal(d.Name, []string{"wsl.exe", "--distribution", d.Name, "--cd", "~"})
		}
	}
	m.Separator()
	for _, s := range a.ctr.list {
		if s.Running() && m.Item(s.Names+" (WSLc)").Chosen() {
			a.openTerminal(s.Names, []string{"wslc.exe", "container", "exec", "--interactive", "--tty", s.ID, "sh"})
		}
	}
	m.Separator()
	if m.Item("PowerShell").Chosen() {
		a.openTerminal("PowerShell", nil)
	}
}
