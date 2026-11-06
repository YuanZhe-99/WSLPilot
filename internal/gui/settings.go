package gui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/WSLPilot/internal/configdoc"
	"github.com/YuanZhe-99/WSLPilot/internal/core"
	"github.com/YuanZhe-99/WSLPilot/internal/jobs"
	"github.com/YuanZhe-99/WSLPilot/internal/scheduler"
	"github.com/YuanZhe-99/WSLPilot/internal/volume"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// Prefs are WSLPilot's preferences (shared with the agent through core).
type Prefs = core.Prefs

// The Settings sheet's layout.
const (
	sheetX       float32 = 56  // from the window's sides
	sheetTop     float32 = 44  // from its top
	sheetBottom  float32 = 28  // from its bottom
	sheetHeaderH float32 = 52  // its header
	settingsNavW float32 = 240 // its sections
	searchH      float32 = 34  // the search field over them
	sheetNavRowH float32 = 34  // a section
)

// settingsSection is a section of the Settings sheet.
type settingsSection int

// Sections.
const (
	setGeneral settingsSection = iota
	setBackups
	setContainers
	setAbout
)

// settingsItem is a section in the sheet's navigation.
type settingsItem struct {
	sec   settingsSection
	icon  *ui.SVG
	label string
}

// settingsGroup is a group of the sheet's sections.
type settingsGroup struct {
	title string
	items []settingsItem
}

// settingsGroups are the sheet's sections: the app's preferences, this
// computer, and the app itself.
func settingsGroups() []settingsGroup {
	return []settingsGroup{
		{L.SetGroupPreferences(), []settingsItem{{setGeneral, iconSliders, L.SetGeneral()}}},
		{L.SetGroupComputer(), []settingsItem{
			{setBackups, iconHistory, L.SetBackupsAgent()},
			{setContainers, iconBox, L.SetWSLc()},
		}},
		{L.AppName(), []settingsItem{{setAbout, iconInfo, L.SetAboutUpdates()}}},
	}
}

// sectionName is a section's name in the navigation.
func sectionName(s settingsSection) string {
	for _, g := range settingsGroups() {
		for _, it := range g.items {
			if it.sec == s {
				return it.label
			}
		}
	}
	return ""
}

// settingsPage is the state of the Settings sheet's pages.
type settingsPage struct {
	loaded         bool
	prefs          Prefs
	backupDir      string
	helperImage    string
	openAtLogin    bool
	agentInstalled bool
	agentChecked   bool
	wslc           confForm
	moving         bool
	moveTo         string
	themeIdx       int // bound to the appearance segments
	closeIdx       int // bound to the close-button segments
}

// settingsFocus is the setting a search hit leads to: its title, until
// when its row stays tinted, and whether its page scrolled to it yet.
type settingsFocus struct {
	title    string
	until    time.Time
	scrolled bool
}

// focusTime is how long a setting found by search stays tinted.
const focusTime = 2 * time.Second

// openSettings opens the Settings sheet at a section.
func (a *App) openSettings(s settingsSection) {
	a.loadPrefs()
	a.settingsOpen, a.settingsAt, a.focus = true, s, settingsFocus{}
}

