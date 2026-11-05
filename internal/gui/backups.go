package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/scheduler"
	"github.com/YuanZhe-99/WSLPilot/internal/volume"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// backupsPage is the state of the Backups page.
type backupsPage struct {
	view int // 0 restore, 1 plans

	locations []backup.Location
	loaded    bool
	loading   bool
	groups    []backup.Group
	scanErrs  []string
	gl, vl    ui.ListState
	gsel      int
	vsel      int
	verified  map[string]string // item key → "ok" or error

	plansLoaded bool
	plans       []scheduler.Plan
	states      map[string]scheduler.State
	pl          ui.ListState
	psel        int
	history     []scheduler.Record

	dlg  string
	rin  restoreInput
	plan planInput
}

// restoreInput holds the restore dialog fields.
type restoreInput struct {
	mode     string // distro, volume, over-distro, over-volume, files
	name     string
	location string
	dir      string
	prefix   string
}

// planInput holds the plan editor fields.
type planInput struct {
	p                                 scheduler.Plan
	targets                           map[string]*bool // "kind\x00name"
	kind, every, at, weekday, day     string
	keepLast, keepDaily, keepWeekly   string
	keepMonthly, retries, compression string
	options                           []scheduler.Target
}

// locationsDoc is where the restore view remembers locations.
const locationsDoc = "locations.json"

// backupDir is the default backup folder.
func (a *App) backupDir() string {
	if d := a.settings.backupDir; d != "" {
		return d
	}
	return filepath.Join(os.Getenv("USERPROFILE"), "WSLPilot Backups")
}

// ensurePlans loads plans and their states once.
func (a *App) ensurePlans() {
	p := &a.bk
	if p.plansLoaded {
		return
	}
	p.plansLoaded = true
	a.async(func(ctx context.Context) func() {
		ps, _ := a.k.Scheduler.Plans()
		st := map[string]scheduler.State{}
		for _, pl := range ps {
			st[pl.ID] = a.k.Scheduler.State(pl.ID)
		}
		return func() { p.plans, p.states = ps, st }
	})
}

// loadCatalog scans every location.
func (a *App) loadCatalog() {
	p := &a.bk
	if p.loading {
		return
	}
	p.loading = true
	own := a.backupDir()
	a.async(func(ctx context.Context) func() {
		var locs []backup.Location
		a.k.Store.Load(locationsDoc, 1, &locs)
		all := append([]backup.Location{{Path: own, Label: L.BkDefaultLocation()}}, locs...)
		var items []backup.Item
		var errs []string
		for _, l := range all {
			if _, err := os.Stat(l.Path); err != nil {
				continue
			}
			its, es := backup.Scan(l)
			items = append(items, its...)
			for _, e := range es {
				errs = append(errs, e.Error())
			}
		}
		groups := backup.GroupBySource(items)
		return func() {
			p.loading, p.loaded, p.locations, p.groups, p.scanErrs = false, true, all, groups, errs
			if p.gsel >= len(groups) {
				p.gsel = -1
			}
		}
	})
}

// addLocation remembers a location opened from outside (read-only).
func (a *App) addLocation(path string) {
	a.async(func(ctx context.Context) func() {
		var locs []backup.Location
		a.k.Store.Load(locationsDoc, 1, &locs)
		for _, l := range locs {
			if strings.EqualFold(l.Path, path) {
				return func() { a.loadCatalog() }
			}
		}
		locs = append(locs, backup.Location{Path: path, ReadOnly: true})
		err := a.k.Store.Save(locationsDoc, 1, locs)
		return func() {
			if err != nil {
				a.notify(friendlyError(err), true)
			}
			a.bk.loaded = false
			a.loadCatalog()
		}
	})
}

// setLocation saves a changed or removed location.
func (a *App) setLocation(path string, readOnly, remove bool) {
	a.async(func(ctx context.Context) func() {
		var locs []backup.Location
		a.k.Store.Load(locationsDoc, 1, &locs)
		kept := locs[:0]
		for _, l := range locs {
			if strings.EqualFold(l.Path, path) {
				if remove {
					continue
				}
				l.ReadOnly = readOnly
			}
			kept = append(kept, l)
		}
		a.k.Store.Save(locationsDoc, 1, kept)
		return func() { a.bk.loaded = false; a.loadCatalog() }
	})
}

