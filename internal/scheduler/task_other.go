//go:build !windows

package scheduler

import (
	"context"
	"errors"
	"os"
	"syscall"
)

// TaskName is the per-user Task Scheduler task that runs due plans.
const TaskName = `WSLPilot\RunDue`

// Install is only supported on Windows.
func Install(ctx context.Context, agent string) error {
	return errors.New("scheduler: Task Scheduler is only available on Windows")
}

// Uninstall does nothing off Windows.
func Uninstall(ctx context.Context) error { return nil }

// Installed is false off Windows.
func Installed(ctx context.Context) bool { return false }

func alive(p *os.Process) bool { return p.Signal(syscall.Signal(0)) == nil }
