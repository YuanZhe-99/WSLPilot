// wslpilot is the desktop GUI: one window with MyGo's native UI. See
// doc/en-us/architecture.md for the processes and packages behind it.
package main

import (
	"context"
	_ "embed"
	"errors"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/core"
	"github.com/YuanZhe-99/WSLPilot/internal/gui"
	"github.com/YuanZhe-99/WSLPilot/internal/scheduler"
	"github.com/YuanZhe-99/WSLPilot/internal/version"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

//go:embed tray.png
var trayIcon []byte

func main() {
	mygo.App.SetName("WSLPilot")
	mygo.App.SetVersion(version.Version)
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.Theme.SetSource(mygo.ThemeSystem)

	k, err := core.New()
	if err != nil {
		log.Fatal(err)
	}
	k.WireCompose(false)
	h := &host{}
	k.Scheduler.Notify = h.Notify
	a := gui.New(k, h, func(fn func()) { fn() })
	var win *mygo.Window
	quitting := false
	show := func() {
		if win != nil {
			win.Show()
			win.Focus()
		}
	}
	mygo.App.OnSecondInstance(func([]string, string) { show() })
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })
	mygo.App.WhenReady(func() {
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "WSLPilot",
			Width:     1200,
			Height:    780,
			MinWidth:  960,
			MinHeight: 600,
			StateKey:  "main",
			Content:   ui.View(a.View),
		})
		h.win = win
		a.SetUpdate(win.Update)
		// Closing the window keeps WSLPilot in the tray while jobs run or
		// the tray exists; Quit in the tray menu ends it.
		tray, terr := mygo.NewTray(mygo.TrayOptions{
			Icon:    trayIcon,
			ToolTip: "WSLPilot",
			Menu: mygo.NewMenu([]*mygo.MenuItem{
				{Label: L.TrayOpen(), Click: func(*mygo.MenuItem, *mygo.Window) { show() }},
				mygo.Separator(),
				{Label: L.TrayQuit(), Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
			}),
		})
		if terr != nil {
			log.Println("tray:", terr)
		} else {
			tray.OnClick(show)
			win.OnClose(func(e *mygo.CloseEvent) {
				if !quitting {
					e.PreventDefault()
					win.Hide()
				}
			})
		}
		a.Refresh()
		go wire(k, win)
		go runPlans(k)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// wire finishes the Compose setup once the probe knows about native Compose.
func wire(k *core.Core, win *mygo.Window) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	native, _ := k.WSLc.HasNativeCompose(ctx)
	if native {
		win.Update(func() { k.SetNativeCompose(native) })
	}
}

// runPlans runs due backup plans every minute while the window is open; the
// lock keeps it from running a plan at the same time as the agent.
func runPlans(k *core.Core) {
	for {
		if err := k.Scheduler.RunDue(context.Background()); err != nil && !errors.Is(err, scheduler.ErrBusy) {
			log.Println("plans:", err)
		}
		time.Sleep(time.Minute)
	}
}
