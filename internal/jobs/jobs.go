// Package jobs runs long operations (export, import, compaction, backups,
// Compose up) off the UI thread with progress, cancellation, a log and a
// final state, and appends destructive actions to an audit log.
package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// State is a job's lifecycle state.
type State int

// Job states.
const (
	Running State = iota
	Succeeded
	Failed
	Cancelled
)

// Done reports whether the job has finished.
func (s State) Done() bool { return s != Running }

// Snapshot is a read-only copy of a job for views.
type Snapshot struct {
	ID       int
	Title    string
	State    State
	Progress float64 // 0..1; < 0 when unknown
	Step     string  // current step, for the person using the app
	Log      string
	Err      error
	Started  time.Time
	Ended    time.Time
}

// Job is the handle a running function reports through.
type Job struct {
	m      *Manager
	id     int
	title  string
	cancel context.CancelFunc

	mu       sync.Mutex
	state    State
	progress float64
	step     string
	log      bytes.Buffer
	err      error
	started  time.Time
	ended    time.Time
}

// maxLog bounds the kept log per job.
const maxLog = 256 << 10

// Write appends to the job's log (Job is an io.Writer for command output).
func (j *Job) Write(p []byte) (int, error) {
	j.mu.Lock()
	j.log.Write(p)
	if j.log.Len() > maxLog {
		b := j.log.Bytes()
		keep := append([]byte(nil), b[len(b)-maxLog/2:]...)
		j.log.Reset()
		j.log.WriteString("…\n")
		j.log.Write(keep)
	}
	j.mu.Unlock()
	j.m.changed()
	return len(p), nil
}

// Logf appends a line to the log.
func (j *Job) Logf(line string) { j.Write([]byte(line + "\n")) }

// SetProgress sets the fraction done (negative: unknown) and step text.
func (j *Job) SetProgress(frac float64, step string) {
	j.mu.Lock()
	j.progress, j.step = frac, step
	j.mu.Unlock()
	j.m.changed()
}

// Cancel asks the job to stop; its context is cancelled.
func (j *Job) Cancel() { j.cancel() }

// Snapshot copies the job's state.
func (j *Job) Snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Snapshot{ID: j.id, Title: j.title, State: j.state, Progress: j.progress, Step: j.step,
		Log: j.log.String(), Err: j.err, Started: j.started, Ended: j.ended}
}

// Func is the work of a job. It returns when done; ctx is cancelled by
// Cancel.
type Func func(ctx context.Context, j *Job) error

// Manager owns the jobs of one process.
type Manager struct {
	// OnChange is called (from any goroutine) whenever a job changes; the
	// GUI passes its Window.Update here.
	OnChange func()
	// OnFinish is called once per job when it ends (notifications).
	OnFinish func(Snapshot)
	// AuditPath, when set, receives one JSON line per Audit call.
	AuditPath string

	mu     sync.Mutex
	next   int
	jobs   []*Job
	auditM sync.Mutex
}

// Start runs fn on a goroutine as a new job and returns its handle.
func (m *Manager) Start(title string, fn Func) *Job {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.next++
	j := &Job{m: m, id: m.next, title: title, cancel: cancel, progress: -1, started: time.Now()}
	m.jobs = append(m.jobs, j)
	m.mu.Unlock()
	m.changed()
	go func() {
		defer cancel()
		err := run(ctx, j, fn)
		j.mu.Lock()
		j.ended = time.Now()
		switch {
		case err == nil:
			j.state, j.progress = Succeeded, 1
		case errors.Is(err, context.Canceled) || ctx.Err() != nil:
			j.state = Cancelled
		default:
			j.state, j.err = Failed, err
		}
		j.mu.Unlock()
		m.changed()
		if m.OnFinish != nil {
			m.OnFinish(j.Snapshot())
		}
	}()
	return j
}

// run calls fn, turning a panic into a job failure.
func run(ctx context.Context, j *Job, fn Func) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("internal error: " + toString(r))
		}
	}()
	return fn(ctx, j)
}

