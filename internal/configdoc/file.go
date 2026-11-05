package configdoc

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/store"
)

// Snapshot is a file's content when it was read, for conflict detection.
type Snapshot struct {
	Path   string
	Text   string
	Exists bool
	sum    [32]byte
}

// ReadFile reads a Windows-side config file (a missing file is an empty,
// non-existent snapshot: every setting has its default).
func ReadFile(path string) (Snapshot, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Snapshot{Path: path, sum: sha256.Sum256(nil)}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Path: path, Text: string(b), Exists: true, sum: sha256.Sum256(b)}, nil
}

// FromText makes a snapshot of text read some other way (wsl.conf, read
// through wsl.exe).
func FromText(path, text string, exists bool) Snapshot {
	return Snapshot{Path: path, Text: text, Exists: exists, sum: sha256.Sum256([]byte(text))}
}

// Changed reports whether current differs from the snapshot.
func (s Snapshot) Changed(current string) bool {
	return sha256.Sum256([]byte(current)) != s.sum
}

// ErrConflict means the file changed on disk after it was read.
var ErrConflict = errors.New("configdoc: the file changed since it was opened")

// Save writes text over the file the snapshot came from.
// Returns: ErrConflict, writing nothing, when the file on disk no longer
// matches the snapshot.
// Side effects: copies the previous file to "<path>.wslpilot-<time>.bak"
// first, then replaces it atomically.
func (s Snapshot) Save(text string) (backup string, err error) {
	cur, err := ReadFile(s.Path)
	if err != nil {
		return "", err
	}
	if cur.sum != s.sum || cur.Exists != s.Exists {
		return "", ErrConflict
	}
	if cur.Exists {
		backup = BackupName(s.Path, time.Now())
		if err := store.WriteFileAtomic(backup, []byte(cur.Text)); err != nil {
			return "", err
		}
	}
	return backup, store.WriteFileAtomic(s.Path, []byte(text))
}

// BackupName is the backup file name used for path at t.
func BackupName(path string, t time.Time) string {
	return path + ".wslpilot-" + t.Format("20060102-150405") + ".bak"
}
