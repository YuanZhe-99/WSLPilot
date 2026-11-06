package gui

import (
	"context"
	"strconv"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/YuanZhe-99/WSLPilot/internal/configdoc"
	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// confForm edits one configuration file: common settings as a form, the
// raw file under Advanced. It never applies an invalid draft and saves only
// the changed lines (configdoc).
type confForm struct {
	fields []configdoc.Field
	yaml   bool // settings.yaml; INI otherwise

	loaded  bool
	loading bool
	err     error
	snap    configdoc.Snapshot

	values map[string]*string // bound form value per field path
	// (formChoice() or "" = not set)
	orig map[string]string

	advanced bool
	raw      *editor.State

	saving  bool
	saveErr string
	note    string
	bools   map[string]*bool // the switches of Bool fields
}

// formChoice is the select label meaning "not set: WSL's default".
func formChoice() string { return L.CfgDefault() }

// reset fills the form from a freshly read file.
func (f *confForm) reset(snap configdoc.Snapshot) {
	f.snap, f.loaded, f.loading, f.err = snap, true, false, nil
	f.values, f.orig = map[string]*string{}, map[string]string{}
	for _, fd := range f.fields {
		v, ok := f.get(fd)
		if ok {
			v = fd.Decode(v)
			if v == "default" && f.yaml {
				v = ""
			}
		}
		f.orig[fd.Path()] = v
		shown := v
		if shown == "" && (fd.Kind == configdoc.Bool || fd.Kind == configdoc.Enum) {
			shown = formChoice()
		}
		f.values[fd.Path()] = &shown
	}
	f.raw = &editor.State{Buffer: editor.NewBuffer(snap.Text)}
	if f.yaml {
		f.raw.Buffer.SetLanguage(editor.LanguageFor("settings.yaml"))
	} else {
		f.raw.Buffer.SetLanguage(editor.LanguageFor("wsl.ini"))
	}
	f.saveErr = ""
	f.bools = map[string]*bool{}
	for _, fd := range f.fields {
		if fd.Kind == configdoc.Bool {
			on := f.effective(fd) == "true"
			f.bools[fd.Path()] = &on
		}
	}
}

func (f *confForm) get(fd configdoc.Field) (string, bool) {
	if f.yaml {
		return configdoc.ParseYAML(f.snap.Text).Get(fd.Path())
	}
	return configdoc.ParseINI(f.snap.Text).Get(fd.Section, fd.Key)
}

// changes lists the fields whose value differs from the file.
func (f *confForm) changes() []configdoc.Change {
	var cs []configdoc.Change
	for _, fd := range f.fields {
		p := fd.Path()
		if v := f.value(p); v != f.orig[p] {
			cs = append(cs, configdoc.Change{Field: fd, Old: f.orig[p], New: v})
		}
	}
	return cs
}

// value returns the normalized form value of a field ("" = not set).
func (f *confForm) value(p string) string {
	v := strings.TrimSpace(*f.values[p])
	if v == formChoice() {
		return ""
	}
	return v
}

// buildDraft applies the form (or the raw editor) to the file text.
func (f *confForm) buildDraft() (string, error) {
	if f.advanced {
		text := f.raw.Buffer.String()
		if f.yaml {
			if err := configdoc.ParseYAML(text).Validate(); err != nil {
				return "", err
			}
		} else if bad := configdoc.ParseINI(text).Problems(); len(bad) > 0 {
			return "", &lineProblems{lines: bad}
		}
		return text, nil
	}
	if f.yaml {
		d := configdoc.ParseYAML(f.snap.Text)
		if err := configdoc.ApplyYAML(d, f.changes()); err != nil {
			return "", err
		}
		return d.String(), nil
	}
	d := configdoc.ParseINI(f.snap.Text)
	if err := configdoc.ApplyINI(d, f.changes()); err != nil {
		return "", err
	}
	return d.String(), nil
}

// lineProblems reports INI lines that are not key=value.
type lineProblems struct{ lines []int }

func (p *lineProblems) Error() string {
	s := make([]string, len(p.lines))
	for i, l := range p.lines {
		s[i] = itoa(l)
	}
	return L.CfgBadLines(strings.Join(s, ", "))
}

// unknownKeys lists keys in an INI file that the form does not show.
func (f *confForm) unknownKeys() []string {
	if f.yaml {
		return nil
	}
	var ks []string
	for _, e := range configdoc.ParseINI(f.snap.Text).Entries() {
		if !configdoc.Known(f.fields, e.Section, e.Key) {
			ks = append(ks, e.Section+"."+e.Key)
		}
	}
	return ks
}

// label returns a field's localized label and help.
func fieldText(fd configdoc.Field) (label, help string) {
	loc := i18n.Locale()
	label = L.Catalog.Format(loc, "Cfg"+fd.ID, nil)
	help = L.Catalog.Format(loc, "Cfg"+fd.ID+"Help", nil)
	return
}

// effective is the value a field has: the one in the file or the form,
// else its documented default.
func (f *confForm) effective(fd configdoc.Field) string {
	if v := f.value(fd.Path()); v != "" {
		return v
	}
	return fd.Default
}

// groupName names an INI section or YAML parent for people.
func groupName(sec string) string {
	switch sec {
	case "boot":
		return L.CfgGroupBoot()
	case "user":
		return L.CfgGroupUser()
	case "network":
		return L.CfgGroupNetwork()
	case "interop", "automount":
		return L.CfgGroupWindows()
	case "time", "gpu":
		return L.CfgGroupSystem()
	case "wsl2":
		return L.CfgGroupVM()
	case "general":
		return L.CfgGroupGeneral()
	case "experimental":
		return L.CfgGroupExperimental()
	case "session":
		return L.CfgGroupSession()
	}
	return L.CfgGroupOther()
}

// fieldSections draws the fields as row sections grouped as groupName
// does; a changed field shows a "Changed" pill.
func (f *confForm) fieldSections(c *ui.Context, r *rows) {
	type group struct {
		name   string
		fields []configdoc.Field
	}
	var gs []group
	for _, fd := range f.fields {
		n := groupName(fd.Section)
		if len(gs) == 0 || gs[len(gs)-1].name != n {
			gs = append(gs, group{name: n})
		}
		gs[len(gs)-1].fields = append(gs[len(gs)-1].fields, fd)
	}
	for gi, g := range gs {
		r.section(c, "grp-"+itoa(gi), g.name, nil, func(r *rows) {
			for _, fd := range g.fields {
				label, help := fieldText(fd)
				if fd.Default != "" && fd.Kind != configdoc.Bool {
					help += " " + L.CfgDefaultIs(fd.Default)
				}
				k := toneNeutral
				if err := fd.Validate(f.value(fd.Path())); err != nil {
					help, k = L.CfgInvalid(), toneBad
				}
				r.rowTone(c, "f-"+fd.Path(), nil, label, help, k, func() { f.control(c, fd, label) })
			}
		})
	}
}

// control is a field's input, preceded by a "Changed" pill.
func (f *confForm) control(c *ui.Context, fd configdoc.Field, label string) {
	p := fd.Path()
	if f.value(p) != f.orig[p] {
		pill(c, L.CfgChanged(), toneWarn)
	}
	switch fd.Kind {
	case configdoc.Bool:
		on := f.bools[p]
		ui.Switch(c.Key("sw-"+p), on).Label(label).OnChange(func() {
			v := strconv.FormatBool(*on)
			if f.orig[p] == "" && v == fd.Default {
				v = "" // back to "not set"
			}
			*f.values[p] = v
		})
	case configdoc.Enum:
		opts := append([]string{formChoice()}, fd.Choices...)
		ui.Select(c.Key("sel-"+p), f.values[p], opts).Label(label).Width(200)
	default:
		in := ui.TextInput(c.Key("in-"+p), f.values[p]).Label(label).Width(rowControlW)
		if fd.Default != "" {
			in.Placeholder(fd.Default)
		}
	}
}

// changesCard is the "changes to save" card: the lines that change, what
// it takes to apply them, and Save (and an extra action, as "Save and
// restart"). It shows a sentence instead when nothing changed.
func (f *confForm) changesCard(c *ui.Context, a *App, effect string, save func(ctx context.Context, text string) error, reload func(), extra string, after func()) {
	t := c.Theme()
	draft, err := f.buildDraft()
	var diff []string
	if err == nil {
		diff = configdoc.DiffLines(f.snap.Text, draft)
	}
	card(c, "", func() {
		switch {
		case err != nil:
			cardHead(c, L.CfgCannotSave(), nil)
			errorText(c, friendlyError(err))
		case len(diff) == 0:
			cardHead(c, L.CfgNoChanges(), nil)
			small(c, L.CfgBackupNote())
		default:
			cardHead(c, L.CfgChangesToSave(len(diff)), nil)
			ui.Column(c).FillWidth().Padding(10, 12).Radius(8).Background(t.Surface).Gap(2).Children(func() {
				for i, l := range diff {
					col := t.Success
					if strings.HasPrefix(l, "-") {
						col = t.Danger
					}
					ui.Text(c.Key(i), l).Font("monospace").FontSize(12).TextColor(col).Selectable()
				}
			})
			small(c, L.CfgBackupNote())
		}
		if !f.snap.Exists {
			small(c, L.CfgMissing())
		}
		if effect != "" {
			ui.Text(c, effect).FontSize(sizeSmall)
		}
		if f.saveErr != "" {
			errorText(c, f.saveErr)
		}
		if f.note != "" {
			ui.Text(c, f.note).TextColor(t.Success)
		}
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			ui.Button(c.Key("reload"), L.CfgReload()).OnClick(reload)
			ok := err == nil && len(diff) > 0 && !f.saving
			doSave := func(then func()) {
				f.saving, f.saveErr, f.note = true, "", ""
				text := draft
				a.async(func(ctx context.Context) func() {
					serr := save(ctx, text)
					return func() {
						f.saving = false
						switch {
						case serr == configdoc.ErrConflict:
							f.saveErr = L.CfgConflict()
						case serr != nil:
							f.saveErr = friendlyError(serr)
						default:
							f.note = L.CfgSaved()
							reload()
							if then != nil {
								then()
							}
						}
					}
				})
			}
			if extra != "" {
				if ui.Button(c.Key("save-plain"), L.CfgSave()).Disabled(!ok).Clicked() {
					doSave(nil)
				}
				if ui.PrimaryButton(c.Key("save-extra"), extra).Disabled(!ok).Clicked() {
					doSave(after)
				}
			} else if ui.PrimaryButton(c.Key("save"), L.CfgSave()).Disabled(!ok).Clicked() {
				doSave(nil)
			}
		})
	})
}