// loadPrefs reads preferences once and applies the theme.
func (a *App) loadPrefs() {
	s := &a.settings
	if s.loaded {
		return
	}
	s.loaded = true
	s.prefs = a.k.LoadPrefs()
	if s.prefs.Theme == "" {
		s.prefs.Theme = "system"
	}
	s.backupDir = s.prefs.BackupDir
	s.helperImage = s.prefs.HelperImage
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

// savePrefs stores the preferences on a goroutine.
func (a *App) savePrefs() {
	prefs := a.settings.prefs
	a.async(func(ctx context.Context) func() {
		err := a.k.SavePrefs(prefs)
		return func() {
			if err != nil {
				a.notify(friendlyError(err), true)
			}
		}
	})
}

// setTheme applies and remembers the appearance.
func (a *App) setTheme(mode string) {
	a.loadPrefs()
	a.settings.prefs.Theme = mode
	if a.host != nil {
		a.host.SetTheme(mode)
	}
	a.savePrefs()
}

// themeLabel names an appearance.
func themeLabel(mode string) string {
	switch mode {
	case "light":
		return L.SetThemeLight()
	case "dark":
		return L.SetThemeDark()
	}
	return L.SetThemeSystem()
}

// settingsSheet is Settings: a sheet over the whole window, dimmed behind;
// its sections on the left under a search field, the chosen section beside
// them. On Windows the window buttons stay where the header holds them.
func (a *App) settingsSheet(c *ui.Context) {
	if !a.settingsOpen {
		return
	}
	t := c.Theme()
	w, h := c.Size()
	open := true
	ui.DialogBase(c, &open, func(back, panel ui.Element) {
		back.Background(sheetScrim).Padding(sheetTop, sheetX, sheetBottom, sheetX)
		panel.Radius(paneRadius).Background(t.Background).Shadow(0, 10, 30, 0, sheetShadow).Clip()
		ui.Column(c.Key("sheet")).Width(max(w-2*sheetX, 600)).Height(max(h-sheetTop-sheetBottom, 420)).Children(func() {
			ui.Row(c.Key("sheet-head")).Height(sheetHeaderH).Shrink(0).Padding(0, 12, 0, 20).AlignItems(ui.Center).Children(func() {
				ui.Text(c, L.NavSettings()).FontSize(16).Bold().Grow(1)
				if iconButton(c, "sheet-close", iconClose, L.Close(), false) {
					open = false
				}
			})
			ui.Divider(c)
			ui.Row(c.Key("sheet-body")).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				ui.Scroll(c.Key("sheet-nav")).Width(settingsNavW).Shrink(0).Padding(14, 12).Gap(2).Children(func() {
					a.settingsNav(c)
				})
				ui.Box(c).Width(1).Shrink(0).Background(t.Border)
				ui.Column(c.Key("sheet-page")).Grow(1).MinWidth(0).Children(func() { a.settingsPageView(c) })
			})
		})
	})
	if ownWindowButtons && a.host != nil {
		ui.Overlay(c, func() {
			// Exactly over the pane header's buttons (head: Padding(6, 8, …), Gap(10)).
			ui.Row(c.Key("sheet-win")).Absolute().Top(paneMargin + 6 + (paneHeaderH+12-6-winButtonH)/2).Right(2 * paneMargin).Gap(10).AlignItems(ui.Center).Children(func() {
				a.windowButtons(c, "sheet-win", true)
			})
		})
	}
	if !open {
		a.settingsOpen, a.focus = false, settingsFocus{}
	}
}

// settingsNav is the search field and, without a search, the sections by
// group; with one, the settings it finds.
func (a *App) settingsNav(c *ui.Context) {
	t := c.Theme()
	ui.Row(c.Key("sheet-search")).Height(searchH).Shrink(0).Gap(8).Padding(0, 10).Radius(8).Border(1, t.Border).AlignItems(ui.Center).Margin(0, 0, 6, 0).Children(func() {
		ui.Icon(c, iconSearch).Size(14, 14).TextColor(t.TextMuted)
		ui.TextInputBase(c.Key("sheet-search-in"), &a.settingsQ).Grow(1).Placeholder(L.SetSearch()).Label(L.SetSearch())
	})
	if strings.TrimSpace(a.settingsQ) != "" {
		hits := searchSettings(settingsHits(), a.settingsQ)
		if len(hits) == 0 {
			small(c, L.SetSearchNone()).Padding(10)
		}
		for i, h := range hits {
			row := ui.ButtonBase(c.Key("hit-"+strconv.Itoa(i))).FillWidth().Radius(8).Padding(6, 10).Label(h.title)
			if row.Hovered() {
				row.Background(t.SurfaceHover)
			}
			row.Children(func() {
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					ui.Text(c, h.title)
					small(c, sectionName(h.sec)).SingleLine()
				})
			})
			if row.Clicked() {
				a.settingsAt, a.focus = h.sec, settingsFocus{title: h.title, until: time.Now().Add(focusTime)}
			}
		}
		return
	}
	for gi, g := range settingsGroups() {
		small(c, g.title).Bold().Padding(12, 10, 4, 10)
		for _, it := range g.items {
			row := ui.ButtonBase(c.Key("nav-"+strconv.Itoa(gi)+"-"+strconv.Itoa(int(it.sec)))).Height(sheetNavRowH).FillWidth().Radius(8).Padding(0, 10).Label(it.label)
			fg := t.TextMuted
			switch {
			case it.sec == a.settingsAt:
				row.Background(t.SurfaceHover)
				fg = t.Text
			case row.Hovered():
				row.Background(t.SurfaceHover.Alpha(0.6))
			}
			row.Children(func() {
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, it.icon).Size(navIconSize, navIconSize).TextColor(fg)
					txt := ui.Text(c, it.label).TextColor(fg).SingleLine()
					if it.sec == a.settingsAt {
						txt.FontWeight(600)
					}
				})
			})
			if row.Clicked() {
				a.settingsAt, a.focus = it.sec, settingsFocus{}
			}
		}
	}
}

