package backup

import (
	"context"
	"slices"
	"sync"

	"github.com/YuanZhe-99/WSLPilot/internal/backup/wpbk"
)

// deltaCache remembers wpbk Delta results by "id base". Snapshot ids name
// immutable content, so an entry never goes stale.
var deltaCache sync.Map

// repoItems converts a repository's snapshots into catalog Items, each
// measured against the version before it of the same source.
// Returns: one Item per snapshot. Full is set on the oldest version of each
// source: no earlier version is left that it builds on. Size is what the
// version stores beyond the one before it (all of its data when Full).
// Notes: when a version's recorded Parent is still that previous version,
// the size recorded at backup time is used. When versions were deleted in
// between, the size is recomputed with wpbk Delta (cached), so deleting an
// older version shows its data moving into the next one. A failed
// recomputation keeps the recorded size.
func repoItems(ctx context.Context, repo *wpbk.Repo, dir string, snaps []wpbk.Snapshot) []Item {
	order := slices.Clone(snaps)
	slices.SortStableFunc(order, func(a, b wpbk.Snapshot) int { return a.Time.Compare(b.Time) })
	prev := map[wpbk.Source]string{}
	base := map[string]string{}
	for _, s := range order {
		base[s.ID] = prev[s.Source]
		prev[s.Source] = s.ID
	}
	items := make([]Item, 0, len(snaps))
	for _, s := range snaps {
		it := snapshotItem(dir, s)
		b := base[s.ID]
		it.Full = b == ""
		if s.Parent != b {
			key := s.ID + " " + b
			if v, ok := deltaCache.Load(key); ok {
				it.Size = v.(int64)
			} else if n, err := repo.Delta(ctx, s.ID, b); err == nil {
				deltaCache.Store(key, n)
				it.Size = n
			}
		}
		items = append(items, it)
	}
	return items
}
