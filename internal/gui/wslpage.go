package gui

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/backup"
	"github.com/YuanZhe-99/WSLPilot/internal/configdoc"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/wsl"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// wslPage is the state of the WSL page.
type wslPage struct {
	// settingsFor is the distribution whose settings page shows ("" for
	// the list); scope 0 is its wsl.conf, 1 all of WSL (.wslconfig).
	settingsFor string
	scope       int

	measuring     map[string]bool
	compactOpen   bool // the compaction panel of the selected distribution
	compactBackup bool // its "Export a backup first"

	loaded, loading bool
	err             error
	distros         []wsl.Distro
	list            ui.ListState
	sel             int
	df              map[string]wsl.DiskUsage
	compact         *bool // wsl.exe has --manage --compact

	conf       confForm
	confDistro string
	wslconfig  confForm

	dlg       string // open dialog: install, import, export, move, resize, user
	online    []wsl.OnlineDistro
	onlineErr error
	in        wslInput
}

// wslInput holds the fields of the WSL dialogs.
type wslInput struct {
	distro, name, location, file, format, size, user, choice string
	web, vhd                                                 bool
	version                                                  string
}

// loadDistros reads the distribution list.
func (a *App) loadDistros() {
	p := &a.wsl
	if p.loading {
		return
	}
	p.loading = true
	a.async(func(ctx context.Context) func() {
		ds, err := a.k.WSL.List(ctx)
		var comp *bool
		if p.compact == nil {
			v := a.k.WSL.SupportsCompact(ctx)
			comp = &v
		}
		return func() {
			p.loading, p.loaded, p.err, p.distros = false, true, err, ds
			if comp != nil {
				p.compact = comp
			}
			if p.sel >= len(ds) {
				p.sel = len(ds) - 1
			}
		}
	})
}

// selected returns the chosen distribution.
func (p *wslPage) selected() (wsl.Distro, bool) {
	if p.sel < 0 || p.sel >= len(p.distros) {
		return wsl.Distro{}, false
	}
	return p.distros[p.sel], true
}

// distroJob runs a job that changes a distribution and reloads the list;
// audit names a destructive action to log ("" for none).
func (a *App) distroJob(title, distro, audit string, fn jobs.Func) {
	a.startJob(title, func(ctx context.Context, j *jobs.Job) error {
		err := fn(ctx, j)
		if audit != "" {
			a.k.Jobs.Audit(audit, distro, "", err)
		}
		return err
	}, a.loadDistros)
}

// measure reads df in a running distribution.
func (a *App) measure(name string) {
	if a.wsl.measuring == nil {
		a.wsl.measuring = map[string]bool{}
	}
	a.wsl.measuring[name] = true
	a.async(func(ctx context.Context) func() {
		u, err := a.k.WSL.DF(ctx, name)
		return func() {
			if err != nil {
				return // stays unmeasured; the disk shows its file size only
			}
			if a.wsl.df == nil {
				a.wsl.df = map[string]wsl.DiskUsage{}
			}
			a.wsl.df[name] = u
		}
	})
}

// setSparse turns sparse VHD on or off.
func (a *App) setSparse(name string, on bool) {
	a.distroJob(L.WSLSparseOn(), name, "", func(ctx context.Context, j *jobs.Job) error {
		if err := a.k.WSL.Terminate(ctx, name); err != nil {
			return err
		}
		return a.k.WSL.SetSparse(ctx, name, on)
	})
}

// backupOptions are the defaults for safety and manual backups.
func backupOptions() backup.Options { return backup.Options{} }

// safetyWriter is where backups made before a danger action go.
func (a *App) safetyWriter() backup.Writer {
	return &backup.ArchiveWriter{Dir: filepath.Join(a.backupDir(), "Safety"), Compression: backup.CompressZstd}
}

// backupDistroNow backs a distribution up into the default location.
func (a *App) backupDistroNow(name string) {
	a.startJob(L.WSLBackupNow()+" "+name, func(ctx context.Context, j *jobs.Job) error {
		w := &backup.RepoWriter{Dir: filepath.Join(a.backupDir(), "Repository")}
		it, err := a.k.Backup.BackupDistro(ctx, name, w, backupOptions())
		if err == nil {
			j.Logf(it.Path)
		}
		return err
	}, func() { a.loadDistros(); a.bk.loaded = false })
}

// openInstall opens the install dialog and loads the online list.
func (a *App) openInstall() {
	p := &a.wsl
	p.in = wslInput{version: "2"}
	p.dlg = "install"
	if p.online != nil {
		return
	}
	a.async(func(ctx context.Context) func() {
		ds, err := a.k.WSL.ListOnline(ctx)
		return func() { p.online, p.onlineErr = ds, err }
	})
}

