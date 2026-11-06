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
	"github.com/YuanZhe-99/WSLPilot/internal/compose"
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

	// manifests holds the contents of project backups by item key: nil
	// while reading or for a version 1 backup (see manifestDone).
	manifests    map[string]*backup.ProjectManifest
	manifestDone map[string]bool
}

// restoreInput holds the restore dialog fields.
type restoreInput struct {
	password string // unlock
	mode     string // distro, volume, over-distro, over-volume, files, unlock
	name     string
	location string
	dir      string
	prefix   string
	// vols are the volumes a project restore brings back.
	vols []volChoice
}

// volChoice is one volume of a project restore: the volume in the backup,
// the volume to restore it into, and the typed name confirming a replace.
type volChoice struct {
	from, key, into, confirm string
	on                       bool
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
	// hour and minute make up at; volProject maps a volume to the Compose
	// project that owns it; external lists each project.s external volumes.
	hour, minute string
	volProject   map[string]string
	external     map[string][]string
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
		case "project":
			err = a.restoreProject(ctx, j, r, it, in)
		default:
			err = errors.New("unknown restore mode")
		}
		if strings.HasPrefix(in.mode, "over-") {
			a.k.Jobs.Audit("backup.restore-over", in.name, it.Path, err)
		}
		return err
	}, func() { a.loadDistros(); a.loadVolumes(); a.bk.loaded = false })
}