// settingsHit is a setting search finds: its section, its title and the
// words that describe it.
type settingsHit struct {
	sec   settingsSection
	title string
	words string
}

// settingsHits are every setting search can find.
func settingsHits() []settingsHit {
	return []settingsHit{
		{setGeneral, L.SetTheme(), L.SetThemeHelp()},
		{setGeneral, L.SetLanguage(), L.SetLanguageHelp() + " English 简体中文 Language 语言"},
		{setGeneral, L.SetOpenAtLogin(), L.SetOpenAtLoginHelp()},
		{setGeneral, L.SetCloseWindow(), L.SetCloseWindowHelp()},
		{setBackups, L.SetAgent(), L.SetAgentHelp()},
		{setBackups, L.SetBackupDir(), L.SetBackupDirHelp()},
		{setBackups, L.SetHelperImage(), L.SetHelperImageHelp()},
		{setContainers, L.SetWSLcFile(), L.CfgEffectSession()},
		{setContainers, L.SetMoveData(), L.SetMoveDataHelp()},
		{setContainers, L.SetRestartSession(), L.SetRestartSessionMessage()},
		{setAbout, L.SetUpdates(), L.SetUpdatesHelp()},
		{setAbout, L.SetCheckDaily(), L.SetCheckDailyHelp()},
		{setAbout, L.SetAutoInstall(), L.SetAutoInstallHelp()},
		{setAbout, L.SetLicenses(), L.SetLicensesHelp()},
		{setAbout, L.SetData(), L.SetDataHelp()},
	}
}

// searchSettings lists the settings whose title or words hold every word
// of the query (case-insensitive).
func searchSettings(all []settingsHit, q string) []settingsHit {
	words := strings.Fields(strings.ToLower(q))
	var res []settingsHit
	for _, h := range all {
		text := strings.ToLower(h.title + " " + h.words)
		ok := true
		for _, w := range words {
			if !strings.Contains(text, w) {
				ok = false
				break
			}
		}
		if ok {
			res = append(res, h)
		}
	}
	return res
}

// focused reports whether a row is the one a search hit leads to, still
// tinted, and whether to scroll it into view in this frame.
func (a *App) focused(title string) (tint, scroll bool) {
	f := &a.focus
	if f.title == "" || f.title != title {
		return false, false
	}
	scroll = !f.scrolled
	f.scrolled = true
	return time.Now().Before(f.until), scroll
}

// settingsPageView draws the chosen section.
func (a *App) settingsPageView(c *ui.Context) {
	r := &rows{hit: a.focused}
	switch a.settingsAt {
	case setBackups:
		a.backupsSettings(c, r)
	case setContainers:
		a.containersSettings(c, r)
	case setAbout:
		a.aboutSettings(c, r)
	default:
		a.generalSettings(c, r)
	}
}

