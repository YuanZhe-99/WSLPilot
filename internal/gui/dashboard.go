package gui

import (
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/upstream"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// dashboard shows what the last probe found and what needs attention.
func (a *App) dashboard(c *ui.Context) {
	a.header(c, L.NavDashboard(), func() {
		ui.Button(c, L.DashRefresh()).Disabled(a.probing).OnClick(a.Refresh)
	})
	r := a.report
	if r == nil {
		ui.Row(c).Gap(smallGap).Children(func() {
			ui.Spinner(c)
			muted(c, L.DashChecking())
		})
		return
	}
	ui.Scroll(c).Grow(1).FillWidth().Gap(gap).Children(func() {
		ui.Grid(c).FillWidth().Columns(2).Gap(gap).Children(func() {
			card(c, L.DashWSL(), func() {
				if !r.HasWSL() {
					muted(c, L.DashNotFound())
					ui.Button(c, L.DashInstallWSL()).OnClick(func() { a.host.OpenURL("https://aka.ms/wslinstall") })
					return
				}
				ui.Text(c, L.DashWSLVersion(r.WSL.WSL, r.WSL.Kernel))
				muted(c, L.DashDistros(len(r.Distros)))
				if upstream.NewerThanTested(r.WSL.WSL) {
					ui.Text(c, L.DashUntested(r.WSL.WSL)).TextColor(c.Theme().Warning)
				}
				ui.Button(c, L.DashOpen()).OnClick(func() { a.page = PageWSL })
			})
			card(c, L.DashWSLc(), func() {
				if !r.HasWSLc() {
					muted(c, L.DashNotFound())
					muted(c, L.DashWSLcHint())
					return
				}
				ui.Text(c, L.DashWSLcVersion(r.WSLc.Client.Version))
				if a.ctr.loaded {
					running := 0
					for _, s := range a.ctr.list {
						if s.Running() {
							running++
						}
					}
					muted(c, L.DashContainers(running, len(a.ctr.list)))
				}
				ui.Button(c, L.DashOpen()).OnClick(func() { a.page = PageWSLc })
			})
			card(c, L.DashCompose(), func() {
				if r.NativeCompose {
					ui.Text(c, L.DashComposeNative())
				} else {
					ui.Text(c, L.DashComposeBuiltin())
				}
				ui.Button(c, L.DashOpen()).OnClick(func() { a.page = PageCompose })
			})
			card(c, L.NavBackups(), func() {
				a.ensurePlans()
				ps := a.bk.plans
				if len(ps) == 0 {
					muted(c, L.DashNoPlans())
				}
				for _, p := range ps {
					st := a.bk.states[p.ID]
					line := L.DashPlanNever(p.Name)
					switch {
					case st.LastResult == "ok":
						line = L.DashPlanOK(p.Name, st.LastSuccess.Local().Format("2006-01-02 15:04"))
					case st.LastResult != "":
						line = L.DashPlanFailed(p.Name)
					}
					t := ui.Text(c.Key(p.ID), line)
					if st.LastResult != "" && st.LastResult != "ok" {
						t.TextColor(c.Theme().Danger)
					}
				}
				ui.Button(c, L.DashOpen()).OnClick(func() { a.page = PageBackups })
			})
		})
	})
}
