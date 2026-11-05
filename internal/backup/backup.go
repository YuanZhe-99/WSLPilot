// Package backup moves tar streams between sources (distributions, WSLc
// volumes, Compose project files) and three kinds of backup: a folder that
// stays browsable from Windows, a single archive with a sidecar, and a
// wpbk incremental repository. Every backup kind is read back through the
// same Catalog, also when it was made on another PC or by another tool.
// Formats are specified in doc/en-us/backup-format.md.
package backup

import (
	"context"
	"io"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/backup/wpbk"
)

// Kind is the type of a backup location or item.
type Kind string

// Backup kinds.
const (
	KindFolder  Kind = "folder"
	KindArchive Kind = "archive"
	KindRepo    Kind = "repo"
	// KindPlainTar is a tar not made by WSLPilot (no sidecar), e.g. from
	// `wsl --export` or `tar` itself.
	KindPlainTar Kind = "tar"
	// KindVHDX is a `wsl --export --format vhd` disk.
	KindVHDX Kind = "vhdx"
)

// Source kinds, shared with wpbk snapshots.
const (
	SourceDistro  = "wsl-distro"
	SourceVolume  = "wslc-volume"
	SourceCompose = "compose-project"
	SourceUnknown = ""
)

// Source names what a backup holds.
type Source = wpbk.Source

// Item is one restorable version found in a location.
type Item struct {
	Kind   Kind
	Path   string // archive or folder path; repository dir for KindRepo
	ID     string // snapshot ID within a repository; "" otherwise
	Source Source
	Time   time.Time
	Size   int64 // bytes on disk (0 when unknown)
	// Consistent is false when the backup was taken from a running source
	// by the user's explicit choice; such items are labeled in the UI.
	Consistent bool
	// Plan names the backup plan that made it; "" for a manual backup or
	// an outside file.
	Plan string
	// Note explains what cannot be known about an outside backup.
	Note string
	// Tool is the program version that wrote it ("" when unknown).
	Tool string
}

// Meta is what a backup records about its content.
type Meta struct {
	Source     Source
	Time       time.Time
	Consistent bool
	Plan       string
	Tool       string
}

// Writer is a destination that consumes one tar stream per backup.
type Writer interface {
	// Write stores the tar stream read from r and returns the new item.
	Write(ctx context.Context, r io.Reader, m Meta) (Item, error)
}

// Open returns a plain tar stream of an item for restoring.
// Notes: the caller closes the reader. Repository items are restored
// through wpbk; folders are reassembled from their manifest.
func Open(ctx context.Context, it Item) (io.ReadCloser, error) {
	switch it.Kind {
	case KindArchive, KindPlainTar:
		return openArchive(it.Path)
	case KindFolder:
		return openFolder(ctx, it.Path)
	case KindRepo:
		return openSnapshot(ctx, it.Path, it.ID)
	}
	return nil, errUnsupported(it.Kind)
}

// Verify checks an item's integrity without restoring it.
// Returns: nil when every check that the item supports passed.
func Verify(ctx context.Context, it Item) error {
	switch it.Kind {
	case KindArchive:
		return verifyArchive(it.Path)
	case KindFolder:
		return verifyFolder(ctx, it.Path)
	case KindRepo:
		r, err := wpbk.Open(it.Path, wpbk.OpenOptions{ReadOnly: true})
		if err != nil {
			return err
		}
		defer r.Close()
		rep, err := r.Check(ctx, true)
		if err != nil {
			return err
		}
		if !rep.Healthy() {
			return &RepoProblems{Report: rep}
		}
		return nil
	case KindPlainTar:
		rc, err := openArchive(it.Path)
		if err != nil {
			return err
		}
		defer rc.Close()
		_, err = countTar(rc)
		return err
	}
	return nil
}

// RepoProblems wraps a repository check that found damage.
type RepoProblems struct{ Report wpbk.CheckReport }

// Error summarizes the problems.
func (p *RepoProblems) Error() string {
	return "repository check found " + itoa(len(p.Report.Problems)) + " problem(s)"
}