// generalSettings: appearance, sign-in and the close button.
func (a *App) generalSettings(c *ui.Context, r *rows) {
	s := &a.settings
	pageColumn(c, "set-general", func() {
		pageIntro(c, L.SetGeneral(), L.SetGeneralAbout())
		r.section(c, "look", L.SetGroupLook(), nil, func(r *rows) {
			r.row(c, "theme", iconSunMoon, L.SetTheme(), L.SetThemeHelp(), func() {
				modes := []string{"system", "light", "dark"}
				s.themeIdx = max(slices.Index(modes, s.prefs.Theme), 0)
				ui.Segmented(c.Key("theme-seg"), &s.themeIdx, L.SetThemeSystem(), L.SetThemeLight(), L.SetThemeDark()).Label(L.SetTheme()).OnChange(func() {
					a.setTheme(modes[s.themeIdx])
				})
			})
			r.row(c, "language", iconGlobe, L.SetLanguage(), L.SetLanguageHelp(), func() {
				labeledSelect(c, "language-sel", &s.prefs.Language, []string{"", "en", "zh-Hans"}, languageLabel).Label(L.SetLanguage()).OnChange(func() {
					a.host.SetLanguage(s.prefs.Language)
					a.savePrefs()
				})
			})
		})
		r.section(c, "start", L.SetGroupStartClose(), nil, func(r *rows) {
			r.row(c, "login", iconPower, L.SetOpenAtLogin(), L.SetOpenAtLoginHelp(), func() {
				ui.Switch(c.Key("login-sw"), &s.openAtLogin).Label(L.SetOpenAtLogin()).OnChange(func() {
					if err := a.host.SetOpenAtLogin(s.openAtLogin); err != nil {
						a.notify(friendlyError(err), true)
					}
				})
			})
			r.row(c, "close", iconTray, L.SetCloseWindow(), L.SetCloseWindowHelp(), func() {
				s.closeIdx = 0
				if s.prefs.CloseQuits {
					s.closeIdx = 1
				}
				ui.Segmented(c.Key("close-seg"), &s.closeIdx, L.SetCloseToTray(), L.SetCloseQuit()).Label(L.SetCloseWindow()).OnChange(func() {
					s.prefs.CloseQuits = s.closeIdx == 1
					a.savePrefs()
				})
			})
		})
	})
}

// backupsSettings: the background agent, the backup folder, the helper
// image.
func (a *App) backupsSettings(c *ui.Context, r *rows) {
	s := &a.settings
	pageColumn(c, "set-backups", func() {
		pageIntro(c, L.SetBackupsAgent(), L.SetBackupsAbout())
		r.section(c, "agent", L.SetAgent(), nil, func(r *rows) {
			about := L.SetAgentHelp()
			k := toneNeutral
			if s.agentChecked && !s.agentInstalled {
				about, k = L.BkAgentMissing(), toneWarn
			}
			r.rowTone(c, "agent", iconClock, L.SetAgent(), about, k, func() {
				switch {
				case !s.agentChecked:
					ui.Spinner(c)
				case s.agentInstalled:
					pill(c, L.SetAgentOn(), toneGood)
					if ui.Button(c.Key("agent-off"), L.SetUninstallAgent()).Clicked() {
						a.async(func(ctx context.Context) func() {
							scheduler.Uninstall(ctx)
							return a.checkAgent
						})
					}
				default:
					if ui.PrimaryButton(c.Key("agent-on"), L.SetInstallAgent()).Clicked() {
						a.installAgent()
					}
				}
			})
		})
		r.section(c, "where", L.SetGroupStorage(), nil, func(r *rows) {
			r.row(c, "dir", iconFolder, L.SetBackupDir(), L.SetBackupDirHelp(), nil)
			r.block(c, "dir-in", func() {
				ui.Row(c).FillWidth().Gap(8).Children(func() {
					ui.TextInput(c.Key("dir-text"), &s.backupDir).Placeholder(filepath.Join(os.Getenv("USERPROFILE"), "WSLPilot Backups")).Label(L.SetBackupDir()).Grow(1)
					if ui.Button(c.Key("dir-save"), L.Save()).Disabled(s.backupDir == s.prefs.BackupDir).Clicked() {
						s.prefs.BackupDir = strings.TrimSpace(s.backupDir)
						a.savePrefs()
						a.bk.loaded = false
					}
				})
			})
			r.row(c, "helper", iconImage, L.SetHelperImage(), L.SetHelperImageHelp(), nil)
			r.block(c, "helper-in", func() {
				ui.Row(c).FillWidth().Gap(8).Children(func() {
					ui.TextInput(c.Key("helper-text"), &s.helperImage).Placeholder(volume.DefaultHelperImage).Label(L.SetHelperImage()).Grow(1)
					if ui.Button(c.Key("helper-save"), L.Save()).Disabled(s.helperImage == s.prefs.HelperImage).Clicked() {
						s.prefs.HelperImage = strings.TrimSpace(s.helperImage)
						a.k.ApplyPrefs(s.prefs)
						a.savePrefs()
					}
				})
			})
		})
	})
}