// locationOf returns the location an item belongs to.
func (p *backupsPage) locationOf(it backup.Item) backup.Location {
	best, n := backup.Location{Path: it.Path, ReadOnly: true}, -1
	for _, l := range p.locations {
		if strings.HasPrefix(strings.ToLower(it.Path), strings.ToLower(l.Path)) && len(l.Path) > n {
			best, n = l, len(l.Path)
		}
	}
	return best
}

// backupsView draws the Backups page.
func (a *App) backupsView(c *ui.Context) {
	p := &a.bk
	if !p.loaded && !p.loading {
		p.gl.Selected, p.vl.Selected, p.pl.Selected = &p.gsel, &p.vsel, &p.psel
		p.gsel, p.vsel, p.psel = -1, -1, -1
		a.loadCatalog()
	}
	a.ensurePlans()
	a.header(c, L.NavBackups(), func() {
		ui.Segmented(c, &p.view, L.BkRestore(), L.BkPlans()).Label(L.NavBackups())
	})
	if p.view == 1 {
		a.plansView(c)
	} else {
		a.restoreView(c)
	}
	a.backupDialogs(c)
}

func (a *App) restoreView(c *ui.Context) {
	p := &a.bk
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
		ui.PrimaryButton(c, L.BkOpenFolder()).OnClick(func() {
			go func() {
				if ps := a.host.PickFiles(L.BkOpenFolder(), true); len(ps) > 0 {
					a.update(func() { a.addLocation(ps[0]) })
				}
			}()
		})
		ui.Button(c, L.BkOpenFile()).OnClick(func() {
			go func() {
				if ps := a.host.PickFiles(L.BkOpenFile(), false, "tar", "gz", "tgz", "zst", "xz", "vhdx"); len(ps) > 0 {
					a.update(func() { a.addLocation(ps[0]) })
				}
			}()
		})
		ui.Spacer(c)
		ui.Button(c, L.Refresh()).Disabled(p.loading).OnClick(func() { p.loaded = false; a.loadCatalog() })
	})
	muted(c, L.BkRestoreHelp())
	for i, e := range p.scanErrs {
		ui.Text(c.Key("se"+itoa(i)), e).TextColor(t.Warning)
	}
	ui.Row(c).Grow(1).FillWidth().Gap(gap).Children(func() {
		ui.Column(c).Width(300).FillHeight().Gap(smallGap).Children(func() {
			ui.Text(c, L.BkWhat()).Bold()
			cols := []ui.TableColumn{{Title: L.ColName()}, {Title: L.BkVersions(), Width: 70, Align: ui.End}}
			ui.Table(c, &p.gl, cols, len(p.groups), func(row, col int) {
				g := p.groups[row]
				switch col {
				case 0:
					ui.Text(c, groupTitle(g)).SingleLine()
				case 1:
					ui.Text(c, itoa(len(g.Items)))
				}
			}).Grow(1).FillWidth()
			ui.Text(c, L.BkLocations()).Bold()
			for _, l := range p.locations {
				ui.Row(c.Key(l.Path)).FillWidth().Gap(smallGap).AlignItems(ui.Center).Children(func() {
					label := l.Path
					if l.Label != "" {
						label = l.Label
					}
					ui.Text(c, label).SingleLine().Grow(1).Tooltip(l.Path)
					if l.ReadOnly {
						muted(c, L.BkReadOnly())
					}
					if l.Label == "" {
						path := l.Path
						ui.Button(c, "…").Label(L.More()).Menu(func(m *ui.Menu) {
							if l.ReadOnly && m.Item(L.BkTakeOver()).Chosen() {
								a.setLocation(path, false, false)
							}
							if m.Item(L.BkForget()).Chosen() {
								a.setLocation(path, false, true)
							}
						})
					}
				})
			}
		})
		if p.gsel >= 0 && p.gsel < len(p.groups) {
			g := p.groups[p.gsel]
			ui.Column(c).Grow(1).FillHeight().Gap(smallGap).Children(func() { a.versions(c, g) })
		} else if p.loaded && len(p.groups) == 0 {
			muted(c, L.BkNone())
		} else {
			muted(c, L.BkPick())
		}
	})
}