// restoreProject writes a project backup's files and folders into a
// folder, then each chosen volume into a volume: a new one, or an
// existing one after a safety backup (the person typed its name).
func (a *App) restoreProject(ctx context.Context, j *jobs.Job, r *backup.Runner, it backup.Item, in restoreInput) error {
	if in.dir != "" {
		j.SetProgress(-1, L.BkRestoreFilesStep())
		placed, err := backup.ExtractProject(ctx, it, in.dir)
		for _, p := range placed {
			j.Logf(p)
		}
		if err != nil {
			return err
		}
	}
	existing := map[string]bool{}
	if vs, err := a.k.WSLc.Volumes(ctx); err == nil {
		for _, v := range vs {
			existing[v.Name] = true
		}
	}
	for _, v := range in.vols {
		if !v.on {
			continue
		}
		into := strings.TrimSpace(v.into)
		j.SetProgress(-1, L.BkRestoreVolumeStep(into))
		o := volume.ImportOptions{Create: true}
		if into == v.from && v.key != "" {
			o.Labels = map[string]string{compose.LabelProject: it.Source.Name, compose.LabelVolume: v.key, compose.LabelBackend: string(compose.BackendBuiltin)}
		}
		if existing[into] {
			if v.confirm != into {
				return fmt.Errorf("%s: %s", into, L.BkVolumeNotConfirmed())
			}
			if _, err := r.BackupVolume(ctx, into, a.safetyWriter(), backupOptions()); err != nil {
				return fmt.Errorf("%s: %w", L.JobBackupFailedStop(), err)
			}
			o = volume.ImportOptions{Replace: true}
		}
		err := r.RestoreProjectVolume(ctx, it, v.from, into, o)
		if existing[into] {
			a.k.Jobs.Audit("backup.restore-over", into, it.Path, err)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", into, err)
		}
	}
	return nil
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
	in.hour, in.minute = "02", "00"
	if h, m, ok := strings.Cut(in.at, ":"); ok {
		in.hour, in.minute = h, m
	}
	a.bk.plan, a.bk.dlg = in, "plan"
	// Offer every distribution, volume and project as a target.
	a.async(func(ctx context.Context) func() {
		var opts []scheduler.Target
		volProject, external := map[string]string{}, map[string][]string{}
		if ds, err := a.k.WSL.List(ctx); err == nil {
			for _, d := range ds {
				opts = append(opts, scheduler.Target{Kind: backup.SourceDistro, Name: d.Name})
			}
		}
		if vs, err := a.k.WSLc.Volumes(ctx); err == nil {
			for _, v := range vs {
				opts = append(opts, scheduler.Target{Kind: backup.SourceVolume, Name: v.Name})
				if pr := v.Labels[compose.LabelProject]; pr != "" {
					volProject[v.Name] = pr
				}
			}
		}
		if ps, err := a.k.Projects.List(); err == nil {
			for _, p := range ps {
				opts = append(opts, scheduler.Target{Kind: backup.SourceCompose, Name: p.Name})
				if d, err := a.k.ProjectData(ctx, p); err == nil {
					external[p.Name] = d.External
				}
			}
		}
		return func() {
			pi := &a.bk.plan
			pi.options = opts
			pi.volProject, pi.external = volProject, external
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
	in.at = in.hour + ":" + in.minute
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
	case "project":
		ui.Text(c, L.BkRestoreProjectTitle(it.Source.Name)).FontSize(16).Bold()
		muted(c, L.BkRestoreProjectHelp())
		folderField(c, a, L.BkFilesInto(), &in.dir)
		small(c, L.BkFilesIntoHelp())
		exists := map[string]bool{}
		for _, v := range a.vol.list {
			exists[v.Name] = true
		}
		some, valid := in.dir != "", true
		for i := range in.vols {
			v := &in.vols[i]
			ui.Checkbox(c.Key("vol-"+v.from), &v.on, L.BkRestoreVolumeCheck(v.from))
			if !v.on {
				continue
			}
			into := strings.TrimSpace(v.into)
			ui.Column(c.Key("into-"+v.from)).FillWidth().Gap(6).Padding(0, 0, 0, 28).Children(func() {
				ui.Field(c, L.BkVolumeInto(), func() { ui.TextInput(c, &v.into).Label(L.BkVolumeInto()) })
				if exists[into] {
					ui.Text(c, L.BkVolumeExists(into)).TextColor(c.Theme().Warning)
					ui.TextInput(c, &v.confirm).Placeholder(into).Label(L.ConfirmTypeName(into))
					small(c, L.ConfirmTypeName(into))
				}
			})
			some = true
			if into == "" || (exists[into] && v.confirm != into) {
				valid = false
			}
		}
		dialogButtons(c, a, L.BkRestore(), some && valid, func() { a.restoreItem(it, *in) })
	}
}

func (a *App) planDialog(c *ui.Context) {
	in := &a.bk.plan
	ui.Text(c, L.BkPlan()).FontSize(16).Bold()
	ui.Scroll(c.Key("plan-form")).MaxHeight(560).FillWidth().Children(func() {
		ui.Form(c, func() {
			ui.Field(c, L.ColName(), func() { ui.TextInput(c, &in.p.Name).Label(L.ColName()) })
			ui.Fieldset(c, L.BkTargets(), func() {
				if in.options == nil {
					ui.Spinner(c)
				}
				chosen := func(kind, name string) bool {
					on := in.targets[kind+"\x00"+name]
					return on != nil && *on
				}
				for _, o := range in.options {
					on := in.targets[o.Kind+"\x00"+o.Name]
					if on == nil {
						continue
					}
					label := groupTitle(backup.Group{Source: backup.Source{Kind: o.Kind, Name: o.Name}, Items: []backup.Item{{}}})
					ui.Checkbox(c.Key(o.Kind+o.Name), on, label)
					switch {
					case o.Kind == backup.SourceVolume && in.volProject[o.Name] != "" && chosen(backup.SourceCompose, in.volProject[o.Name]):
						small(c, L.BkInProject(in.volProject[o.Name])).Padding(0, 0, 0, 28)
					case o.Kind == backup.SourceCompose && *on && len(in.external[o.Name]) > 0:
						small(c, L.BkProjectExternal(strings.Join(in.external[o.Name], ", "))).TextColor(c.Theme().Warning).Padding(0, 0, 0, 28)
					}
				}
			})
			ui.Field(c, L.BkKind(), func() {
				labeledSelect(c, "kind", &in.kind, []string{string(backup.KindRepo), string(backup.KindArchive), string(backup.KindFolder)},
					func(v string) string { return kindLabel(backup.Kind(v)) }).Label(L.BkKind())
			}).Description(L.BkKindHelp())
			if in.kind == string(backup.KindArchive) {
				ui.Field(c, L.BkCompression(), func() {
					labeledSelect(c, "compression", &in.compression, []string{string(backup.CompressZstd), string(backup.CompressGzip), string(backup.CompressNone)},
						compressionLabel).Label(L.BkCompression())
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
				labeledSelect(c, "every", &in.every, []string{string(scheduler.Daily), string(scheduler.Weekly), string(scheduler.Monthly), string(scheduler.Hourly), string(scheduler.Manual)},
					everyLabel).Label(L.BkSchedule())
			})
			if in.every == string(scheduler.Weekly) {
				days := []string{L.Mon(), L.Tue(), L.Wed(), L.Thu(), L.Fri(), L.Sat(), L.Sun()}
				ui.Field(c, L.BkWeekday(), func() { ui.Select(c, &in.weekday, days).Label(L.BkWeekday()) })
			}
			if in.every == string(scheduler.Monthly) {
				ui.Field(c, L.BkDay(), func() {
					labeledSelect(c, "day", &in.day, numbers(1, 28, 1), func(v string) string { return v }).Label(L.BkDay())
				})
			}
			switch in.every {
			case string(scheduler.Manual):
			case string(scheduler.Hourly):
				ui.Field(c, L.BkAtMinute(), func() {
					labeledSelect(c, "minute", &in.minute, numbers(0, 55, 5), func(v string) string { return ":" + v }).Label(L.BkAtMinute())
				})
			default:
				ui.Field(c, L.BkAt(), func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						labeledSelect(c, "hour", &in.hour, numbers(0, 23, 1), func(v string) string { return v }).MinWidth(80).Label(L.BkHour())
						ui.Text(c, ":")
						labeledSelect(c, "minute", &in.minute, numbers(0, 55, 5), func(v string) string { return v }).MinWidth(80).Label(L.BkMinute())
					})
				})
			}
			ui.Fieldset(c, L.BkRetention(), func() {
				small(c, L.BkRetentionHelp())
				ui.Field(c, L.BkKeepLast(), func() { ui.TextInput(c, &in.keepLast).Width(90).Label(L.BkKeepLast()) })
				ui.Field(c, L.BkKeepDaily(), func() { ui.TextInput(c, &in.keepDaily).Width(90).Label(L.BkKeepDaily()) })
				ui.Field(c, L.BkKeepWeekly(), func() { ui.TextInput(c, &in.keepWeekly).Width(90).Label(L.BkKeepWeekly()) })
				ui.Field(c, L.BkKeepMonthly(), func() { ui.TextInput(c, &in.keepMonthly).Width(90).Label(L.BkKeepMonthly()) })
			})
			ui.Field(c, L.BkRetries(), func() { ui.TextInput(c, &in.retries).Width(90).Label(L.BkRetries()) }).Description(L.BkRetriesHelp())
			ui.Field(c, L.BkConsistency(), func() { ui.Checkbox(c, &in.p.Live, L.BkLiveOption()) }).Description(L.BkLiveHelp())
			ui.Field(c, L.BkNotify(), func() {
				ui.Checkbox(c, &in.p.Notify, L.BkNotifyFail())
				ui.Checkbox(c, &in.p.NotifySuccess, L.BkNotifyOK())
			})
			ui.Field(c, L.BkEnabled(), func() { ui.Switch(c, &in.p.Enabled).Label(L.BkEnabled()) }).Description(L.BkEnabledHelp())
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

// numbers lists from..to by step as two-digit strings ("00", "05", …).
func numbers(from, to, step int) []string {
	var out []string
	for n := from; n <= to; n += step {
		out = append(out, fmt.Sprintf("%02d", n))
	}
	return out
}

// everyLabel names a schedule frequency.
func everyLabel(v string) string {
	switch scheduler.Every(v) {
	case scheduler.Hourly:
		return L.BkEveryHourly()
	case scheduler.Daily:
		return L.BkEveryDaily()
	case scheduler.Weekly:
		return L.BkEveryWeekly()
	case scheduler.Monthly:
		return L.BkEveryMonthly()
	}
	return L.BkEveryManual()
}

// compressionLabel names an archive compression.
func compressionLabel(v string) string {
	switch backup.Compression(v) {
	case backup.CompressZstd:
		return L.BkCompZstd()
	case backup.CompressGzip:
		return L.BkCompGzip()
	}
	return L.BkCompNone()
}
