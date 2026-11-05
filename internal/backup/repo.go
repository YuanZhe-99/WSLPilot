package backup

import (
	"context"
	"io"
	"os"

	"github.com/YuanZhe-99/WSLPilot/internal/backup/wpbk"
)

// RepoWriter stores each backup as a snapshot in a wpbk repository,
// creating the repository on first use.
type RepoWriter struct {
	Dir      string
	Progress func(wpbk.Progress)
}

// Write runs a wpbk backup of the tar stream.
func (w *RepoWriter) Write(ctx context.Context, r io.Reader, m Meta) (Item, error) {
	repo, err := wpbk.Open(w.Dir, wpbk.OpenOptions{})
	if err != nil {
		if _, serr := os.Stat(w.Dir); serr == nil && !isEmptyDir(w.Dir) {
			return Item{}, err
		}
		if repo, err = wpbk.Init(w.Dir, wpbk.InitOptions{}); err != nil {
			return Item{}, err
		}
	}
	defer repo.Close()
	tags := []string{}
	if !m.Consistent {
		tags = append(tags, "inconsistent")
	}
	if m.Plan != "" {
		tags = append(tags, "plan:"+m.Plan)
	}
	snap, err := repo.Backup(ctx, r, wpbk.SnapshotMeta{Source: m.Source, Tags: tags, Time: m.Time}, w.Progress)
	if err != nil {
		return Item{}, err
	}
	return snapshotItem(w.Dir, snap), nil
}

func snapshotItem(dir string, s wpbk.Snapshot) Item {
	it := Item{Kind: KindRepo, Path: dir, ID: s.ID, Source: s.Source, Time: s.Time, Size: s.Totals.NewBytes,
		Consistent: true, Tool: s.Tool}
	for _, t := range s.Tags {
		switch {
		case t == "inconsistent":
			it.Consistent = false
		case len(t) > 5 && t[:5] == "plan:":
			it.Plan = t[5:]
		}
	}
	return it
}

// openSnapshot restores a snapshot as a tar stream, read-only.
func openSnapshot(ctx context.Context, dir, id string) (io.ReadCloser, error) {
	repo, err := wpbk.Open(dir, wpbk.OpenOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	go func() {
		err := repo.Restore(ctx, id, pw, nil, nil)
		repo.Close()
		pw.CloseWithError(err)
	}()
	return pr, nil
}

func isEmptyDir(dir string) bool {
	es, err := os.ReadDir(dir)
	return err == nil && len(es) == 0
}
