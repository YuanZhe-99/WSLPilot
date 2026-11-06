package gui

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// startJob runs fn as a job and opens the job panel.
// Inputs: refresh runs on the UI thread when the job ends (reload the
// page's data).
func (a *App) startJob(title string, fn jobs.Func, refresh func()) {
	j := a.k.Jobs.Start(title, fn)
	go func() {
		s := j.Wait()
		a.update(func() {
			if refresh != nil {
				refresh()
			}
			switch s.State {
			case jobs.Succeeded:
				a.notify(L.JobDone(s.Title), false)
			case jobs.Failed:
				a.notify(L.JobFailed(s.Title), true)
			}
		})
	}()
}

// jobFinished notifies the system when the window may not be in front.
func (a *App) jobFinished(s jobs.Snapshot) {
	if a.host == nil || s.Ended.Sub(s.Started).Seconds() < 20 {
		return
	}
	if s.State == jobs.Failed {
		a.host.Notify(s.Title, L.JobFailed(s.Title))
	} else if s.State == jobs.Succeeded {
		a.host.Notify(s.Title, L.JobDone(s.Title))
	}
}

// friendlyError turns an error into text for the interface; the raw
// command and exit code stay in the job log.
func friendlyError(err error) string {
	if err == nil {
		return ""
	}
	if err == context.Canceled {
		return L.JobStateCancelled()
	}
	msg := err.Error()
	// procexec.ExitError: "wsl.exe args: exit code N: message" → message.
	if i := strings.LastIndex(msg, ": exit code "); i >= 0 {
		rest := msg[i+len(": exit code "):]
		if j := strings.Index(rest, ": "); j >= 0 {
			msg = rest[j+2:]
		}
	}
	if i := strings.Index(msg, "\nError code:"); i >= 0 {
		msg = msg[:i]
	}
	return strings.TrimSpace(msg)
}

// tasksView is the Tasks page: running jobs with progress and Cancel,
// finished ones with their result, the log of each.
func (a *App) tasksView(c *ui.Context) {
	list := a.k.Jobs.List()
	a.head(c, paneHead{icon: iconTasks, title: L.NavTasks(), sub: L.TasksAbout(), actions: func() {
		if ui.Button(c, L.JobsClear()).Disabled(len(list) == a.k.Jobs.Active()).Clicked() {
			a.k.Jobs.ClearFinished()
		}
	}})
	scrollBody(c, func() {
		if len(list) == 0 {
			emptyState(c, iconTasks, L.JobsNone(), L.TasksEmpty(), nil)
			return
		}
		for _, s := range list {
			a.taskCard(c, s)
		}
	})
}

// taskCard is one job.
func (a *App) taskCard(c *ui.Context, s jobs.Snapshot) {
	t := c.Theme()
	ui.Column(c.Key(s.ID)).FillWidth().Gap(8).Padding(14, 16).Radius(cardRadius).Border(1, t.Border).Background(t.Background).Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			switch s.State {
			case jobs.Running:
				ui.Spinner(c).Size(16, 16)
			case jobs.Succeeded:
				ui.Icon(c, iconCheck).Size(16, 16).TextColor(t.Success)
			case jobs.Failed:
				ui.Icon(c, iconAlert).Size(16, 16).TextColor(t.Danger)
			default:
				ui.Icon(c, iconClose).Size(16, 16).TextColor(t.TextMuted)
			}
			ui.Text(c, s.Title).FontWeight(600).Grow(1).SingleLine()
			small(c, s.Started.Local().Format("15:04:05"))
			if s.State == jobs.Running {
				id := s.ID
				if ui.Button(c, L.Cancel()).Clicked() {
					if j := a.k.Jobs.Get(id); j != nil {
						j.Cancel()
					}
				}
			}
		})
		switch s.State {
		case jobs.Running:
			if s.Progress >= 0 {
				bar(c, 6, barSeg{s.Progress, t.Accent})
			} else {
				ui.Progress(c, -1).Label(s.Title).FillWidth()
			}
			if s.Step != "" {
				small(c, s.Step)
			}
		case jobs.Failed:
			errorText(c, friendlyError(s.Err)).Selectable()
		case jobs.Cancelled:
			small(c, L.JobStateCancelled())
		}
		if log := strings.TrimSpace(s.Log); log != "" {
			tail := log
			if len(tail) > 4000 {
				tail = "…" + tail[len(tail)-4000:]
			}
			console(c, "log", tail, 160)
		}
	})
}

// console is a dark monospace text area, as the design's log view.
func console(c *ui.Context, key, text string, maxH float32) ui.Element {
	return ui.Scroll(c.Key(key)).FillWidth().MaxHeight(maxH).Padding(10, 12).Radius(8).Background(consoleBg).Children(func() {
		for i, line := range strings.Split(text, "\n") {
			col := consoleText
			l := strings.ToLower(line)
			switch {
			case strings.Contains(l, "error") || strings.Contains(l, "fail"):
				col = consoleErr
			case strings.Contains(l, "warn"):
				col = consoleWarn
			}
			ui.Text(c.Key(i), line).Font("monospace").FontSize(12).TextColor(col).Selectable()
		}
	})
}