// groupTitle names a source for the list.
func groupTitle(g backup.Group) string {
	switch g.Source.Kind {
	case backup.SourceDistro:
		if g.Source.Name == "" {
			return L.BkOutsideDisk(filepath.Base(g.Items[0].Path))
		}
		return L.BkDistro(g.Source.Name)
	case backup.SourceVolume:
		return L.BkVolume(g.Source.Name)
	case backup.SourceCompose:
		return L.BkProject(g.Source.Name)
	}
	return L.BkOutsideTar(filepath.Base(g.Items[0].Path))
}

func itemKey(it backup.Item) string { return it.Path + "#" + it.ID }

func kindLabel(k backup.Kind) string {
	switch k {
	case backup.KindFolder:
		return L.BkKindFolder()
	case backup.KindArchive:
		return L.BkKindArchive()
	case backup.KindRepo:
		return L.BkKindRepo()
	case backup.KindVHDX:
		return L.BkKindVHDX()
	}
	return L.BkKindTar()
}

func (a *App) versions(c *ui.Context, g backup.Group) {
	p := &a.bk
	t := c.Theme()
	ui.Text(c, groupTitle(g)).FontSize(sectionSize).Bold()
	cols := []ui.TableColumn{{Title: L.BkWhen(), Width: 150}, {Title: L.BkKind(), Width: 110}, {Title: L.ColDisk(), Width: 90, Align: ui.End}, {Title: L.BkFrom()}, {Title: L.BkCheck(), Width: 90}}
	ui.Table(c, &p.vl, cols, len(g.Items), func(row, col int) {
		it := g.Items[row]
		switch col {
		case 0:
			ui.Text(c, it.Time.Local().Format("2006-01-02 15:04"))
		case 1:
			ui.Text(c, kindLabel(it.Kind))
		case 2:
			if it.Size > 0 {
				ui.Text(c, jobs.FormatBytes(it.Size))
			}
		case 3:
			from := it.Source.Host
			if it.Plan != "" {
				from += " · " + a.planName(it.Plan)
			}
			if !it.Consistent {
				from += " · " + L.BkInconsistent()
			}
			ui.Text(c, from).SingleLine()
		case 4:
			switch v := p.verified[itemKey(it)]; v {
			case "":
			case "ok":
				ui.Text(c, L.BkCheckOK()).TextColor(t.Success)
			default:
				ui.Text(c, L.BkCheckBad()).TextColor(t.Danger)
			}
		}
	}).Grow(1).FillWidth()
	if p.vsel < 0 || p.vsel >= len(g.Items) {
		return
	}
	it := g.Items[p.vsel]
	loc := p.locationOf(it)
	card(c, "", func() {
		kv(c, L.BkPath(), it.Path)
		if it.ID != "" {
			kv(c, L.BkSnapshot(), it.ID[:min(12, len(it.ID))])
		}
		if it.Note != "" {
			ui.Text(c, noteText(it.Note)).TextColor(t.Warning)
		}
		if v := p.verified[itemKey(it)]; v != "" && v != "ok" {
			errorText(c, v)
		}
	})
	ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
		ui.Button(c, L.BkVerify()).OnClick(func() { a.verifyItem(it) })
		switch it.Source.Kind {
		case backup.SourceDistro, backup.SourceUnknown:
			ui.PrimaryButton(c, L.BkRestoreNewDistro()).OnClick(func() { p.rin = restoreInput{mode: "distro"}; p.dlg = "restore" })
			if it.Kind != backup.KindVHDX {
				ui.Button(c, L.BkRestoreFiles()).OnClick(func() { p.rin = restoreInput{mode: "files", dir: "/"}; p.dlg = "restore" })
			}
			if it.Source.Name != "" {
				dangerButton(c, L.BkRestoreOver(it.Source.Name)).OnClick(func() { p.rin = restoreInput{mode: "over-distro", name: it.Source.Name}; p.dlg = "restore" })
			}
		}
		if it.Kind != backup.KindVHDX && it.Source.Kind != backup.SourceCompose && it.Source.Kind != backup.SourceDistro {
			ui.Button(c, L.BkRestoreNewVolume()).OnClick(func() { p.rin = restoreInput{mode: "volume"}; p.dlg = "restore" })
		}
		if it.Source.Kind == backup.SourceVolume {
			dangerButton(c, L.BkRestoreOver(it.Source.Name)).OnClick(func() { p.rin = restoreInput{mode: "over-volume", name: it.Source.Name}; p.dlg = "restore" })
		}
		if it.Source.Kind == backup.SourceCompose {
			ui.Button(c, L.BkExtractFiles()).OnClick(func() { p.rin = restoreInput{mode: "extract"}; p.dlg = "restore" })
		}
		if !loc.ReadOnly && (it.Kind == backup.KindArchive || it.Kind == backup.KindFolder) {
			dangerButton(c, L.Delete()).OnClick(func() {
				a.ask(L.BkDeleteTitle(), L.BkDeleteMessage(), L.Delete(), func(bool) {
					a.async(func(ctx context.Context) func() {
						err := backup.Delete(loc, it)
						a.k.Jobs.Audit("backup.delete", it.Path, "", err)
						return func() {
							if err != nil {
								a.notify(friendlyError(err), true)
							}
							p.loaded = false
							a.loadCatalog()
						}
					})
				})
			})
		}
	})
}