// advancedCard offers the raw file: other keys kept as they are, and the
// editor (only changed lines are written; the file is backed up first).
func (f *confForm) advancedCard(c *ui.Context, path string) {
	card(c, L.CfgAdvancedTitle(), func() {
		if ks := f.unknownKeys(); len(ks) > 0 {
			small(c, L.CfgOtherKeys(strings.Join(ks, ", ")))
		}
		small(c, path).Selectable()
		label := L.CfgEditRaw()
		if f.advanced {
			label = L.CfgBackToForm()
		}
		if ui.Button(c.Key("raw-toggle"), label).Clicked() {
			if !f.advanced {
				text := f.form2text()
				f.raw.Buffer = editor.NewBuffer(text)
				if f.yaml {
					f.raw.Buffer.SetLanguage(editor.LanguageFor("settings.yaml"))
				} else {
					f.raw.Buffer.SetLanguage(editor.LanguageFor("wsl.ini"))
				}
			}
			f.advanced = !f.advanced
		}
	})
}

// form2text is the file as the form would write it (the editor starts
// there, so form edits are not lost).
func (f *confForm) form2text() string {
	adv := f.advanced
	f.advanced = false
	text, err := f.buildDraft()
	f.advanced = adv
	if err != nil {
		return f.snap.Text
	}
	return text
}