// containersSettings: the settings.yaml form, moving the data, restarting
// the session.
func (a *App) containersSettings(c *ui.Context, r *rows) {
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
	pageColumn(c, "set-wslc", func() {
		pageIntro(c, L.SetWSLc(), L.SetWSLcAbout())
		r.section(c, "data", L.SetGroupData(), nil, func(r *rows) {
			r.row(c, "move", iconHardDrive, L.SetMoveData(), L.SetMoveDataHelp(), func() {
				if ui.Button(c.Key("move-btn"), L.SetMoveData()).Clicked() {
					a.settings.moveTo, a.settings.moving = "", true
				}
			})
			r.row(c, "restart", iconRestart, L.SetRestartSession(), L.SetRestartSessionMessage(), func() {
				if ui.Button(c.Key("restart-btn"), L.SetRestartSession()).Clicked() {
					a.ask(L.SetRestartSession(), L.SetRestartSessionMessage(), L.SetRestartSession(), func(bool) {
						a.wslcJob(L.SetRestartSession(), "wslc.session.terminate", a.k.WSLc.TerminateSession)
					})
				}
			})
		})
		small(c, L.SetWSLcFile()).Bold().Padding(20, 4, 8, 4)
		f.formRows(c, a, path, L.CfgEffectSession(), func(ctx context.Context, text string) error {
			_, err := f.snap.Save(text)
			return err
		}, reload)
	})
	a.moveDataDialog(c, path)
}

// moveDataDialog asks where WSL Containers data should go and moves it.
func (a *App) moveDataDialog(c *ui.Context, settingsPath string) {
	s := &a.settings
	if !s.moving {
		return
	}
	ui.Modal(c, &s.moving, func() {
		ui.Column(c).Width(dialogWidth).Gap(gap).Children(func() {
			ui.Text(c, L.SetMoveData()).FontSize(16).Bold()
			callout(c, "move-warn", toneWarn, iconAlert, L.SetMoveDataMessage(), nil)
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

// aboutSettings: the version and source, licenses, updates, diagnostics.
func (a *App) aboutSettings(c *ui.Context, r *rows) {
	pageColumn(c, "set-about", func() {
		pageIntro(c, L.SetAboutUpdates(), L.SetAboutAbout())
		r.section(c, "app", L.AppName(), nil, func(r *rows) {
			r.row(c, "version", iconWSL, L.SetVersion(appVersion()), L.SetAboutText(), func() {
				if ui.Button(c.Key("src"), L.SetSource()).Clicked() {
					a.host.OpenURL("https://github.com/YuanZhe-99/WSLPilot")
				}
			})
			r.row(c, "license", iconFile, L.SetLicenses(), L.SetLicensesHelp(), func() {
				if ui.Button(c.Key("lic"), L.SetShow()).Clicked() {
					a.host.OpenURL("https://github.com/YuanZhe-99/WSLPilot/blob/main/THIRD_PARTY_NOTICES.md")
				}
			})
		})
		r.section(c, "updates", L.SetUpdates(), nil, func(r *rows) { a.updateRows(c, r) })
		r.section(c, "diag", L.SetDiagnostics(), nil, func(r *rows) {
			r.row(c, "data", iconFolder, L.SetData(), L.SetDataHelp(), func() {
				if ui.Button(c.Key("data-open"), L.ShowInExplorer()).Clicked() {
					a.host.OpenPath(a.k.Store.Dir)
				}
			})
		})
	})
}

// languageLabel names an interface language in itself, so a person who
// cannot read the current one still finds theirs.
func languageLabel(tag string) string {
	switch tag {
	case "en":
		return "English"
	case "zh-Hans":
		return "简体中文"
	}
	return L.SetLanguageSystem()
}
