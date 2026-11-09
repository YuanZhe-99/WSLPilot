package backup

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/YuanZhe-99/WSLPilot/internal/backup/wpbk"
)

// RepoPassword, when set, supplies the password of the encrypted wpbk
// repository in dir; ok is false when none is known. Scan, Open (restore)
// and Verify use it to unlock encrypted repositories; nil means no
// passwords are available. The callback must not block on the UI.
var RepoPassword func(dir string) (password string, ok bool)

// NoteEncrypted is the Note of the single Item Scan reports for an
// encrypted repository whose password is not available (or is wrong): its
// snapshots cannot be listed without it.
const NoteEncrypted = "encrypted"

// RepoWriter stores each backup as a snapshot in a wpbk repository,
// creating the repository on first use.
type RepoWriter struct {
	Dir      string
	Progress func(wpbk.Progress)
	// Password encrypts a repository this writer creates and unlocks an
	// existing encrypted one. Empty creates an unencrypted repository; for
	// an existing repository RepoPassword is consulted when it is empty.
	Password string
}

// Write runs a wpbk backup of the tar stream.
func (w *RepoWriter) Write(ctx context.Context, r io.Reader, m Meta) (Item, error) {
	pw := w.Password
	if pw == "" {
		pw, _ = repoPassword(w.Dir)
	}
	repo, err := wpbk.Open(w.Dir, wpbk.OpenOptions{Password: pw})
	if err != nil {
		if _, serr := os.Stat(w.Dir); serr == nil && !isEmptyDir(w.Dir) {
			return Item{}, err
		}
		if repo, err = wpbk.Init(w.Dir, wpbk.InitOptions{Password: w.Password}); err != nil {
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

// snapshotItem converts a repository snapshot into a catalog Item.
func snapshotItem(dir string, s wpbk.Snapshot) Item {
	it := Item{Kind: KindRepo, Path: dir, ID: s.ID, Source: s.Source, Time: s.Time, Size: s.Totals.NewBytes,
		Full: s.Parent == "", Consistent: true, Tool: s.Tool}
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

// encryptedRepoItem is the placeholder Item for an encrypted repository
// whose snapshots cannot be listed (no or wrong password).
func encryptedRepoItem(dir string) Item {
	it := Item{Kind: KindRepo, Path: dir, Consistent: true, Note: NoteEncrypted}
	if cfg, err := wpbk.ReadConfig(dir); err == nil {
		it.Time = cfg.Created
	}
	return it
}

// repoPassword asks RepoPassword for dir's password.
func repoPassword(dir string) (string, bool) {
	if RepoPassword == nil {
		return "", false
	}
	return RepoPassword(dir)
}

// openRepoReadOnly opens the repository in dir read-only, unlocking it with
// RepoPassword when it is encrypted.
// Returns: wpbk.ErrEncrypted or wpbk.ErrPassword (wrapped by wpbk) when an
// encrypted repository cannot be unlocked.
func openRepoReadOnly(dir string) (*wpbk.Repo, error) {
	pw, _ := repoPassword(dir)
	return wpbk.Open(dir, wpbk.OpenOptions{ReadOnly: true, Password: pw})
}

// isLockedOut reports whether err means an encrypted repository could not
// be unlocked.
func isLockedOut(err error) bool {
	return errors.Is(err, wpbk.ErrEncrypted) || errors.Is(err, wpbk.ErrPassword)
}

// openSnapshot restores a snapshot as a tar stream, read-only.
func openSnapshot(ctx context.Context, dir, id string) (io.ReadCloser, error) {
	repo, err := openRepoReadOnly(dir)
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

// isEmptyDir reports whether dir exists and has no entries.
func isEmptyDir(dir string) bool {
	es, err := os.ReadDir(dir)
	return err == nil && len(es) == 0
}
