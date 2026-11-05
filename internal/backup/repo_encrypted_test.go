package backup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/YuanZhe-99/WSLPilot/internal/backup/wpbk"
)

// TestEncryptedRepoHook covers RepoWriter.Password and the RepoPassword
// callback: with the password the repository behaves like any other; without
// it Scan lists one placeholder item instead of failing.
func TestEncryptedRepoHook(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "repo")
	known := true
	RepoPassword = func(d string) (string, bool) {
		if d == dir && known {
			return "hunter2", true
		}
		return "", false
	}
	t.Cleanup(func() { RepoPassword = nil })

	roundTrip(t, &RepoWriter{Dir: dir, Password: "hunter2"})
	// A second backup through a writer without its own password unlocks
	// the existing repository with RepoPassword.
	roundTrip(t, &RepoWriter{Dir: dir})
	items, errs := Scan(Location{Path: root, ReadOnly: true})
	if len(errs) != 0 || len(items) != 2 || items[0].Note != "" {
		t.Fatalf("with password: %+v %v", items, errs)
	}

	known = false
	items, errs = Scan(Location{Path: root, ReadOnly: true})
	if len(errs) != 0 || len(items) != 1 || items[0].Kind != KindRepo || items[0].Note != NoteEncrypted || items[0].Time.IsZero() {
		t.Fatalf("without password: %+v %v", items, errs)
	}
	if err := Verify(context.Background(), items[0]); !errors.Is(err, wpbk.ErrEncrypted) {
		t.Fatalf("verify without password: %v", err)
	}
	RepoPassword = func(string) (string, bool) { return "wrong", true }
	items, errs = Scan(Location{Path: root, ReadOnly: true})
	if len(errs) != 0 || len(items) != 1 || items[0].Note != NoteEncrypted {
		t.Fatalf("wrong password: %+v %v", items, errs)
	}
	if _, err := Open(context.Background(), Item{Kind: KindRepo, Path: dir, ID: "x"}); !errors.Is(err, wpbk.ErrPassword) {
		t.Fatalf("open with wrong password: %v", err)
	}
}
