package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobStates(t *testing.T) {
	m := &Manager{AuditPath: filepath.Join(t.TempDir(), "audit.jsonl")}
	ok := m.Start("ok", func(ctx context.Context, j *Job) error { j.Logf("hello"); return nil }).Wait()
	if ok.State != Succeeded || !strings.Contains(ok.Log, "hello") {
		t.Errorf("ok: %+v", ok)
	}
	bad := m.Start("bad", func(ctx context.Context, j *Job) error { return errors.New("boom") }).Wait()
	if bad.State != Failed || bad.Err == nil {
		t.Errorf("bad: %+v", bad)
	}
	p := m.Start("panic", func(ctx context.Context, j *Job) error { panic("x") }).Wait()
	if p.State != Failed {
		t.Errorf("panic: %+v", p)
	}
	j := m.Start("cancel", func(ctx context.Context, j *Job) error { <-ctx.Done(); return ctx.Err() })
	j.Cancel()
	if s := j.Wait(); s.State != Cancelled {
		t.Errorf("cancel: %+v", s)
	}
	if len(m.List()) != 4 || m.Active() != 0 {
		t.Errorf("list: %d active %d", len(m.List()), m.Active())
	}
	m.Audit("wsl.unregister", "wslpilot-test-x", "", nil)
	b, _ := os.ReadFile(m.AuditPath)
	if !strings.Contains(string(b), `"action":"wsl.unregister"`) {
		t.Errorf("audit: %s", b)
	}
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int64]string{5: "5 B", 1536: "1.5 KiB", 3 << 30: "3.0 GiB"} {
		if got := FormatBytes(n); got != want {
			t.Errorf("%d: %q", n, got)
		}
	}
}
