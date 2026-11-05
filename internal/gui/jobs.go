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
	a.jobsOpen = true
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

// jobsPanel lists jobs with progress, cancel and the log of the chosen one.
func (a *App) jobsPanel(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Width(jobsWidth).FillHeight().Padding(cardPadding).Gap(gap).Background(t.Surface).Border(1, t.Border).Children(func() {
		ui.Row(c).FillWidth().Children(func() {
			ui.Text(c, L.JobsTitle()).Bold().Grow(1)
			ui.Button(c, L.JobsClear()).OnClick(a.k.Jobs.ClearFinished)
			ui.Button(c, L.Close()).OnClick(func() { a.jobsOpen = false })
		})
		list := a.k.Jobs.List()
		if len(list) == 0 {
			muted(c, L.JobsNone())
		}
		ui.Scroll(c).Grow(1).FillWidth().Gap(gap).Children(func() {
			for _, s := range list {
				ui.Column(c.Key(s.ID)).FillWidth().Gap(4).Children(func() {
					ui.Text(c, s.Title).Bold()
					switch s.State {
					case jobs.Running:
						ui.Progress(c, s.Progress).Label(s.Title).FillWidth()
						if s.Step != "" {
							muted(c, s.Step)
						}
						id := s.ID
						ui.Button(c, L.Cancel()).OnClick(func() {
							if j := a.k.Jobs.Get(id); j != nil {
								j.Cancel()
							}
						})
					case jobs.Succeeded:
						ui.Text(c, L.JobStateDone()).TextColor(t.Success)
					case jobs.Cancelled:
						muted(c, L.JobStateCancelled())
					case jobs.Failed:
						errorText(c, L.JobStateFailed())
						ui.Text(c, friendlyError(s.Err)).Selectable()
					}
					if log := strings.TrimSpace(s.Log); log != "" {
						tail := log
						if len(tail) > 2000 {
							tail = "…" + tail[len(tail)-2000:]
						}
						ui.Text(c, tail).FontSize(11).TextColor(t.TextMuted).Selectable()
					}
				})
			}
		})
	})
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
