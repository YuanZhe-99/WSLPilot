package gui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/configdoc"
	"github.com/YuanZhe-99/WSLPilot/internal/core"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/scheduler"
	"github.com/YuanZhe-99/WSLPilot/internal/version"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// settingsDoc stores the app's own preferences.
const settingsDoc = "settings.json"

// Prefs are WSLPilot's preferences.
type Prefs struct {
	Theme     string `json:"theme"` // system, light, dark
	BackupDir string `json:"backupDir,omitempty"`
}

// settingsPage is the state of the Settings page.
type settingsPage struct {
	loaded         bool
	prefs          Prefs
	backupDir      string
	themeChoice    string
	openAtLogin    bool
	agentInstalled bool
	agentChecked   bool
	tab            int
	wslc           confForm
	moving         bool
	moveTo         string
}

// loadPrefs reads preferences once and applies the theme.
func (a *App) loadPrefs() {
	s := &a.settings
	if s.loaded {
		return
	}
	s.loaded = true
	a.k.Store.Load(settingsDoc, 1, &s.prefs)
	if s.prefs.Theme == "" {
		s.prefs.Theme = "system"
	}
	s.backupDir = s.prefs.BackupDir
	s.themeChoice = themeLabel(s.prefs.Theme)
	if a.host != nil {
		s.openAtLogin = a.host.OpenAtLogin()
		a.host.SetTheme(s.prefs.Theme)
	}
	a.checkAgent()
}

// checkAgent finds out whether the scheduled task is registered.
func (a *App) checkAgent() {
	a.async(func(ctx context.Context) func() {
		ok := scheduler.Installed(ctx)
		return func() { a.settings.agentInstalled, a.settings.agentChecked = ok, true }
	})
}

// installAgent registers the agent task.
func (a *App) installAgent() {
	a.async(func(ctx context.Context) func() {
		err := scheduler.Install(ctx, core.AgentPath())
		return func() {
			if err != nil {
				a.notify(friendlyError(err), true)
			} else {
				a.notify(L.SetAgentInstalled(), false)
			}
			a.checkAgent()
		}
	})
}

func (a *App) savePrefs() {
	prefs := a.settings.prefs
	a.async(func(ctx context.Context) func() {
		err := a.k.Store.Save(settingsDoc, 1, prefs)
		return func() {
			if err != nil {
				a.notify(friendlyError(err), true)
			}
		}
	})
}

func themeLabel(mode string) string {
	switch mode {
	case "light":
		return L.SetThemeLight()
	case "dark":
		return L.SetThemeDark()
	}
	return L.SetThemeSystem()
}

func themeMode(label string) string {
	switch label {
	case L.SetThemeLight():
		return "light"
	case L.SetThemeDark():
		return "dark"
	}
	return "system"
}

// settingsView draws the Settings page.
func (a *App) settingsView(c *ui.Context) {
	s := &a.settings
	a.loadPrefs()
	a.header(c, L.NavSettings(), nil)
	ui.Tabs(c, &s.tab, L.SetGeneral(), L.SetWSLc(), L.SetAbout())
	switch s.tab {
	case 0:
		ui.Form(c, func() {
			ui.Field(c, L.SetTheme(), func() {
				ui.Select(c, &s.themeChoice, []string{L.SetThemeSystem(), L.SetThemeLight(), L.SetThemeDark()}).Label(L.SetTheme()).OnChange(func() {
					s.prefs.Theme = themeMode(s.themeChoice)
					a.host.SetTheme(s.prefs.Theme)
					a.savePrefs()
				})
			})
			ui.Field(c, L.SetOpenAtLogin(), func() {
				ui.Switch(c, &s.openAtLogin).Label(L.SetOpenAtLogin()).OnChange(func() {
					if err := a.host.SetOpenAtLogin(s.openAtLogin); err != nil {
						a.notify(friendlyError(err), true)
					}
				})
			})
			ui.Field(c, L.SetBackupDir(), func() {
				ui.Row(c).FillWidth().Gap(smallGap).Children(func() {
					ui.TextInput(c, &s.backupDir).Placeholder(filepath.Join(os.Getenv("USERPROFILE"), "WSLPilot Backups")).Label(L.SetBackupDir()).Grow(1)
					ui.Button(c, L.Save()).OnClick(func() {
						s.prefs.BackupDir = s.backupDir
						a.savePrefs()
						a.bk.loaded = false
					})
				})
			}).Description(L.SetBackupDirHelp())
			ui.Field(c, L.SetAgent(), func() {
				switch {
				case !s.agentChecked:
					ui.Spinner(c)
				case s.agentInstalled:
					ui.Row(c).Gap(smallGap).Children(func() {
						ui.Text(c, L.SetAgentOn()).TextColor(c.Theme().Success)
						ui.Button(c, L.SetUninstallAgent()).OnClick(func() {
							a.async(func(ctx context.Context) func() {
								scheduler.Uninstall(ctx)
								return a.checkAgent
							})
						})
					})
				default:
					ui.Button(c, L.SetInstallAgent()).OnClick(a.installAgent)
				}
			}).Description(L.SetAgentHelp())
			ui.Field(c, L.SetData(), func() {
				ui.Button(c, a.k.Store.Dir).OnClick(func() { a.host.OpenPath(a.k.Store.Dir) })
			})
		})
	case 1:
		a.wslcSettings(c)
	case 2:
		card(c, L.AppName(), func() {
			ui.Text(c, L.SetVersion(version.Version))
			muted(c, L.SetAboutText())
			ui.Button(c, "GitHub").OnClick(func() { a.host.OpenURL("https://github.com/YuanZhe-99/WSLPilot") })
		})
	}
}

