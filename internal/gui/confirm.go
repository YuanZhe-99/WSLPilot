package gui

import (
	"github.com/egoist/mygo/ui"

	L "github.com/YuanZhe-99/WSLPilot/locales"
)

// confirmState is the one confirmation dialog the app shows for danger
// actions (behavior contract: confirm, offer a backup first, log).
type confirmState struct {
	open    bool
	title   string
	message string
	action  string
	// typeName, when set, must be typed to enable the action (unregister,
	// deleting a volume with data).
	typeName string
	typed    string
	// offerBackup shows "Back up first", on by default.
	offerBackup bool
	backup      bool
	// extra is an optional second choice (e.g. "Back up live").
	onOK func(backupFirst bool)
}

// ask opens the confirmation dialog.
func (a *App) ask(title, message, action string, onOK func(backupFirst bool)) {
	a.confirm = confirmState{open: true, title: title, message: message, action: action, onOK: onOK}
}

// askDanger opens it with "Back up first" (on) and, when typeName is set,
// the name to type.
func (a *App) askDanger(title, message, action, typeName string, onOK func(backupFirst bool)) {
	a.confirm = confirmState{open: true, title: title, message: message, action: action, typeName: typeName,
		offerBackup: true, backup: true, onOK: onOK}
}

// confirmDialog draws the dialog when open.
func (a *App) confirmDialog(c *ui.Context) {
	cs := &a.confirm
	if !cs.open {
		return
	}
	ui.Modal(c, &cs.open, func() {
		ui.Column(c).Width(dialogWidth).Gap(gap).Children(func() {
			ui.Text(c, cs.title).FontSize(sectionSize).Bold()
			ui.Text(c, cs.message)
			if cs.offerBackup {
				ui.Checkbox(c, &cs.backup, L.ConfirmBackupFirst())
			}
			if cs.typeName != "" {
				muted(c, L.ConfirmTypeName(cs.typeName))
				ui.TextInput(c, &cs.typed).Label(L.ConfirmTypeName(cs.typeName)).AutoFocus()
			}
			ui.Row(c).Gap(smallGap).Justify(ui.End).FillWidth().Children(func() {
				ui.Button(c, L.Cancel()).OnClick(func() { cs.open = false })
				ok := cs.typeName == "" || cs.typed == cs.typeName
				dangerButton(c, cs.action).Disabled(!ok).OnClick(func() {
					cs.open = false
					if cs.onOK != nil {
						cs.onOK(cs.offerBackup && cs.backup)
					}
				})
			})
		})
	})
}
