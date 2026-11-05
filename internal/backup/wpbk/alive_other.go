//go:build !windows

package wpbk

import (
	"os"
	"syscall"
)

// processAlive reports whether a process with pid is running on this host.
// Notes: signal 0 probes existence; a permission error means it exists.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}
