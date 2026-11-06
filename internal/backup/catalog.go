package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/YuanZhe-99/WSLPilot/internal/backup/wpbk"
)

// Location is a place backups are found: a folder, an archive file or a
// repository, on this PC or not.
type Location struct {
	Path string `json:"path"`
	// ReadOnly locations were opened from outside (another PC, a NAS):
	// WSLPilot never locks, prunes or deletes in them.
	ReadOnly bool   `json:"readOnly"`
	Label    string `json:"label,omitempty"`
}

// Scan lists every restorable item at a location.
// Notes: a folder is searched two levels deep for archives, folder
// backups and repositories. Problems with single entries are returned with
// the items found so far, never stopping the scan.
func Scan(loc Location) ([]Item, []error) {
	st, err := os.Stat(loc.Path)
	if err != nil {
		return nil, []error{err}
	}
	if !st.IsDir() {
		it, ok, err := classifyFile(loc.Path, st)
		if err != nil {
			return nil, []error{err}
		}
		if !ok {
			return nil, []error{errors.New(filepath.Base(loc.Path) + ": not a backup WSLPilot can read")}
		}
		return []Item{it}, nil
	}
	var items []Item
	var errs []error
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if its, ok, err := classifyDir(dir); ok || err != nil {
			items = append(items, its...)
			if err != nil {
				errs = append(errs, err)
			}
			return
		}
		if depth == 0 {
			return
		}
		es, err := os.ReadDir(dir)
		if err != nil {
			errs = append(errs, err)
			return
		}
		for _, e := range es {
			p := filepath.Join(dir, e.Name())
			if e.IsDir() {
				walk(p, depth-1)
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if it, ok, err := classifyFile(p, info); err != nil {
				errs = append(errs, err)
			} else if ok {
				items = append(items, it)
			}
		}
	}
	walk(loc.Path, 2)
	sort.Slice(items, func(i, j int) bool { return items[i].Time.After(items[j].Time) })
	return items, errs
}

// classifyDir recognizes a folder backup or a wpbk repository.
func classifyDir(dir string) ([]Item, bool, error) {
	if _, err := os.Stat(filepath.Join(dir, ".wslpilot", "folder.json")); err == nil {
		info, err := readFolderInfo(dir)
		if err != nil {
			return nil, true, err
		}
		return []Item{folderItem(dir, info)}, true, nil
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		if _, err := os.Stat(filepath.Join(dir, "snapshots")); err != nil {
			return nil, false, nil
		}
		repo, err := openRepoReadOnly(dir)
		if isLockedOut(err) {
			return []Item{encryptedRepoItem(dir)}, true, nil
		}
		if err != nil {
			return nil, true, err
		}
		defer repo.Close()
		snaps, err := repo.Snapshots()
		items := make([]Item, 0, len(snaps))
		for _, s := range snaps {
			items = append(items, snapshotItem(dir, s))
		}
		return items, true, err
	}
	return nil, false, nil
}

// archiveExts are the archive names Scan picks up.
var archiveExts = []string{".tar", ".tar.gz", ".tgz", ".tar.zst", ".tar.xz"}

// classifyFile recognizes an archive (with or without sidecar) or a VHDX.
func classifyFile(p string, st os.FileInfo) (Item, bool, error) {
	lower := strings.ToLower(p)
	if strings.HasSuffix(lower, ".vhdx") || strings.HasSuffix(lower, ".vhd") {
		return Item{Kind: KindVHDX, Path: p, Time: st.ModTime(), Size: st.Size(), Source: Source{Kind: SourceDistro},
			Note: "disk image: restores only as a new distribution"}, true, nil
	}
	isArchive := false
	for _, e := range archiveExts {
		if strings.HasSuffix(lower, e) {
			isArchive = true
		}
	}
	if !isArchive {
		return Item{}, false, nil
	}
	if sc, err := readSidecar(p); err == nil {
		return archiveItem(p, sc), true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Item{}, false, err
	}
	return Item{Kind: KindPlainTar, Path: p, Time: st.ModTime(), Size: st.Size(), Consistent: true,
		Note: "no WSLPilot metadata: what it came from and whether it was consistent are unknown"}, true, nil
}

// Group is all versions of one source.
type Group struct {
	Source Source
	Items  []Item // newest first
}