// noteText localizes the notes Scan attaches.
func noteText(n string) string {
	switch {
	case strings.HasPrefix(n, "no WSLPilot metadata"):
		return L.BkNoteNoMeta()
	case strings.HasPrefix(n, "disk image"):
		return L.BkNoteVHDX()
	case n == "incomplete":
		return L.BkNoteIncomplete()
	}
	return n
}

func (a *App) planName(id string) string {
	for _, p := range a.bk.plans {
		if p.ID == id {
			return p.Name
		}
	}
	return L.BkOtherPlan()
}

// verifyItem checks an item as a job.
func (a *App) verifyItem(it backup.Item) {
	a.startJob(L.BkVerify()+" "+filepath.Base(it.Path), func(ctx context.Context, j *jobs.Job) error {
		err := backup.Verify(ctx, it)
		res := "ok"
		if err != nil {
			res = friendlyError(err)
		}
		a.update(func() {
			if a.bk.verified == nil {
				a.bk.verified = map[string]string{}
			}
			a.bk.verified[itemKey(it)] = res
		})
		return err
	}, nil)
}

// restoreItem runs the chosen restore as a job.
func (a *App) restoreItem(it backup.Item, in restoreInput) {
	title := L.BkRestore() + " " + filepath.Base(it.Path)
	a.startJob(title, func(ctx context.Context, j *jobs.Job) error {
		j.SetProgress(-1, L.BkVerifying())
		if err := backup.Verify(ctx, it); err != nil {
			return fmt.Errorf("%s: %w", L.BkVerifyFailed(), err)
		}
		a.k.Backup.Log = j.Logf
		defer func() { a.k.Backup.Log = nil }()
		j.SetProgress(-1, title)
		var err error
		switch in.mode {
		case "distro":
			err = a.k.Backup.RestoreAsDistro(ctx, it, in.name, in.location)
		case "over-distro":
			_, err = a.k.Backup.ReplaceDistro(ctx, it, in.name, a.safetyWriter())
		case "files":
			prefix := strings.Trim(strings.TrimSpace(in.prefix), "/")
			var filter func(string) bool
			if prefix != "" {
				filter = func(n string) bool {
					n = strings.TrimPrefix(strings.TrimPrefix(n, "./"), "/")
					return n == prefix || strings.HasPrefix(n, prefix+"/")
				}
			}
			err = a.k.Backup.RestoreIntoDistro(ctx, it, in.name, in.dir, filter)
		case "volume":
			err = a.k.Backup.RestoreToVolume(ctx, it, in.name, volume.ImportOptions{Create: true})
		case "over-volume":
			if _, berr := a.k.Backup.BackupVolume(ctx, in.name, a.safetyWriter(), backupOptions()); berr != nil {
				return fmt.Errorf("%s: %w", L.JobBackupFailedStop(), berr)
			}
			err = a.k.Backup.RestoreToVolume(ctx, it, in.name, volume.ImportOptions{Replace: true})
		case "extract":
			err = extractFiles(ctx, it, in.dir)
		default:
			err = errors.New("unknown restore mode")
		}
		if strings.HasPrefix(in.mode, "over-") {
			a.k.Jobs.Audit("backup.restore-over", in.name, it.Path, err)
		}
		return err
	}, func() { a.loadDistros(); a.loadVolumes(); a.bk.loaded = false })
}

// extractFiles writes a Compose project backup's files into a folder.
// Notes: these are Windows files to begin with (compose.yaml, .env), so
// unpacking them on Windows loses nothing.
func extractFiles(ctx context.Context, it backup.Item, dir string) error {
	rc, err := backup.Open(ctx, it)
	if err != nil {
		return err
	}
	defer rc.Close()
	return backup.ExtractFlat(rc, dir)
}