func toString(v any) string {
	if e, ok := v.(error); ok {
		return e.Error()
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Wait blocks until the job ends and returns its final snapshot (CLI,
// agent and tests).
func (j *Job) Wait() Snapshot {
	for {
		s := j.Snapshot()
		if s.State.Done() {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// List returns snapshots of all jobs, newest first.
func (m *Manager) List() []Snapshot {
	m.mu.Lock()
	js := append([]*Job(nil), m.jobs...)
	m.mu.Unlock()
	res := make([]Snapshot, 0, len(js))
	for i := len(js) - 1; i >= 0; i-- {
		res = append(res, js[i].Snapshot())
	}
	return res
}

// Get returns the job with id, or nil.
func (m *Manager) Get(id int) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.id == id {
			return j
		}
	}
	return nil
}

// Active is the number of running jobs.
func (m *Manager) Active() int {
	n := 0
	for _, s := range m.List() {
		if !s.State.Done() {
			n++
		}
	}
	return n
}

// ClearFinished drops finished jobs from the list.
func (m *Manager) ClearFinished() {
	m.mu.Lock()
	kept := m.jobs[:0]
	for _, j := range m.jobs {
		if !j.Snapshot().State.Done() {
			kept = append(kept, j)
		}
	}
	m.jobs = kept
	m.mu.Unlock()
	m.changed()
}

func (m *Manager) changed() {
	if m.OnChange != nil {
		m.OnChange()
	}
}

// AuditEntry is one line of the audit log.
type AuditEntry struct {
	Time   time.Time `json:"time"`
	Action string    `json:"action"` // e.g. "wsl.unregister"
	Target string    `json:"target"`
	Detail string    `json:"detail,omitempty"`
	Result string    `json:"result"` // "ok" or the error text
}

// Audit appends a destructive action and its result to AuditPath.
// Side effects: creates the log file's directory. Errors writing the log
// are ignored: auditing never blocks the user's action.
func (m *Manager) Audit(action, target, detail string, err error) {
	if m.AuditPath == "" {
		return
	}
	e := AuditEntry{Time: time.Now().UTC(), Action: action, Target: target, Detail: detail, Result: "ok"}
	if err != nil {
		e.Result = err.Error()
	}
	b, _ := json.Marshal(e)
	m.auditM.Lock()
	defer m.auditM.Unlock()
	os.MkdirAll(filepath.Dir(m.AuditPath), 0o755)
	f, ferr := os.OpenFile(m.AuditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if ferr != nil {
		return
	}
	f.Write(append(b, '\n'))
	f.Close()
}

// ProgressWriter counts bytes written through it and reports the fraction
// of total to the job (total <= 0: bytes only, unknown fraction).
type ProgressWriter struct {
	Job *Job
	// Writer, when set, receives the bytes (the counter is a pass-through).
	Writer io.Writer
	Total  int64
	Step   string
	n      int64
	last   time.Time
}

// Write counts p and updates the job at most ten times a second.
func (p *ProgressWriter) Write(b []byte) (int, error) {
	if p.Writer != nil {
		n, err := p.Writer.Write(b)
		p.count(n)
		return n, err
	}
	p.count(len(b))
	return len(b), nil
}

// count adds n bytes and reports progress at most ten times a second.
func (p *ProgressWriter) count(n int) {
	p.n += int64(n)
	if time.Since(p.last) > 100*time.Millisecond {
		p.last = time.Now()
		frac := -1.0
		if p.Total > 0 {
			frac = min(float64(p.n)/float64(p.Total), 1)
		}
		p.Job.SetProgress(frac, p.Step+" "+FormatBytes(p.n))
	}
}

// N is the number of bytes seen.
func (p *ProgressWriter) N() int64 { return p.n }

// FormatBytes renders a size with a binary unit, e.g. "1.5 GiB".
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + string("KMGTPE"[exp]) + "iB"
}

// ProgressReader reports the bytes read through it like ProgressWriter.
type ProgressReader struct {
	Job    *Job
	Reader io.Reader
	Total  int64
	Step   string
	w      ProgressWriter
}

// Read reads from Reader and counts.
func (p *ProgressReader) Read(b []byte) (int, error) {
	n, err := p.Reader.Read(b)
	if p.w.Job == nil {
		p.w = ProgressWriter{Job: p.Job, Total: p.Total, Step: p.Step}
	}
	p.w.count(n)
	return n, err
}