// wslDialogs draws the open dialog of the WSL page.
func (a *App) wslDialogs(c *ui.Context) {
	p := &a.wsl
	if p.dlg == "" {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(dialogWidth).Gap(gap).Children(func() {
			switch p.dlg {
			case "install":
				a.installDialog(c)
			case "import":
				a.importDialog(c)
			case "export":
				a.exportDialog(c)
			case "move":
				ui.Text(c, L.WSLMoveTitle(p.in.distro)).FontSize(16).Bold()
				muted(c, L.WSLMoveHelp())
				folderField(c, a, L.WSLNewLocation(), &p.in.location)
				dialogButtons(c, a, L.WSLMove(), p.in.location != "", func() {
					in := p.in
					a.distroJob(L.WSLMove()+" "+in.distro, in.distro, "wsl.move", func(ctx context.Context, j *jobs.Job) error {
						if err := a.k.WSL.Terminate(ctx, in.distro); err != nil {
							return err
						}
						return a.k.WSL.Move(ctx, in.distro, in.location, j)
					})
				})
			case "resize":
				ui.Text(c, L.WSLResizeTitle(p.in.distro)).FontSize(16).Bold()
				muted(c, L.WSLResizeHelp())
				ui.Field(c, L.WSLNewSize(), func() { ui.TextInput(c, &p.in.size).Placeholder("256GB").Label(L.WSLNewSize()) })
				ok := (configdoc.Field{Kind: configdoc.Size}).Validate(p.in.size) == nil && p.in.size != ""
				dialogButtons(c, a, L.WSLResize(), ok, func() {
					in := p.in
					a.distroJob(L.WSLResize()+" "+in.distro, in.distro, "wsl.resize", func(ctx context.Context, j *jobs.Job) error {
						if err := a.k.WSL.Terminate(ctx, in.distro); err != nil {
							return err
						}
						return a.k.WSL.Resize(ctx, in.distro, in.size)
					})
				})
			case "user":
				ui.Text(c, L.WSLDefaultUser()).FontSize(16).Bold()
				ui.Field(c, L.WSLUserName(), func() { ui.TextInput(c, &p.in.user).Label(L.WSLUserName()) })
				dialogButtons(c, a, L.Save(), strings.TrimSpace(p.in.user) != "", func() {
					in := p.in
					a.distroJob(L.WSLDefaultUser(), in.distro, "", func(ctx context.Context, j *jobs.Job) error {
						return a.k.WSL.SetDefaultUser(ctx, in.distro, strings.TrimSpace(in.user))
					})
				})
			}
		})
	})
	if !open {
		p.dlg = ""
	}
}

// dialogButtons draws Cancel and the action; act runs and closes.
// Notes: the action is the label of the button that opened the dialog
// less its "…", which only says a dialog follows.
func dialogButtons(c *ui.Context, a *App, action string, enabled bool, act func()) {
	action = strings.TrimSuffix(action, "…")
	ui.Row(c).FillWidth().Gap(smallGap).Justify(ui.End).Children(func() {
		ui.Button(c, L.Cancel()).OnClick(a.closeDialogs)
		ui.PrimaryButton(c, action).Disabled(!enabled).OnClick(func() {
			act()
			a.closeDialogs()
		})
	})
}

// closeDialogs closes every page dialog.
func (a *App) closeDialogs() {
	a.wsl.dlg, a.ctr.dlg, a.vol.dlg, a.bk.dlg, a.cmp.dlg = "", "", "", "", ""
}

// folderField is a text field with a Browse button for a folder.
func folderField(c *ui.Context, a *App, label string, v *string) {
	pathField(c, a, label, v, true)
}

// pathField is a text field with a Browse button.
func pathField(c *ui.Context, a *App, label string, v *string, folder bool, exts ...string) {
	ui.Field(c.Key(label), label, func() {
		ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
			ui.TextInput(c, v).Label(label).Grow(1)
			ui.Button(c, L.Browse()).OnClick(func() {
				go func() {
					ps := a.host.PickFiles(label, folder, exts...)
					if len(ps) > 0 {
						a.update(func() { *v = ps[0] })
					}
				}()
			})
		})
	})
}

