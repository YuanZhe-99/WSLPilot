// Package restart implements container restart policies, which WSLc lacks
// (`wslc run` has no --restart, verified on 3.0.3.0). A container opts in
// with the label io.wslpilot.restart; the watcher follows `wslc events`
// and starts it again after it dies.
package restart

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// Label holds a container's restart policy.
const Label = "io.wslpilot.restart"

// Policy values, as in Compose.
const (
	No            = "no"
	Always        = "always"
	OnFailure     = "on-failure"
	UnlessStopped = "unless-stopped"
)

// Decide reports whether a container that died should be restarted.
// Inputs: policy from the label; exitCode of the death; userStopped when a
// stop or kill by the user preceded it.
// Notes: "on-failure[:N]" allows N restarts (0: unlimited); "always" and
// "unless-stopped" both honor a user's stop until the container is
// started again, as the WSLPilot agent cannot tell a Docker daemon
// restart apart.
func Decide(policy string, exitCode int, userStopped bool, restarts int) bool {
	if userStopped {
		return false
	}
	switch {
	case policy == Always || policy == UnlessStopped:
		return true
	case policy == OnFailure:
		return exitCode != 0
	case len(policy) > len(OnFailure)+1 && policy[:len(OnFailure)+1] == OnFailure+":":
		max, err := strconv.Atoi(policy[len(OnFailure)+1:])
		return exitCode != 0 && (err != nil || max == 0 || restarts < max)
	}
	return false
}

// Engine is what the watcher needs from the wslc adapter.
type Engine interface {
	Events(ctx context.Context, fn func(wslc.Event)) error
	Start(ctx context.Context, id string) error
	InspectContainers(ctx context.Context, ids ...string) ([]wslc.Container, error)
}

// Watcher restarts containers by their policy.
type Watcher struct {
	E   Engine
	Log func(string)
	// Backoff between restarts of the same container grows from 1s to 1m
	// so a crashing container does not spin.
	mu       sync.Mutex
	stopped  map[string]bool
	restarts map[string]int
	last     map[string]time.Time
}

func (w *Watcher) log(s string) {
	if w.Log != nil {
		w.Log(s)
	}
}

// Run follows events until ctx ends, reconnecting when the stream drops.
func (w *Watcher) Run(ctx context.Context) error {
	w.stopped, w.restarts, w.last = map[string]bool{}, map[string]int{}, map[string]time.Time{}
	for ctx.Err() == nil {
		err := w.E.Events(ctx, func(e wslc.Event) { w.handle(ctx, e) })
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.log("event stream ended: " + errString(err) + "; reconnecting")
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
	return ctx.Err()
}

// handle reacts to one event.
func (w *Watcher) handle(ctx context.Context, e wslc.Event) {
	if e.Type != "container" {
		return
	}
	id := e.Actor.ID
	policy := e.Actor.Attributes[Label]
	w.mu.Lock()
	defer w.mu.Unlock()
	switch e.Action {
	case "stop", "kill":
		// A kill with SIGTERM/SIGKILL that WSLPilot did not send is a user
		// stop; a process dying by itself only produces "die".
		w.stopped[id] = true
	case "start":
		if time.Since(w.last[id]) > 2*time.Second {
			w.stopped[id] = false // started by someone: policy active again
		}
	case "die":
		if policy == "" || policy == No {
			return
		}
		code, _ := strconv.Atoi(e.Actor.Attributes["exitCode"])
		if !Decide(policy, code, w.stopped[id], w.restarts[id]) {
			return
		}
		n := w.restarts[id]
		w.restarts[id] = n + 1
		delay := min(time.Second<<min(n, 6), time.Minute)
		name := e.Actor.Attributes["name"]
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			w.mu.Lock()
			if w.stopped[id] {
				w.mu.Unlock()
				return
			}
			w.last[id] = time.Now()
			w.mu.Unlock()
			if err := w.E.Start(ctx, id); err != nil {
				w.log("restart " + name + ": " + err.Error())
			} else {
				w.log("restarted " + name + " (" + policy + ")")
			}
		}()
	}
}

func errString(err error) string {
	if err == nil {
		return "closed"
	}
	return err.Error()
}
