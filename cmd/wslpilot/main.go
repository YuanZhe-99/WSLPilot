// wslpilot is the desktop GUI: one window with MyGo's native UI. See
// doc/en-us/architecture.md for the processes and packages behind it.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/capability"
	"github.com/YuanZhe-99/WSLPilot/internal/gui"
	"github.com/YuanZhe-99/WSLPilot/internal/version"
)

func main() {
	mygo.App.SetName("WSLPilot")
	mygo.App.SetVersion(version.Version)
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.Theme.SetSource(mygo.ThemeSystem)

	a := gui.New(capability.NewProber().Probe, nil)
	var win *mygo.Window
	mygo.App.OnSecondInstance(func([]string, string) {
		if win != nil {
			win.Show()
			win.Focus()
		}
	})
	mygo.App.WhenReady(func() {
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "WSLPilot",
			Width:     1100,
			Height:    720,
			MinWidth:  880,
			MinHeight: 560,
			StateKey:  "main",
			Content:   ui.View(a.View),
		})
		a.SetUpdate(win.Update)
		a.Refresh()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