func (a *App) installDialog(c *ui.Context) {
	p := &a.wsl
	ui.Text(c, L.WSLInstallTitle()).FontSize(16).Bold()
	switch {
	case p.onlineErr != nil:
		errorText(c, friendlyError(p.onlineErr))
	case p.online == nil:
		ui.Spinner(c)
	default:
		names := make([]string, len(p.online))
		for i, d := range p.online {
			names[i] = d.FriendlyName + " (" + d.Name + ")"
		}
		ui.Field(c, L.WSLDistribution(), func() {
			ui.Select(c, &p.in.choice, names).Label(L.WSLDistribution())
		})
		for i, n := range names {
			if n == p.in.choice {
				p.in.distro = p.online[i].Name
			}
		}
	}
	ui.Field(c, L.WSLNameOptional(), func() { ui.TextInput(c, &p.in.name).Label(L.WSLNameOptional()) })
	folderField(c, a, L.WSLLocationOptional(), &p.in.location)
	ui.Checkbox(c, &p.in.web, L.WSLWebDownload())
	dialogButtons(c, a, L.WSLInstall(), p.in.distro != "", func() {
		in := p.in
		a.startJob(L.WSLInstall()+" "+in.distro, func(ctx context.Context, j *jobs.Job) error {
			return a.k.WSL.Install(ctx, in.distro, wsl.InstallOptions{Name: in.name, Location: in.location, WebDownload: in.web}, j)
		}, a.loadDistros)
	})
}

func (a *App) importDialog(c *ui.Context) {
	p := &a.wsl
	ui.Text(c, L.WSLImportTitle()).FontSize(16).Bold()
	muted(c, L.WSLImportHelp())
	pathField(c, a, L.WSLImportFile(), &p.in.file, false, "tar", "gz", "xz", "vhdx", "wsl")
	ui.Field(c, L.ColName(), func() { ui.TextInput(c, &p.in.name).Label(L.ColName()) })
	folderField(c, a, L.WSLLocation(), &p.in.location)
	ui.Checkbox(c, &p.in.vhd, L.WSLImportVHD())
	ok := p.in.file != "" && validDistroName(p.in.name) && p.in.location != ""
	if p.in.name != "" && !validDistroName(p.in.name) {
		errorText(c, L.WSLBadName())
	}
	dialogButtons(c, a, L.WSLImport(), ok, func() {
		in := p.in
		a.startJob(L.WSLImport()+" "+in.name, func(ctx context.Context, j *jobs.Job) error {
			vhd := in.vhd || strings.HasSuffix(strings.ToLower(in.file), ".vhdx")
			return a.k.WSL.Import(ctx, in.name, in.location, in.file, wsl.ImportOptions{VHD: vhd}, nil, j)
		}, a.loadDistros)
	})
}

func (a *App) exportDialog(c *ui.Context) {
	p := &a.wsl
	ui.Text(c, L.WSLExportTitle(p.in.distro)).FontSize(16).Bold()
	muted(c, L.WSLExportHelp())
	if p.in.format == string(wsl.FormatVHD) {
		ui.Text(c, L.WSLExportVHDWarning()).TextColor(c.Theme().Warning)
	}
	ui.Field(c, L.WSLFormat(), func() {
		ui.Select(c, &p.in.format, []string{"tar", "tar.gz", "tar.xz", "vhd"}).Label(L.WSLFormat())
	})
	pathField(c, a, L.WSLExportTo(), &p.in.file, false)
	dialogButtons(c, a, L.WSLExport(), p.in.file != "", func() {
		in := p.in
		a.distroJob(L.WSLExport()+" "+in.distro, in.distro, "", func(ctx context.Context, j *jobs.Job) error {
			d, err := a.k.WSL.Get(ctx, in.distro)
			if err != nil {
				return err
			}
			switch {
			case in.format == string(wsl.FormatVHD):
				// The utility VM keeps the disk attached after --terminate
				// (ERROR_SHARING_VIOLATION, verified on 3.0.3.0): only a
				// shutdown releases it.
				j.SetProgress(-1, L.WSLShutdown())
				if err := a.k.WSL.Shutdown(ctx); err != nil {
					return err
				}
			case d.Running:
				j.SetProgress(-1, L.JobStopping(in.distro))
				if err := a.k.WSL.Terminate(ctx, in.distro); err != nil {
					return err
				}
			}
			err = a.k.WSL.Export(ctx, in.distro, in.file, wsl.ExportFormat(in.format), nil)
			if err != nil {
				os.Remove(in.file)
			}
			return err
		})
	})
}

// validDistroName follows WSL's rule: letters, digits, '.', '-', '_'.
func validDistroName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// updateCommand is the package update command for a distribution flavor
// (os-release ID, as WSL records it), run in a terminal the user watches.
// Returns: "" for a flavor WSLPilot does not know.
func updateCommand(flavor string) string {
	switch strings.ToLower(flavor) {
	case "ubuntu", "debian", "kali", "kali-linux", "pengwin":
		return "apt-get update && apt-get -y full-upgrade"
	case "fedora", "almalinux", "rocky", "rhel", "centos", "ol":
		return "dnf -y upgrade"
	case "opensuse-tumbleweed":
		return "zypper --non-interactive dup"
	case "opensuse-leap", "sles", "opensuse":
		return "zypper --non-interactive up"
	case "arch", "archlinux":
		return "pacman -Syu --noconfirm"
	case "alpine":
		return "apk update && apk upgrade"
	}
	return ""
}
