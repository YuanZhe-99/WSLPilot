package gui

import (
	"context"
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

	reviewing bool
	preview   []string
	draft     string
	saving    bool
	saveErr   string
	note      string
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
	f.reviewing, f.saveErr = false, ""
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

// view draws the form. save is called with the new text after review; it
// runs on a goroutine and returns the error to show.
func (f *confForm) view(c *ui.Context, a *App, title, path string, effect string, save func(ctx context.Context, text string) error, reload func()) {
	t := c.Theme()
	if !f.loaded {
		if f.err != nil {
			errorText(c, friendlyError(f.err))
			ui.Button(c, L.Retry()).OnClick(reload)
		} else {
			ui.Spinner(c)
		}
		return
	}
	ui.Row(c).FillWidth().Gap(smallGap).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Children(func() {
			ui.Text(c, title).Bold()
			muted(c, path).SingleLine()
		})
		ui.Switch(c, &f.advanced).Label(L.CfgAdvanced())
		muted(c, L.CfgAdvanced())
	})
	if !f.snap.Exists {
		muted(c, L.CfgMissing())
	}
	if f.advanced {
		muted(c, L.CfgAdvancedHelp())
		editor.View(c, f.raw).FillWidth().Height(editorHeight)
	} else {
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Form(c, func() {
				section := ""
				for _, fd := range f.fields {
					if fd.Section != section {
						section = fd.Section
						if section != "" {
							ui.Text(c.Key("s-"+section), "["+section+"]").TextColor(t.TextMuted).Bold()
						}
					}
					f.fieldView(c, fd)
				}
			})
			if ks := f.unknownKeys(); len(ks) > 0 {
				muted(c, L.CfgOtherKeys(strings.Join(ks, ", ")))
			}
		})
	}
	if f.saveErr != "" {
		errorText(c, f.saveErr)
	}
	if f.note != "" {
		ui.Text(c, f.note).TextColor(t.Success)
	}
	ui.Row(c).FillWidth().Gap(smallGap).Justify(ui.End).Children(func() {
		ui.Button(c, L.CfgReload()).OnClick(reload)
		ui.PrimaryButton(c, L.CfgReview()).Disabled(f.saving).OnClick(func() {
			draft, err := f.buildDraft()
			if err != nil {
				f.saveErr = friendlyError(err)
				return
			}
			f.saveErr, f.note = "", ""
			f.draft = draft
			f.preview = configdoc.DiffLines(f.snap.Text, draft)
			f.reviewing = true
		})
	})
	if f.reviewing {
		ui.Modal(c, &f.reviewing, func() {
			ui.Column(c).Width(dialogWidth + 120).Gap(gap).Children(func() {
				ui.Text(c, L.CfgReviewTitle()).FontSize(sectionSize).Bold()
				if len(f.preview) == 0 {
					muted(c, L.CfgNoChanges())
				}
				ui.Scroll(c).MaxHeight(320).FillWidth().Children(func() {
					for i, l := range f.preview {
						col := t.Success
						if strings.HasPrefix(l, "-") {
							col = t.Danger
						}
						ui.Text(c.Key(i), l).Font("monospace").TextColor(col).Selectable()
					}
				})
				muted(c, L.CfgBackupNote())
				if effect != "" {
					ui.Text(c, effect).TextColor(t.Warning)
				}
				ui.Row(c).FillWidth().Gap(smallGap).Justify(ui.End).Children(func() {
					ui.Button(c, L.Cancel()).OnClick(func() { f.reviewing = false })
					ui.PrimaryButton(c, L.CfgSave()).Disabled(len(f.preview) == 0).OnClick(func() {
						f.reviewing, f.saving = false, true
						text := f.draft
						a.async(func(ctx context.Context) func() {
							err := save(ctx, text)
							return func() {
								f.saving = false
								if err != nil {
									f.saveErr = friendlyError(err)
									if err == configdoc.ErrConflict {
										f.saveErr = L.CfgConflict()
									}
									return
								}
								f.note = L.CfgSaved()
								reload()
							}
						})
					})
				})
			})
		})
	}
}

// fieldView draws one field.
func (f *confForm) fieldView(c *ui.Context, fd configdoc.Field) {
	label, help := fieldText(fd)
	p := fd.Path()
	val := f.value(p)
	bound := f.values[p]
	var errMsg string
	if err := fd.Validate(val); err != nil && fd.Kind != configdoc.Bool && fd.Kind != configdoc.Enum {
		errMsg = L.CfgInvalid()
	}
	desc := help
	if fd.Default != "" {
		desc = help + " " + L.CfgDefaultIs(fd.Default)
	}
	ui.Field(c.Key(p), label, func() {
		switch fd.Kind {
		case configdoc.Bool, configdoc.Enum:
			opts := []string{formChoice()}
			if fd.Kind == configdoc.Bool {
				opts = append(opts, "true", "false")
			} else {
				opts = append(opts, fd.Choices...)
			}
			ui.Select(c, bound, opts).Label(label)
		default:
			in := ui.TextInput(c, bound).Label(label)
			if fd.Default != "" {
				in.Placeholder(fd.Default)
			}
		}
	}).Description(desc).Error(errMsg)
}
