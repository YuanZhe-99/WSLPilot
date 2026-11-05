// Package vhdx compacts virtual disks that WSL itself cannot compact: a
// distribution on a WSL release without `wsl --manage --compact`, and the
// VHDX files of WSLc vhd volumes. It drives diskpart in a separate,
// elevated child process; the GUI itself never runs elevated.
package vhdx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Script returns the diskpart script that compacts path.
// Notes: the disk is attached read-only, the mode diskpart's compact
// requires; the path is quoted, and a path containing a double quote is
// rejected because diskpart has no escape for it.
func Script(path string) (string, error) {
	if strings.ContainsAny(path, "\"\r\n") {
		return "", fmt.Errorf("vhdx: unsupported character in %q", path)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("vhdx: %q is not an absolute path", path)
	}
	return fmt.Sprintf("select vdisk file=\"%s\"\r\nattach vdisk readonly\r\ncompact vdisk\r\ndetach vdisk\r\nexit\r\n", path), nil
}

// Elevator runs a program elevated (UAC prompt) and waits for it.
// Returns: the exit code, or an error when the user declined the prompt.
type Elevator func(ctx context.Context, program, params string) (int, error)

// ErrInUse means the disk is open by another process (a running
// distribution or WSLc session).
var ErrInUse = errors.New("vhdx: the disk is in use; stop WSL first")

// Result is the outcome of a compaction.
type Result struct {
	Before, After int64
}

// Compact runs diskpart elevated on path and returns the sizes before and
// after.
// Side effects: writes the script and diskpart's log next to each other in
// a temporary folder; shows a UAC prompt.
// Notes: the caller must have stopped everything using the disk; Compact
// checks only that the file can be opened exclusively.
func Compact(ctx context.Context, elevate Elevator, path string, log func(string)) (Result, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	if err := checkExclusive(path); err != nil {
		return Result{}, err
	}
	script, err := Script(path)
	if err != nil {
		return Result{}, err
	}
	dir, err := os.MkdirTemp("", "wslpilot-compact-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(dir)
	sp := filepath.Join(dir, "compact.txt")
	if err := os.WriteFile(sp, []byte(script), 0o600); err != nil {
		return Result{}, err
	}
	// diskpart writes to a console the elevated process owns; cmd
	// redirects it to a file this process can read afterwards.
	out := filepath.Join(dir, "diskpart.log")
	// cmd.exe rule: with /s, the first and last quote of the command are
	// removed and the rest is run as typed, inner quotes included.
	code, err := elevate(ctx, "cmd.exe", CmdLine(sp, out))
	if b, rerr := os.ReadFile(out); rerr == nil && log != nil {
		log(string(b))
	}
	if err != nil {
		return Result{}, err
	}
	if code != 0 {
		return Result{}, fmt.Errorf("diskpart exited with code %d", code)
	}
	after, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	return Result{Before: st.Size(), After: after.Size()}, nil
}

// checkExclusive opens the file for writing to find a disk that is still
// attached.
func checkExclusive(path string) error {
	f, err := openExclusive(path)
	if err != nil {
		return ErrInUse
	}
	return f.Close()
}

// CmdLine is the cmd.exe parameter string that runs diskpart on script and
// writes its output to log.
// Notes: cmd.exe does not understand C-runtime escaping (\"); with /s it
// strips the outer pair of quotes and runs the rest as typed.
func CmdLine(script, log string) string {
	return `/d /s /c ""diskpart.exe" /s "` + script + `" > "` + log + `" 2>&1"`
}
