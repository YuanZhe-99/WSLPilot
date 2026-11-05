package wpbk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Lock timing (see backup-format.md, "Locks").
const (
	// lockStaleAfter is how long an unrefreshed lock stays valid.
	lockStaleAfter = 30 * time.Minute
	// lockRefreshEvery is how often a held lock is refreshed.
	lockRefreshEvery = 5 * time.Minute
)

// LockInfo is the content of one locks/<id>.json file.
type LockInfo struct {
	Version   int       `json:"version"`
	Exclusive bool      `json:"exclusive"`
	Host      string    `json:"host"`
	PID       int       `json:"pid"`
	Created   time.Time `json:"created"`
	Refreshed time.Time `json:"refreshed"`
}

// LockError reports the lock that blocked an operation. It matches
// ErrLocked with errors.Is.
type LockError struct {
	Holder LockInfo
}

// Error describes the holder for logs.
func (e *LockError) Error() string {
	kind := "shared"
	if e.Holder.Exclusive {
		kind = "exclusive"
	}
	return fmt.Sprintf("wpbk: repository has a %s lock held by pid %d on %s since %s", kind, e.Holder.PID, e.Holder.Host, e.Holder.Created.Format(time.RFC3339))
}

// Is makes errors.Is(err, ErrLocked) true.
func (e *LockError) Is(target error) bool { return target == ErrLocked }

// repoLock is a held lock; release removes it.
type repoLock struct {
	path string
	info LockInfo
	stop chan struct{}
	wg   sync.WaitGroup
}

// lock takes a shared or exclusive repository lock.
// Returns: a held lock, nil for shared locks on read-only repositories, a
// *LockError on conflict, ErrReadOnly for exclusive locks on read-only ones.
// Side effects: creates locks/<random>.json, removes stale locks of others,
// and refreshes its own lock in the background until release.
func (r *Repo) lock(exclusive bool) (*repoLock, error) {
	if r.readOnly {
		if exclusive {
			return nil, ErrReadOnly
		}
		return nil, nil
	}
	host, _ := os.Hostname()
	now := r.now().UTC()
	l := &repoLock{
		path: filepath.Join(r.dir, "locks", randomID()+".json"),
		info: LockInfo{Version: FormatVersion, Exclusive: exclusive, Host: host, PID: os.Getpid(), Created: now, Refreshed: now},
		stop: make(chan struct{}),
	}
	if err := l.write(); err != nil {
		return nil, err
	}
	if err := r.checkLocks(l); err != nil {
		os.Remove(l.path)
		return nil, err
	}
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		t := time.NewTicker(lockRefreshEvery)
		defer t.Stop()
		for {
			select {
			case <-l.stop:
				return
			case <-t.C:
				l.info.Refreshed = time.Now().UTC()
				l.write()
			}
		}
	}()
	return l, nil
}

// write publishes the lock file.
func (l *repoLock) write() error {
	data, err := json.Marshal(l.info)
	if err != nil {
		return err
	}
	return writeFileAtomic(l.path, data)
}

// release stops refreshing and removes the lock file. A nil lock is a no-op.
func (l *repoLock) release() {
	if l == nil {
		return
	}
	close(l.stop)
	l.wg.Wait()
	os.Remove(l.path)
}

// checkLocks looks for locks that conflict with own and removes stale ones.
// Notes: two writers that lock at the same moment may both see a conflict
// and both back off; that is safe, and the caller may retry.
func (r *Repo) checkLocks(own *repoLock) error {
	dir := filepath.Join(r.dir, "locks")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		p := filepath.Join(dir, e.Name())
		if p == own.path || e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var other LockInfo
		if json.Unmarshal(data, &other) != nil {
			if info, err := e.Info(); err == nil && r.now().Sub(info.ModTime()) > lockStaleAfter {
				os.Remove(p)
				continue
			}
			other = LockInfo{Exclusive: true}
		} else if r.lockStale(other, own.info) {
			os.Remove(p)
			continue
		}
		if own.info.Exclusive || other.Exclusive {
			return &LockError{Holder: other}
		}
	}
	return nil
}

// lockStale reports whether other is abandoned: not refreshed within
// lockStaleAfter, or held by a process of this host that no longer runs.
func (r *Repo) lockStale(other, own LockInfo) bool {
	if r.now().Sub(other.Refreshed) > lockStaleAfter {
		return true
	}
	if other.Host == own.Host && other.PID != own.PID && !processAlive(other.PID) {
		return true
	}
	return false
}
