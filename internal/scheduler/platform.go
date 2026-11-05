package scheduler

import (
	"context"
	"os"

	"github.com/YuanZhe-99/WSLPilot/internal/backup/wpbk"
	"github.com/YuanZhe-99/WSLPilot/internal/secret"
)

// applyRepoRetention forgets snapshots of this plan by policy, then prunes.
func applyRepoRetention(ctx context.Context, p Plan) ([]string, error) {
	pw, err := secret.Unprotect(p.Destination.ProtectedPassword)
	if err != nil {
		return nil, err
	}
	repo, err := wpbk.Open(p.Destination.Path, wpbk.OpenOptions{Password: pw})
	if err != nil {
		return nil, err
	}
	defer repo.Close()
	snaps, err := repo.Snapshots()
	if err != nil {
		return nil, err
	}
	var mine []wpbk.Snapshot
	for _, s := range snaps {
		for _, t := range s.Tags {
			if t == "plan:"+p.ID {
				mine = append(mine, s)
			}
		}
	}
	_, forget := wpbk.PlanPolicy(mine, p.Retention)
	if len(forget) == 0 {
		return nil, nil
	}
	ids := make([]string, len(forget))
	for i, s := range forget {
		ids[i] = s.ID
	}
	gone, err := repo.Forget(ids...)
	if err != nil {
		return gone, err
	}
	_, err = repo.Prune(ctx)
	return gone, err
}

// processAlive reports whether a process with pid exists.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return alive(proc)
}
