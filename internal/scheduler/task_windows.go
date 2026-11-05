//go:build windows

package scheduler

import (
	"context"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// TaskName is the per-user Task Scheduler task that runs due plans.
const TaskName = `WSLPilot\RunDue`

// runKey and runValue start `wslpilot-agent watch` at sign-in (restart
// policies, Compose autostart) without elevation.
const (
	runKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue = "WSLPilotAgent"
)

// Install registers the task (every 15 minutes, as the signed-in user,
// "Interactive only", no elevation) and the sign-in entry for the watcher.
// Inputs: agent is the full path of wslpilot-agent.exe.
// Notes: verified on Windows 11 26300 that a standard user can create the
// task with schtasks /SC MINUTE.
func Install(ctx context.Context, agent string) error {
	tr := `"` + agent + `" run-due`
	cmd := exec.CommandContext(ctx, "schtasks.exe", "/Create", "/F", "/SC", "MINUTE", "/MO", "15", "/TN", TaskName, "/TR", tr)
	hide(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return &TaskError{Out: string(out), Err: err}
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValue, `"`+agent+`" watch`)
}

// Uninstall removes the task and the sign-in entry.
func Uninstall(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "schtasks.exe", "/Delete", "/F", "/TN", TaskName)
	hide(cmd)
	cmd.Run()
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err == nil {
		k.DeleteValue(runValue)
		k.Close()
	}
	return nil
}

// Installed reports whether the task exists.
func Installed(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "schtasks.exe", "/Query", "/TN", TaskName)
	hide(cmd)
	return cmd.Run() == nil
}

func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

// alive asks Windows whether the process still runs.
func alive(p *os.Process) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p.Pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
