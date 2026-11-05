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

// terminalView draws the Terminal page: tabs of sessions and a launcher.
func (a *App) terminalView(c *ui.Context) {
	p := &a.term
	t := c.Theme()
	a.header(c, L.NavTerminal(), func() {
		ui.MenuButton(c, L.TermNew(), func(m *ui.Menu) {
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
		})
	})
	if !a.wsl.loaded {
		a.loadDistros()
	}
	if len(p.sessions) == 0 {
		muted(c, L.TermNone())
		return
	}
	ui.Row(c).FillWidth().Gap(4).Children(func() {
		for i, s := range p.sessions {
			idx := i
			tab := ui.Row(c.Key(s.id)).Gap(4).Padding(4, 10).Radius(6).AlignItems(ui.Center)
			if i == p.active {
				tab.Background(t.Surface).Border(1, t.Border)
			}
			tab.Children(func() {
				title := s.title
				if s.term.Done() {
					title += " · " + L.TermEnded()
				}
				ui.Button(c, title).OnClick(func() { p.active = idx })
				ui.Button(c, "×").Label(L.Close()).OnClick(func() { a.closeTerminal(idx) })
			})
		}
	})
	if p.active >= 0 && p.active < len(p.sessions) {
		s := p.sessions[p.active]
		ui.Column(c.Key("term-" + itoa(s.id))).Grow(1).FillWidth().Children(func() {
			s.term.View(c).Fill().AutoFocus()
		})
	}
}