func (a *App) plansView(c *ui.Context) {
	p := &a.bk
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
		ui.PrimaryButton(c, L.BkNewPlan()).OnClick(func() { a.editPlan(nil) })
	})
	muted(c, L.BkPlansHelp())
	if !a.settings.agentInstalled {
		ui.Row(c).Gap(smallGap).AlignItems(ui.Center).Children(func() {
			ui.Text(c, L.BkAgentMissing()).TextColor(t.Warning)
			ui.Button(c, L.SetInstallAgent()).OnClick(a.installAgent)
		})
	}
	ui.Row(c).Grow(1).FillWidth().Gap(gap).Children(func() {
		cols := []ui.TableColumn{{Title: L.ColName()}, {Title: L.BkSchedule(), Width: 160}, {Title: L.BkNext(), Width: 140}, {Title: L.BkLast()}}
		ui.Table(c, &p.pl, cols, len(p.plans), func(row, col int) {
			pl := p.plans[row]
			st := p.states[pl.ID]
			switch col {
			case 0:
				name := pl.Name
				if !pl.Enabled {
					name += " (" + L.BkPaused() + ")"
				}
				ui.Text(c, name)
			case 1:
				ui.Text(c, scheduleText(pl.Schedule))
			case 2:
				if pl.Enabled && !st.NextRun.IsZero() {
					ui.Text(c, st.NextRun.Local().Format("2006-01-02 15:04"))
				}
			case 3:
				switch {
				case st.LastResult == "ok":
					ui.Text(c, L.BkLastOK(st.LastRun.Local().Format("01-02 15:04"))).TextColor(t.Success)
				case st.LastResult != "":
					ui.Text(c, friendlyError(errors.New(st.LastResult))).TextColor(t.Danger).SingleLine()
				}
			}
		}).Grow(1).FillHeight()
		if p.psel >= 0 && p.psel < len(p.plans) {
			pl := p.plans[p.psel]
			ui.Column(c).Width(detailWidth).FillHeight().Gap(gap).Children(func() { a.planDetail(c, pl) })
		}
	})
}

func (a *App) planDetail(c *ui.Context, pl scheduler.Plan) {
	p := &a.bk
	ui.Text(c, pl.Name).FontSize(sectionSize).Bold()
	ui.Row(c).Gap(smallGap).Children(func() {
		ui.PrimaryButton(c, L.BkRunNow()).OnClick(func() {
			plan := pl
			a.startJob(L.BkRunNow()+" "+plan.Name, func(ctx context.Context, j *jobs.Job) error {
				a.k.Backup.Log = j.Logf
				defer func() { a.k.Backup.Log = nil }()
				r := a.k.Scheduler.Run(ctx, plan)
				if r.Result != "ok" {
					return errors.New(r.Result)
				}
				return nil
			}, func() { p.plansLoaded, p.loaded = false, false; p.history = nil })
		})
		ui.Button(c, L.Edit()).OnClick(func() { a.editPlan(&pl) })
		dangerButton(c, L.Delete()).OnClick(func() {
			a.ask(L.BkDeletePlanTitle(pl.Name), L.BkDeletePlanMessage(), L.Delete(), func(bool) {
				id := pl.ID
				a.async(func(ctx context.Context) func() {
					err := a.k.Scheduler.Delete(id)
					return func() {
						if err != nil {
							a.notify(friendlyError(err), true)
						}
						p.plansLoaded, p.psel = false, -1
					}
				})
			})
		})
	})
	card(c, "", func() {
		var ts []string
		for _, t := range pl.Targets {
			ts = append(ts, t.Name)
		}
		kv(c, L.BkTargets(), strings.Join(ts, ", "))
		kv(c, L.BkDestination(), kindLabel(pl.Destination.Kind)+" · "+pl.Destination.Path)
		kv(c, L.BkSchedule(), scheduleText(pl.Schedule))
		r := pl.Retention
		kv(c, L.BkRetention(), L.BkRetentionText(r.KeepLast, r.KeepDaily, r.KeepWeekly, r.KeepMonthly))
		if pl.Live {
			kv(c, L.BkConsistency(), L.BkLive())
		}
	})
	ui.Text(c, L.BkHistory()).Bold()
	if p.history == nil {
		id := pl.ID
		p.history = []scheduler.Record{}
		a.async(func(ctx context.Context) func() {
			h := a.k.Scheduler.History(id, 30)
			return func() { p.history = h }
		})
	}
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		for i, r := range p.history {
			line := r.Start.Local().Format("2006-01-02 15:04") + "  "
			if r.Result == "ok" {
				line += L.BkHistoryOK(len(r.Items), len(r.Expired))
				ui.Text(c.Key(i), line)
			} else {
				ui.Text(c.Key(i), line+friendlyError(errors.New(r.Result))).TextColor(c.Theme().Danger)
			}
		}
	})
}