// loadingOr draws the load error or a spinner until the form has loaded.
// Returns: whether the form is loaded.
func (f *confForm) loadingOr(c *ui.Context, reload func()) bool {
	if f.loaded {
		return true
	}
	if f.err != nil {
		errorText(c, friendlyError(f.err))
		ui.Button(c.Key("retry"), L.Retry()).OnClick(reload)
	} else {
		ui.Spinner(c)
	}
	return false
}

// formRows is the whole form in one column (the Settings sheet): fields
// (or the raw editor), the changes card, the advanced card.
func (f *confForm) formRows(c *ui.Context, a *App, path, effect string, save func(ctx context.Context, text string) error, reload func()) {
	if !f.loadingOr(c, reload) {
		return
	}
	if f.advanced {
		editor.View(c, f.raw).FillWidth().Height(editorHeight)
	} else {
		f.fieldSections(c, &rows{})
	}
	ui.Box(c).Height(14)
	f.changesCard(c, a, effect, save, reload, "", nil)
	ui.Box(c).Height(14)
	f.advancedCard(c, path)
}

// formSplit is the form with the changes and advanced cards beside it (the
// distribution and WSL settings pages, as in the design).
func (f *confForm) formSplit(c *ui.Context, a *App, path, effect string, save func(ctx context.Context, text string) error, reload func(), extra string, after func()) {
	if !f.loaded {
		body(c, func() { f.loadingOr(c, reload) })
		return
	}
	ui.Row(c.Key("form-split")).Grow(1).FillWidth().MinHeight(0).Padding(0, panePadX, 0, panePadX).Gap(20).AlignItems(ui.Start).Children(func() {
		ui.Scroll(c.Key("form-left")).Grow(1).MinWidth(0).FillHeight().Padding(0, 0, panePadX, 0).Children(func() {
			if f.advanced {
				editor.View(c, f.raw).FillWidth().Height(520)
			} else {
				f.fieldSections(c, &rows{})
			}
		})
		ui.Column(c.Key("form-right")).Width(detailWidth-20).Shrink(0).Gap(14).Padding(20, 0, 0, 0).Children(func() {
			f.changesCard(c, a, effect, save, reload, extra, after)
			f.advancedCard(c, path)
		})
	})
}
