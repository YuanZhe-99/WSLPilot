// Command wslpilot-agent does WSLPilot's work while the window is closed.
// It runs as the signed-in user, never elevated.
//
//	wslpilot-agent run-due     run backup plans that are due, then exit
//	                           (started every 15 minutes by Task Scheduler)
//	wslpilot-agent watch       restart containers by their restart policy,
//	                           start Compose projects marked autostart, and
//	                           run due plans every 5 minutes (started at sign-in)
//	wslpilot-agent install     register the task and the sign-in entry
//	wslpilot-agent uninstall   remove them
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/core"
	"github.com/YuanZhe-99/WSLPilot/internal/restart"
	"github.com/YuanZhe-99/WSLPilot/internal/scheduler"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: wslpilot-agent run-due | watch | install | uninstall")
		os.Exit(2)
	}
	k, err := core.New()
	if err != nil {
		log.Fatal(err)
	}
	setupLog(k.Store.Path("logs/agent.log"))
	native, _ := k.WSLc.HasNativeCompose(context.Background())
	k.WireCompose(native)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	switch os.Args[1] {
	case "run-due":
		err = k.Scheduler.RunDue(ctx)
		if errors.Is(err, scheduler.ErrBusy) {
			err = nil // the GUI or another agent is on it
		}
	case "watch":
		err = watch(ctx, k)
	case "install":
		err = scheduler.Install(ctx, core.AgentPath())
	case "uninstall":
		err = scheduler.Uninstall(ctx)
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("%s: %v", os.Args[1], err)
		os.Exit(1)
	}
}

// watch runs the restart watcher, Compose autostart and periodic plans.
func watch(ctx context.Context, k *core.Core) error {
	if k.AutostartCompose != nil {
		go k.AutostartCompose(ctx, func(s string) { log.Print(s) })
	}
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			if err := k.Scheduler.RunDue(ctx); err != nil && !errors.Is(err, scheduler.ErrBusy) {
				log.Printf("run-due: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	w := &restart.Watcher{E: k.WSLc, Log: func(s string) { log.Print(s) }}
	return w.Run(ctx)
}

// setupLog appends the log to path, keeping it under 1 MiB.
func setupLog(path string) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	if st, err := os.Stat(path); err == nil && st.Size() > 1<<20 {
		os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		log.SetOutput(f)
	}
}
