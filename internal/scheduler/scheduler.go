package scheduler

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/store"
)

// planSchema is the version of plans/<id>.json.
const planSchema = 1

// Target is one thing a plan backs up.
type Target struct {
	Kind string `json:"kind"` // backup.SourceDistro | SourceVolume | SourceCompose
	Name string `json:"name"`
}

// Destination is where a plan writes.
type Destination struct {
	Kind        backup.Kind        `json:"kind"` // folder | archive | repo
	Path        string             `json:"path"`
	Compression backup.Compression `json:"compression,omitempty"`
}

// Plan is a scheduled backup.
type Plan struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Enabled     bool          `json:"enabled"`
	Targets     []Target      `json:"targets"`
	Destination Destination   `json:"destination"`
	Schedule    Schedule      `json:"schedule"`
	Retention   backup.Policy `json:"retention"`
	Retry       Retry         `json:"retry"`
	// Live backs up running sources without stopping them (labeled
	// inconsistent). The default stops and restarts them.
	Live bool `json:"live"`
	// Notify shows a notification when a run fails (and, when
	// NotifySuccess, also when it succeeds).
	Notify        bool `json:"notify"`
	NotifySuccess bool `json:"notifySuccess"`
}

// State is a plan's run bookkeeping (state/<id>.json).
type State struct {
	LastRun     time.Time `json:"lastRun"`
	LastResult  string    `json:"lastResult"` // "ok" or the error
	NextRun     time.Time `json:"nextRun"`
	Attempt     int       `json:"attempt"` // failed attempts of the current run
	LastSuccess time.Time `json:"lastSuccess"`
}

// Record is one line of history/<id>.jsonl.
type Record struct {
	Start   time.Time     `json:"start"`
	End     time.Time     `json:"end"`
	Result  string        `json:"result"`
	Items   []backup.Item `json:"items,omitempty"`
	Expired []string      `json:"expired,omitempty"`
	Attempt int           `json:"attempt"`
}

// Service stores plans and runs them.
type Service struct {
	Store *store.Store
	// Do runs one target into a writer (backup.Runner methods in the app,
	// a fake in tests).
	Do func(ctx context.Context, t Target, w backup.Writer, o backup.Options) (backup.Item, error)
	// Notify shows a notification; may be nil.
	Notify func(title, body string)
	Now    func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// NewID returns a random plan ID.
func NewID() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Plans lists every stored plan, by name.
func (s *Service) Plans() ([]Plan, error) {
	names, err := s.Store.List("plans")
	if err != nil {
		return nil, err
	}
	var ps []Plan
	for _, n := range names {
		if !strings.HasSuffix(n, ".json") {
			continue
		}
		var p Plan
		if _, err := s.Store.Load("plans/"+n, planSchema, &p); err != nil {
			continue
		}
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].Name < ps[j].Name })
	return ps, nil
}

// Save validates and stores a plan, recomputing its next run.
func (s *Service) Save(p Plan) error {
	if p.ID == "" {
		return errors.New("scheduler: plan without ID")
	}
	if err := p.Schedule.Validate(); err != nil {
		return err
	}
	if len(p.Targets) == 0 {
		return errors.New("scheduler: a plan needs at least one target")
	}
	if p.Destination.Path == "" {
		return errors.New("scheduler: a plan needs a destination")
	}
	if err := s.Store.Save("plans/"+p.ID+".json", planSchema, p); err != nil {
		return err
	}
	st := s.State(p.ID)
	st.NextRun = p.Schedule.Next(s.now())
	st.Attempt = 0
	return s.saveState(p.ID, st)
}

// Delete removes a plan and its state; its backups stay where they are.
func (s *Service) Delete(id string) error {
	s.Store.Delete("state/" + id + ".json")
	return s.Store.Delete("plans/" + id + ".json")
}

// State returns a plan's bookkeeping (zero when it never ran).
func (s *Service) State(id string) State {
	var st State
	s.Store.Load("state/"+id+".json", 1, &st)
	return st
}

func (s *Service) saveState(id string, st State) error {
	return s.Store.Save("state/"+id+".json", 1, st)
}

// History returns a plan's run records, newest first (at most limit).
func (s *Service) History(id string, limit int) []Record {
	f, err := os.Open(s.Store.Path("history/" + id + ".jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var rs []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			rs = append(rs, r)
		}
	}
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	if limit > 0 && len(rs) > limit {
		rs = rs[:limit]
	}
	return rs
}