// wslcSettings is the settings.yaml form.
func (a *App) wslcSettings(c *ui.Context) {
	f := &a.settings.wslc
	if f.fields == nil {
		f.fields, f.yaml = configdoc.WSLcSettings, true
	}
	path := filepath.Join(os.Getenv("LOCALAPPDATA"), "wslc", "settings.yaml")
	if a.report != nil && a.report.WSLc.Client.SettingsFile != "" {
		path = a.report.WSLc.Client.SettingsFile
	}
	reload := func() {
		f.loading, f.loaded = true, false
		a.async(func(ctx context.Context) func() {
			snap, err := configdoc.ReadFile(path)
			return func() {
				if err != nil {
					f.err, f.loading = err, false
					return
				}
				f.reset(snap)
			}
		})
	}
	if !f.loaded && !f.loading && f.err == nil {
		reload()
	}
	f.view(c, a, "settings.yaml", path, L.CfgEffectSession(), func(ctx context.Context, text string) error {
		_, err := f.snap.Save(text)
		return err
	}, reload)
	ui.Row(c).Gap(smallGap).Children(func() {
		ui.Button(c, L.SetMoveData()).OnClick(func() { a.settings.moveTo = ""; a.settings.moving = true })
		muted(c, L.SetMoveDataHelp())
	})
	a.moveDataDialog(c, path)
	ui.Button(c, L.SetRestartSession()).OnClick(func() {
		a.ask(L.SetRestartSession(), L.SetRestartSessionMessage(), L.SetRestartSession(), func(bool) {
			a.wslcJob(L.SetRestartSession(), "wslc.session.terminate", a.k.WSLc.TerminateSession)
		})
	})
}

// moveDataDialog asks where WSL Containers data should go and moves it.
func (a *App) moveDataDialog(c *ui.Context, settingsPath string) {
	s := &a.settings
	if !s.moving {
		return
	}
	ui.Modal(c, &s.moving, func() {
		ui.Column(c).Width(dialogWidth).Gap(gap).Children(func() {
			ui.Text(c, L.SetMoveData()).FontSize(sectionSize).Bold()
			ui.Text(c, L.SetMoveDataMessage()).TextColor(c.Theme().Warning)
			folderField(c, a, L.WSLNewLocation(), &s.moveTo)
			ui.Row(c).FillWidth().Gap(smallGap).Justify(ui.End).Children(func() {
				ui.Button(c, L.Cancel()).OnClick(func() { s.moving = false })
				ui.PrimaryButton(c, L.SetMoveData()).Disabled(!filepath.IsAbs(s.moveTo)).OnClick(func() {
					s.moving = false
					dest := s.moveTo
					staging := filepath.Join(a.backupDir(), "WSLc move "+time.Now().Format("20060102-150405"))
					a.startJob(L.SetMoveData(), func(ctx context.Context, j *jobs.Job) error {
						res, err := a.k.MoveWSLcData(ctx, dest, settingsPath, staging, func(s string) { j.SetProgress(-1, s); j.Logf(s) })
						a.k.Jobs.Audit("wslc.storage.move", dest, staging, err)
						if err == nil {
							j.Logf(L.SetMoveDataDone(res.Images, res.Volumes, strings.Join(res.Containers, ", "), res.Staging))
						}
						return err
					}, func() { a.settings.wslc.loaded = false; a.loadContainers(); a.loadVolumes() })
				})
			})
		})
	})
}