// GroupBySource gathers items of every location by source; outside tars
// without metadata form one group per file.
func GroupBySource(items []Item) []Group {
	idx := map[string]int{}
	var gs []Group
	for _, it := range items {
		key := it.Source.Kind + "\x00" + it.Source.Name
		if it.Source.Name == "" {
			key = "\x01" + it.Path
		}
		i, ok := idx[key]
		if !ok {
			i = len(gs)
			idx[key] = i
			gs = append(gs, Group{Source: it.Source})
		}
		gs[i].Items = append(gs[i].Items, it)
	}
	for i := range gs {
		sort.Slice(gs[i].Items, func(a, b int) bool { return gs[i].Items[a].Time.After(gs[i].Items[b].Time) })
	}
	sort.Slice(gs, func(a, b int) bool {
		if gs[a].Source.Kind != gs[b].Source.Kind {
			return gs[a].Source.Kind < gs[b].Source.Kind
		}
		return gs[a].Source.Name < gs[b].Source.Name
	})
	return gs
}

// Policy is a retention policy (the same rules as wpbk's).
type Policy = wpbk.Policy

// PlanRetention splits archive and folder items of one location into kept
// and expired ones by policy, grouped by source.
// Notes: repository items are left out; their retention runs inside the
// repository (wpbk ApplyPolicy) followed by a prune.
func PlanRetention(items []Item, p Policy) (keep, expire []Item) {
	var snaps []wpbk.Snapshot
	byID := map[string]Item{}
	for _, it := range items {
		if it.Kind != KindArchive && it.Kind != KindFolder {
			continue
		}
		snaps = append(snaps, wpbk.Snapshot{ID: it.Path, Time: it.Time, Source: it.Source})
		byID[it.Path] = it
	}
	k, f := wpbk.PlanPolicy(snaps, p)
	for _, s := range k {
		keep = append(keep, byID[s.ID])
	}
	for _, s := range f {
		expire = append(expire, byID[s.ID])
	}
	return keep, expire
}

// Delete removes an archive (with its sidecar) or a folder backup.
// Notes: refuses repository items and anything in a read-only location.
func Delete(loc Location, it Item) error {
	if loc.ReadOnly {
		return errors.New("backup: the location is read-only")
	}
	switch it.Kind {
	case KindArchive:
		os.Remove(it.Path + SidecarSuffix)
		return os.Remove(it.Path)
	case KindFolder:
		if _, err := os.Stat(filepath.Join(it.Path, ".wslpilot", "folder.json")); err != nil {
			return errors.New("backup: not a folder backup, not deleted")
		}
		return os.RemoveAll(it.Path)
	}
	return errUnsupported(it.Kind)
}

// DeleteVersion removes one backup version the person chose to delete.
// An archive or a folder backup is removed as Delete does. A repository
// snapshot is forgotten and the repository pruned: data that later
// versions share stays with them (in effect merged into the next one),
// and only what this version alone held is freed. When it was the last
// snapshot, the whole repository is removed.
// Notes: refuses read-only locations and locked (encrypted, not unlocked)
// repositories.
func DeleteVersion(ctx context.Context, loc Location, it Item) error {
	if it.Kind != KindRepo {
		return Delete(loc, it)
	}
	if loc.ReadOnly {
		return errors.New("backup: the location is read-only")
	}
	if it.ID == "" {
		return errors.New("backup: unlock the repository first")
	}
	pw, _ := repoPassword(it.Path)
	r, err := wpbk.Open(it.Path, wpbk.OpenOptions{Password: pw})
	if err != nil {
		return err
	}
	if _, err := r.Forget(it.ID); err != nil {
		r.Close()
		return err
	}
	left, err := r.Snapshots()
	if err != nil {
		r.Close()
		return err
	}
	if len(left) == 0 {
		r.Close()
		return removeRepo(it.Path)
	}
	_, err = r.Prune(ctx)
	if cerr := r.Close(); err == nil {
		err = cerr
	}
	return err
}

// removeRepo deletes a repository's own entries (config.json and its
// four folders), then the folder itself when nothing else is in it.
func removeRepo(dir string) error {
	if _, err := wpbk.ReadConfig(dir); err != nil {
		return errors.New("backup: not a repository, not deleted")
	}
	for _, d := range []string{"packs", "index", "snapshots", "locks"} {
		if err := os.RemoveAll(filepath.Join(dir, d)); err != nil {
			return err
		}
	}
	if err := os.Remove(filepath.Join(dir, "config.json")); err != nil {
		return err
	}
	os.Remove(dir) // kept when other files are there
	return nil
}
