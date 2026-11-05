//go:build windows

package procexec

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: a console child of a GUI process would
// otherwise flash a console window.
const createNoWindow = 0x08000000

// hideWindow keeps child console programs from opening a window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
