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
	"github.com/YuanZhe-99/WSLPilot/internal/backup/wpbk"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/scheduler"
	"github.com/YuanZhe-99/WSLPilot/internal/secret"
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
	password string // unlock
	mode     string // distro, volume, over-distro, over-volume, files, unlock
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
	// encrypt asks for a new password (pw1, pw2) for the repository.
	encrypt  bool
	pw1, pw2 string
	options  []scheduler.Target
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
	own, label := a.backupDir(), L.BkDefaultLocation()
	a.async(func(ctx context.Context) func() {
		var locs []backup.Location
		a.k.Store.Load(locationsDoc, 1, &locs)
		all := append([]backup.Location{{Path: own, Label: label}}, locs...)
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
	if it := g.Items[0]; it.Kind == backup.KindRepo && it.Note == backup.NoteEncrypted {
		return L.BkEncryptedRepo(filepath.Base(it.Path))
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

// noteText localizes the notes Scan attaches.
func noteText(n string) string {
	switch {
	case n == backup.NoteEncrypted:
		return L.BkNoteEncrypted()
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
		// A copy of the runner, so this job's log does not catch other jobs' lines.
		runner := *a.k.Backup
		runner.Log = j.Logf
		r := &runner
		j.SetProgress(-1, title)
		var err error
		switch in.mode {
		case "distro":
			err = r.RestoreAsDistro(ctx, it, in.name, in.location)
		case "over-distro":
			_, err = r.ReplaceDistro(ctx, it, in.name, a.safetyWriter())
		case "files":
			prefix := strings.Trim(strings.TrimSpace(in.prefix), "/")
			var filter func(string) bool
			if prefix != "" {
				filter = func(n string) bool {
					n = strings.TrimPrefix(strings.TrimPrefix(n, "./"), "/")
					return n == prefix || strings.HasPrefix(n, prefix+"/")
				}
			}
			err = r.RestoreIntoDistro(ctx, it, in.name, in.dir, filter)
		case "volume":
			err = r.RestoreToVolume(ctx, it, in.name, volume.ImportOptions{Create: true})
		case "over-volume":
			if _, berr := r.BackupVolume(ctx, in.name, a.safetyWriter(), backupOptions()); berr != nil {
				return fmt.Errorf("%s: %w", L.JobBackupFailedStop(), berr)
			}
			err = r.RestoreToVolume(ctx, it, in.name, volume.ImportOptions{Replace: true})
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
		in.encrypt = pl.Destination.ProtectedPassword != ""
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
	keep := in.pw1 == "" && in.p.Destination.ProtectedPassword != ""
	if in.kind == string(backup.KindRepo) && in.encrypt && !keep {
		if len(in.pw1) < 8 {
			return p, errors.New(L.BkPasswordShort())
		}
		if in.pw1 != in.pw2 {
			return p, errors.New(L.BkPasswordMismatch())
		}
	}
	if in.kind != string(backup.KindRepo) {
		p.Destination.ProtectedPassword = ""
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
		ui.Text(c, L.BkRestoreNewDistro()).FontSize(16).Bold()
		ui.Field(c, L.ColName(), func() { ui.TextInput(c, &in.name).Label(L.ColName()) })
		folderField(c, a, L.WSLLocation(), &in.location)
		dialogButtons(c, a, L.BkRestore(), validDistroName(in.name) && in.location != "", func() { a.restoreItem(it, *in) })
	case "files":
		ui.Text(c, L.BkRestoreFiles()).FontSize(16).Bold()
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
		ui.Text(c, L.BkRestoreNewVolume()).FontSize(16).Bold()
		ui.Field(c, L.ColName(), func() { ui.TextInput(c, &in.name).Label(L.ColName()) })
		dialogButtons(c, a, L.BkRestore(), strings.TrimSpace(in.name) != "", func() { a.restoreItem(it, *in) })
	case "over-distro", "over-volume":
		ui.Text(c, L.BkRestoreOver(in.name)).FontSize(16).Bold()
		ui.Text(c, L.BkRestoreOverMessage()).TextColor(c.Theme().Warning)
		muted(c, L.ConfirmTypeName(in.name))
		ui.TextInput(c, &in.location).Label(L.ConfirmTypeName(in.name))
		dialogButtons(c, a, L.BkRestore(), in.location == in.name, func() { a.restoreItem(it, *in) })
	case "unlock":
		ui.Text(c, L.BkUnlock()).FontSize(16).Bold()
		muted(c, in.dir)
		ui.Field(c, L.BkPassword(), func() { ui.TextInput(c, &in.password).Password().AutoFocus().Label(L.BkPassword()) })
		dialogButtons(c, a, L.BkUnlock(), in.password != "", func() {
			dir, pw := in.dir, in.password
			a.async(func(ctx context.Context) func() {
				err := checkRepoPassword(dir, pw)
				if err == nil {
					a.k.RememberPassword(dir, pw)
				}
				return func() {
					if err != nil {
						a.notify(L.BkWrongPassword(), true)
						return
					}
					a.bk.loaded = false
					a.loadCatalog()
				}
			})
		})
	case "extract":
		ui.Text(c, L.BkExtractFiles()).FontSize(16).Bold()
		folderField(c, a, L.BkIntoFolder(), &in.dir)
		dialogButtons(c, a, L.BkRestore(), in.dir != "", func() { a.restoreItem(it, *in) })
	}
}

func (a *App) planDialog(c *ui.Context) {
	in := &a.bk.plan
	ui.Text(c, L.BkPlan()).FontSize(16).Bold()
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
			if in.kind == string(backup.KindRepo) {
				ui.Field(c, L.BkEncrypt(), func() { ui.Checkbox(c, &in.encrypt, L.BkEncryptOption()) }).Description(L.BkEncryptHelp())
				if in.encrypt {
					ui.Field(c, L.BkPassword(), func() { ui.TextInput(c, &in.pw1).Password().Label(L.BkPassword()) })
					ui.Field(c, L.BkPasswordAgain(), func() { ui.TextInput(c, &in.pw2).Password().Label(L.BkPasswordAgain()) })
				}
			}
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
			if in.encrypt && in.pw1 != "" {
				prot, perr := secret.Protect(in.pw1)
				if perr != nil {
					return func() { a.notify(friendlyError(perr), true) }
				}
				plan.Destination.ProtectedPassword = prot
			} else if !in.encrypt {
				plan.Destination.ProtectedPassword = ""
			}
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

// checkRepoPassword opens an encrypted repository read-only with password.
func checkRepoPassword(dir, password string) error {
	r, err := wpbk.Open(dir, wpbk.OpenOptions{ReadOnly: true, Password: password})
	if err != nil {
		return err
	}
	return r.Close()
}