func (s *Service) appendHistory(id string, r Record) {
	p := s.Store.Path("history/" + id + ".jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(r)
	f.Write(append(b, '\n'))
}

// Due returns the enabled plans whose next run (or retry) has come.
func (s *Service) Due() ([]Plan, error) {
	ps, err := s.Plans()
	if err != nil {
		return nil, err
	}
	now := s.now()
	var due []Plan
	for _, p := range ps {
		if !p.Enabled || p.Schedule.Every == Manual {
			continue
		}
		st := s.State(p.ID)
		if st.NextRun.IsZero() {
			st.NextRun = p.Schedule.Next(now)
			s.saveState(p.ID, st)
			continue
		}
		if !st.NextRun.After(now) {
			due = append(due, p)
		}
	}
	return due, nil
}

// RunDue runs every due plan under the scheduler lock.
// Returns: ErrBusy when another process (GUI or agent) holds the lock.
func (s *Service) RunDue(ctx context.Context) error {
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	due, err := s.Due()
	if err != nil {
		return err
	}
	for _, p := range due {
		s.Run(ctx, p)
	}
	return nil
}

// writer returns the backup writer for a plan's destination.
func writer(d Destination) (backup.Writer, error) {
	switch d.Kind {
	case backup.KindFolder:
		return &backup.FolderWriter{Dir: d.Path}, nil
	case backup.KindArchive:
		c := d.Compression
		if c == "" {
			c = backup.CompressZstd
		}
		return &backup.ArchiveWriter{Dir: d.Path, Compression: c}, nil
	case backup.KindRepo:
		return &backup.RepoWriter{Dir: d.Path}, nil
	}
	return nil, fmt.Errorf("scheduler: unknown destination kind %q", d.Kind)
}

// Run executes a plan now (scheduled or by hand), applies its retention
// and records the result.
// Side effects: writes backups, may delete expired archives/folders (only
// ones this plan made) and forget+prune repository snapshots; updates
// state and history; notifies.
func (s *Service) Run(ctx context.Context, p Plan) Record {
	rec := Record{Start: s.now()}
	st := s.State(p.ID)
	w, err := writer(p.Destination)
	var errs []string
	if err != nil {
		errs = append(errs, err.Error())
	} else {
		for _, t := range p.Targets {
			it, err := s.Do(ctx, t, w, backup.Options{Live: p.Live, Plan: p.ID})
			if err != nil {
				errs = append(errs, t.Name+": "+err.Error())
				continue
			}
			rec.Items = append(rec.Items, it)
		}
		if len(errs) == 0 {
			rec.Expired, err = s.applyRetention(ctx, p)
			if err != nil {
				errs = append(errs, "retention: "+err.Error())
			}
		}
	}
	rec.End = s.now()
	rec.Result = "ok"
	if len(errs) > 0 {
		rec.Result = strings.Join(errs, "; ")
	}
	st.LastRun, st.LastResult = rec.Start, rec.Result
	if rec.Result == "ok" {
		st.Attempt, st.LastSuccess = 0, rec.End
		st.NextRun = p.Schedule.Next(rec.End)
		if p.Notify && p.NotifySuccess && s.Notify != nil {
			s.Notify(p.Name, "Backup finished")
		}
	} else {
		st.Attempt++
		if st.Attempt <= p.Retry.Max {
			st.NextRun = rec.End.Add(p.Retry.Delay(st.Attempt))
		} else {
			st.Attempt = 0
			st.NextRun = p.Schedule.Next(rec.End)
		}
		if p.Notify && s.Notify != nil {
			s.Notify(p.Name, "Backup failed: "+rec.Result)
		}
	}
	rec.Attempt = st.Attempt
	s.saveState(p.ID, st)
	s.appendHistory(p.ID, rec)
	return rec
}

// applyRetention removes what the policy expires among this plan's own
// backups in its destination.
func (s *Service) applyRetention(ctx context.Context, p Plan) ([]string, error) {
	if p.Retention == (backup.Policy{}) {
		return nil, nil
	}
	loc := backup.Location{Path: p.Destination.Path}
	if p.Destination.Kind == backup.KindRepo {
		return applyRepoRetention(ctx, p)
	}
	items, _ := backup.Scan(loc)
	var mine []backup.Item
	for _, it := range items {
		if it.Plan == p.ID {
			mine = append(mine, it)
		}
	}
	_, expire := backup.PlanRetention(mine, p.Retention)
	var gone []string
	for _, it := range expire {
		if err := backup.Delete(loc, it); err != nil {
			return gone, err
		}
		gone = append(gone, it.Path)
	}
	return gone, nil
}

// ErrBusy means another WSLPilot process is running plans.
var ErrBusy = errors.New("scheduler: another WSLPilot process is running backups")

// lock takes the scheduler lock file (locks/scheduler.lock).
// Notes: a lock whose process is gone, or older than a day, is stale.
func (s *Service) lock() (func(), error) {
	p := s.Store.Path("locks/scheduler.lock")
	os.MkdirAll(filepath.Dir(p), 0o755)
	for range 2 {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
			f.Close()
			return func() { os.Remove(p) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		b, _ := os.ReadFile(p)
		lines := strings.Split(string(b), "\n")
		pid, _ := strconv.Atoi(strings.TrimSpace(lines[0]))
		at := time.Time{}
		if len(lines) > 1 {
			at, _ = time.Parse(time.RFC3339, strings.TrimSpace(lines[1]))
		}
		if pid > 0 && processAlive(pid) && time.Since(at) < 24*time.Hour {
			return nil, ErrBusy
		}
		os.Remove(p)
	}
	return nil, ErrBusy
}
