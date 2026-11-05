package scheduler

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/store"
)

func TestNext(t *testing.T) {
	loc := time.FixedZone("x", -5*3600)
	at := time.Date(2026, 11, 5, 10, 30, 0, 0, loc) // Thursday
	cases := []struct {
		s    Schedule
		want time.Time
	}{
		{Schedule{Every: Hourly, At: "00:15"}, time.Date(2026, 11, 5, 11, 15, 0, 0, loc)},
		{Schedule{Every: Daily, At: "11:00"}, time.Date(2026, 11, 5, 11, 0, 0, 0, loc)},
		{Schedule{Every: Daily, At: "02:00"}, time.Date(2026, 11, 6, 2, 0, 0, 0, loc)},
		{Schedule{Every: Weekly, At: "10:30", Weekday: time.Thursday}, time.Date(2026, 11, 12, 10, 30, 0, 0, loc)},
		{Schedule{Every: Weekly, At: "09:00", Weekday: time.Sunday}, time.Date(2026, 11, 8, 9, 0, 0, 0, loc)},
		{Schedule{Every: Monthly, At: "03:00", Day: 1}, time.Date(2026, 12, 1, 3, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		if got := c.s.Next(at); !got.Equal(c.want) {
			t.Errorf("%+v: got %v want %v", c.s, got, c.want)
		}
	}
	if (Schedule{Every: Daily, At: "25:00"}).Validate() == nil {
		t.Error("bad time accepted")
	}
}

func TestRetryDelay(t *testing.T) {
	r := Retry{Max: 5, Initial: time.Minute, Limit: 5 * time.Minute}
	got := []time.Duration{r.Delay(1), r.Delay(2), r.Delay(3), r.Delay(4)}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("delay %d = %v", i+1, got[i])
		}
	}
}

func emptyTar() []byte {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	tw.WriteHeader(&tar.Header{Name: "f", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644})
	tw.Write([]byte("x"))
	tw.Close()
	return b.Bytes()
}

func TestRunRetryRetentionHistory(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.Local)
	dest := filepath.Join(t.TempDir(), "out")
	fail := true
	s := &Service{
		Store: &store.Store{Dir: t.TempDir()},
		Now:   func() time.Time { return now },
		Do: func(ctx context.Context, tg Target, w backup.Writer, o backup.Options) (backup.Item, error) {
			if fail {
				return backup.Item{}, errors.New("boom")
			}
			return w.Write(ctx, bytes.NewReader(emptyTar()), backup.Meta{Source: backup.Source{Kind: tg.Kind, Name: tg.Name}, Time: now, Consistent: true, Plan: o.Plan})
		},
	}
	p := Plan{ID: "p1", Name: "Nightly", Enabled: true, Targets: []Target{{Kind: backup.SourceDistro, Name: "Debian"}},
		Destination: Destination{Kind: backup.KindArchive, Path: dest}, Schedule: Schedule{Every: Daily, At: "02:00"},
		Retention: backup.Policy{KeepLast: 2}, Retry: Retry{Max: 1, Initial: 10 * time.Minute}}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.Due(); len(due) != 0 {
		t.Fatal("due before its time")
	}
	now = now.Add(17 * time.Hour) // 03:00 next day
	if due, _ := s.Due(); len(due) != 1 {
		t.Fatal("not due")
	}
	if err := s.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := s.State("p1")
	if st.Attempt != 1 || !st.NextRun.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("retry not scheduled: %+v", st)
	}
	fail = false
	for i := 0; i < 4; i++ {
		now = now.Add(time.Hour)
		s.Run(context.Background(), p)
	}
	items, _ := backup.Scan(backup.Location{Path: dest})
	if len(items) != 2 {
		t.Errorf("retention kept %d archives", len(items))
	}
	h := s.History("p1", 0)
	if len(h) != 5 || h[0].Result != "ok" || h[4].Result == "ok" {
		t.Errorf("history %+v", h)
	}
	if st := s.State("p1"); st.Attempt != 0 || st.LastResult != "ok" {
		t.Errorf("state %+v", st)
	}
}

func TestLockExcludes(t *testing.T) {
	s := &Service{Store: &store.Store{Dir: t.TempDir()}}
	unlock, err := s.lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.lock(); !errors.Is(err, ErrBusy) {
		t.Errorf("second lock: %v", err)
	}
	unlock()
	if u, err := s.lock(); err != nil {
		t.Error(err)
	} else {
		u()
	}
}