// scheduleText describes a schedule.
func scheduleText(s scheduler.Schedule) string {
	switch s.Every {
	case scheduler.Hourly:
		return L.BkEveryHour(strings.SplitN(s.At, ":", 2)[len(strings.SplitN(s.At, ":", 2))-1])
	case scheduler.Daily:
		return L.BkEveryDay(s.At)
	case scheduler.Weekly:
		return L.BkEveryWeek(weekdayName(s.Weekday), s.At)
	case scheduler.Monthly:
		return L.BkEveryMonth(s.Day, s.At)
	}
	return L.BkManual()
}

func weekdayName(d time.Weekday) string {
	return []string{L.Sun(), L.Mon(), L.Tue(), L.Wed(), L.Thu(), L.Fri(), L.Sat()}[d]
}

// editPlan opens the plan editor (nil: a new plan).
func (a *App) editPlan(pl *scheduler.Plan) {
	in := planInput{targets: map[string]*bool{}, kind: string(backup.KindRepo), every: string(scheduler.Daily), at: "02:00",
		weekday: weekdayName(time.Sunday), day: "1", keepLast: "7", keepDaily: "7", keepWeekly: "4", keepMonthly: "6", retries: "2",
		compression: string(backup.CompressZstd)}
	in.p = scheduler.Plan{ID: scheduler.NewID(), Enabled: true, Notify: true,
		Destination: scheduler.Destination{Path: filepath.Join(a.backupDir(), "Repository")}}
	if pl != nil {
		in.p = *pl
		in.kind, in.every, in.at = string(pl.Destination.Kind), string(pl.Schedule.Every), pl.Schedule.At
		in.weekday, in.day = weekdayName(pl.Schedule.Weekday), itoa(max(pl.Schedule.Day, 1))
		r := pl.Retention
		in.keepLast, in.keepDaily, in.keepWeekly, in.keepMonthly = itoa(r.KeepLast), itoa(r.KeepDaily), itoa(r.KeepWeekly), itoa(r.KeepMonthly)
		in.retries = itoa(pl.Retry.Max)
		if pl.Destination.Compression != "" {
			in.compression = string(pl.Destination.Compression)
		}
	}
	a.bk.plan, a.bk.dlg = in, "plan"
	// Offer every distribution, volume and project as a target.
	a.async(func(ctx context.Context) func() {
		var opts []scheduler.Target
		if ds, err := a.k.WSL.List(ctx); err == nil {
			for _, d := range ds {
				opts = append(opts, scheduler.Target{Kind: backup.SourceDistro, Name: d.Name})
			}
		}
		if vs, err := a.k.WSLc.Volumes(ctx); err == nil {
			for _, v := range vs {
				opts = append(opts, scheduler.Target{Kind: backup.SourceVolume, Name: v.Name})
			}
		}
		if ps, err := a.k.Projects.List(); err == nil {
			for _, p := range ps {
				opts = append(opts, scheduler.Target{Kind: backup.SourceCompose, Name: p.Name})
			}
		}
		return func() {
			pi := &a.bk.plan
			pi.options = opts
			for _, o := range opts {
				on := false
				for _, t := range pi.p.Targets {
					if t == o {
						on = true
					}
				}
				pi.targets[o.Kind+"\x00"+o.Name] = &on
			}
		}
	})
}

