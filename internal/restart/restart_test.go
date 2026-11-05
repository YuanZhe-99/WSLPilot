package restart

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		policy  string
		code    int
		stopped bool
		n       int
		want    bool
	}{
		{Always, 0, false, 0, true},
		{Always, 137, true, 0, false},
		{UnlessStopped, 1, false, 9, true},
		{OnFailure, 0, false, 0, false},
		{OnFailure, 2, false, 50, true},
		{"on-failure:3", 1, false, 2, true},
		{"on-failure:3", 1, false, 3, false},
		{No, 1, false, 0, false},
		{"", 1, false, 0, false},
	}
	for _, c := range cases {
		if got := Decide(c.policy, c.code, c.stopped, c.n); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}

type fakeEngine struct {
	events  []wslc.Event
	mu      sync.Mutex
	started []string
}

func (f *fakeEngine) Events(ctx context.Context, fn func(wslc.Event)) error {
	for _, e := range f.events {
		fn(e)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeEngine) Start(ctx context.Context, id string) error {
	f.mu.Lock()
	f.started = append(f.started, id)
	f.mu.Unlock()
	return nil
}

func (f *fakeEngine) InspectContainers(ctx context.Context, ids ...string) ([]wslc.Container, error) {
	return nil, nil
}

func ev(action, id, policy, code string) wslc.Event {
	var e wslc.Event
	e.Type, e.Action, e.Actor.ID = "container", action, id
	e.Actor.Attributes = map[string]string{Label: policy, "exitCode": code, "name": id}
	return e
}

func TestWatcherRestartsCrashNotUserStop(t *testing.T) {
	f := &fakeEngine{events: []wslc.Event{
		ev("die", "crash", Always, "1"),
		ev("kill", "stopped", Always, ""), ev("die", "stopped", Always, "137"),
		ev("die", "plain", "", "1"),
	}}
	w := &Watcher{E: f}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	w.Run(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.started) != 1 || f.started[0] != "crash" {
		t.Errorf("started %v", f.started)
	}
}
