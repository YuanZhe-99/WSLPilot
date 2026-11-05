//go:build !windows

package procexec

import "os/exec"

// hideWindow does nothing off Windows; WSLPilot only runs on Windows, but the
// packages build elsewhere so CI and tooling can type-check them.
func hideWindow(*exec.Cmd) {}