// buildPlan turns the editor into a plan.
func (in *planInput) buildPlan() (scheduler.Plan, error) {
	p := in.p
	p.Targets = nil
	for _, o := range in.options {
		if on := in.targets[o.Kind+"\x00"+o.Name]; on != nil && *on {
			p.Targets = append(p.Targets, o)
		}
	}
	p.Destination.Kind = backup.Kind(in.kind)
	p.Destination.Compression = backup.Compression(in.compression)
	p.Schedule = scheduler.Schedule{Every: scheduler.Every(in.every), At: in.at}
	for i := 0; i < 7; i++ {
		if weekdayName(time.Weekday(i)) == in.weekday {
			p.Schedule.Weekday = time.Weekday(i)
		}
	}
	if p.Schedule.Every == scheduler.Monthly {
		p.Schedule.Day, _ = strconv.Atoi(in.day)
	}
	n := func(s string) int { v, _ := strconv.Atoi(strings.TrimSpace(s)); return max(v, 0) }
	p.Retention = backup.Policy{KeepLast: n(in.keepLast), KeepDaily: n(in.keepDaily), KeepWeekly: n(in.keepWeekly), KeepMonthly: n(in.keepMonthly)}
	p.Retry = scheduler.Retry{Max: n(in.retries), Initial: 10 * time.Minute, Limit: 2 * time.Hour}
	if strings.TrimSpace(p.Name) == "" {
		return p, errors.New(L.BkNeedName())
	}
	if len(p.Targets) == 0 {
		return p, errors.New(L.BkNeedTarget())
	}
	return p, p.Schedule.Validate()
}

func (a *App) backupDialogs(c *ui.Context) {
	p := &a.bk
	if p.dlg == "" {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(dialogWidth + 80).Gap(gap).Children(func() {
			switch p.dlg {
			case "restore":
				a.restoreDialog(c)
			case "plan":
				a.planDialog(c)
			}
		})
	})
	if !open {
		p.dlg = ""
	}
}

func (a *App) restoreDialog(c *ui.Context) {
	p := &a.bk
	g := p.groups[p.gsel]
	it := g.Items[p.vsel]
	in := &p.rin
	switch in.mode {
	case "distro":
		ui.Text(c, L.BkRestoreNewDistro()).FontSize(sectionSize).Bold()
		ui.Field(c, L.ColName(), func() { ui.TextInput(c, &in.name).Label(L.ColName()) })
		folderField(c, a, L.WSLLocation(), &in.location)
		dialogButtons(c, a, L.BkRestore(), validDistroName(in.name) && in.location != "", func() { a.restoreItem(it, *in) })
	case "files":
		ui.Text(c, L.BkRestoreFiles()).FontSize(sectionSize).Bold()
		muted(c, L.BkRestoreFilesHelp())
		var names []string
		for _, d := range a.wsl.distros {
			names = append(names, d.Name)
		}
		ui.Field(c, L.WSLDistribution(), func() { ui.Select(c, &in.name, names).Label(L.WSLDistribution()) })
		ui.Field(c, L.BkOnlyPath(), func() { ui.TextInput(c, &in.prefix).Placeholder("home/user/project").Label(L.BkOnlyPath()) })
		ui.Field(c, L.BkIntoFolder(), func() { ui.TextInput(c, &in.dir).Label(L.BkIntoFolder()) })
		dialogButtons(c, a, L.BkRestore(), in.name != "" && strings.HasPrefix(in.dir, "/"), func() { a.restoreItem(it, *in) })
	case "volume":
		ui.Text(c, L.BkRestoreNewVolume()).FontSize(sectionSize).Bold()
		ui.Field(c, L.ColName(), func() { ui.TextInput(c, &in.name).Label(L.ColName()) })
		dialogButtons(c, a, L.BkRestore(), strings.TrimSpace(in.name) != "", func() { a.restoreItem(it, *in) })
	case "over-distro", "over-volume":
		ui.Text(c, L.BkRestoreOver(in.name)).FontSize(sectionSize).Bold()
		ui.Text(c, L.BkRestoreOverMessage()).TextColor(c.Theme().Warning)
		muted(c, L.ConfirmTypeName(in.name))
		ui.TextInput(c, &in.location).Label(L.ConfirmTypeName(in.name))
		dialogButtons(c, a, L.BkRestore(), in.location == in.name, func() { a.restoreItem(it, *in) })
	case "extract":
		ui.Text(c, L.BkExtractFiles()).FontSize(sectionSize).Bold()
		folderField(c, a, L.BkIntoFolder(), &in.dir)
		dialogButtons(c, a, L.BkRestore(), in.dir != "", func() { a.restoreItem(it, *in) })
	}
}

func (a *App) planDialog(c *ui.Context) {
	in := &a.bk.plan
	ui.Text(c, L.BkPlan()).FontSize(sectionSize).Bold()
	ui.Scroll(c).MaxHeight(560).FillWidth().Children(func() {
		ui.Form(c, func() {
			ui.Field(c, L.ColName(), func() { ui.TextInput(c, &in.p.Name).Label(L.ColName()) })
			ui.Fieldset(c, L.BkTargets(), func() {
				if in.options == nil {
					ui.Spinner(c)
				}
				for _, o := range in.options {
					on := in.targets[o.Kind+"\x00"+o.Name]
					if on == nil {
						continue
					}
					label := groupTitle(backup.Group{Source: backup.Source{Kind: o.Kind, Name: o.Name}, Items: []backup.Item{{}}})
					ui.Checkbox(c.Key(o.Kind+o.Name), on, label)
				}
			})
			ui.Field(c, L.BkKind(), func() {
				ui.Select(c, &in.kind, []string{string(backup.KindRepo), string(backup.KindArchive), string(backup.KindFolder)}).Label(L.BkKind())
			}).Description(L.BkKindHelp())
			if in.kind == string(backup.KindArchive) {
				ui.Field(c, L.BkCompression(), func() {
					ui.Select(c, &in.compression, []string{string(backup.CompressZstd), string(backup.CompressGzip), string(backup.CompressNone)}).Label(L.BkCompression())
				})
			}
			folderField(c, a, L.BkDestination(), &in.p.Destination.Path)
			ui.Field(c, L.BkSchedule(), func() {
				ui.Select(c, &in.every, []string{string(scheduler.Hourly), string(scheduler.Daily), string(scheduler.Weekly), string(scheduler.Monthly), string(scheduler.Manual)}).Label(L.BkSchedule())
			})
			if in.every != string(scheduler.Manual) {
				ui.Field(c, L.BkAt(), func() { ui.TextInput(c, &in.at).Placeholder("02:00").Label(L.BkAt()) })
			}
			if in.every == string(scheduler.Weekly) {
				days := []string{L.Sun(), L.Mon(), L.Tue(), L.Wed(), L.Thu(), L.Fri(), L.Sat()}
				ui.Field(c, L.BkWeekday(), func() { ui.Select(c, &in.weekday, days).Label(L.BkWeekday()) })
			}
			if in.every == string(scheduler.Monthly) {
				ui.Field(c, L.BkDay(), func() { ui.TextInput(c, &in.day).Label(L.BkDay()) })
			}
			ui.Fieldset(c, L.BkRetention(), func() {
				ui.Field(c, L.BkKeepLast(), func() { ui.TextInput(c, &in.keepLast).Label(L.BkKeepLast()) })
				ui.Field(c, L.BkKeepDaily(), func() { ui.TextInput(c, &in.keepDaily).Label(L.BkKeepDaily()) })
				ui.Field(c, L.BkKeepWeekly(), func() { ui.TextInput(c, &in.keepWeekly).Label(L.BkKeepWeekly()) })
				ui.Field(c, L.BkKeepMonthly(), func() { ui.TextInput(c, &in.keepMonthly).Label(L.BkKeepMonthly()) })
			})
			ui.Field(c, L.BkRetries(), func() { ui.TextInput(c, &in.retries).Label(L.BkRetries()) })
			ui.Field(c, L.BkConsistency(), func() { ui.Checkbox(c, &in.p.Live, L.BkLiveOption()) }).Description(L.BkLiveHelp())
			ui.Field(c, L.BkNotify(), func() {
				ui.Checkbox(c, &in.p.Notify, L.BkNotifyFail())
				ui.Checkbox(c, &in.p.NotifySuccess, L.BkNotifyOK())
			})
			ui.Field(c, L.BkEnabled(), func() { ui.Switch(c, &in.p.Enabled).Label(L.BkEnabled()) })
		})
	})
	plan, err := in.buildPlan()
	if err != nil {
		errorText(c, friendlyError(err))
	}
	dialogButtons(c, a, L.Save(), err == nil, func() {
		a.async(func(ctx context.Context) func() {
			serr := a.k.Scheduler.Save(plan)
			return func() {
				if serr != nil {
					a.notify(friendlyError(serr), true)
				}
				a.bk.plansLoaded = false
			}
		})
	})
}
