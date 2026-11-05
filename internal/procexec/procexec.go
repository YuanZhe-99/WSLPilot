// Package procexec runs the upstream command-line tools (wsl.exe, wslc.exe)
// as child processes: argument vectors only, never a shell string, with a
// context for cancellation and the output decoded to UTF-8.
//
// M0 holds the minimum the probes need; streaming, progress and job
// integration arrive in M1 (see PLAN.md §3).
package procexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf16"
)

// Runner runs a program and returns its decoded standard output. Adapters
// depend on this type so tests can substitute recorded output.
//
// Inputs: name is the program (resolved on PATH), args its argument vector.
// Returns: decoded stdout; an *ExitError when the program ran and failed.
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// ExitError reports a program that started but exited with a non-zero code.
type ExitError struct {
	Name   string
	Args   []string
	Code   int
	Stderr string
}

// Error describes the failed command for logs; the GUI shows friendlier copy.
func (e *ExitError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		return fmt.Sprintf("%s %s: exit code %d", e.Name, strings.Join(e.Args, " "), e.Code)
	}
	return fmt.Sprintf("%s %s: exit code %d: %s", e.Name, strings.Join(e.Args, " "), e.Code, msg)
}

// Run is the real Runner.
//
// Side effects: starts a child process with WSL_UTF8=1 added to the
// environment, so wsl.exe writes UTF-8 instead of UTF-16LE.
// Notes: output is still passed through Decode, because older WSL releases
// ignore WSL_UTF8.
func Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	hideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := Decode(stdout.Bytes())
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out, &ExitError{Name: name, Args: args, Code: ee.ExitCode(), Stderr: Decode(stderr.Bytes())}
		}
		return out, err
	}
	return out, nil
}

// Decode converts upstream output to UTF-8 text with LF line endings.
//
// Notes: wsl.exe writes UTF-16LE without a byte-order mark when its output
// is not a console. The heuristic treats the data as UTF-16LE when it has a
// UTF-16LE BOM, or when it has an even length and NUL in at least a third
// of its odd positions (ASCII text in UTF-16LE has NUL in every one).
func Decode(b []byte) string {
	if bytes.HasPrefix(b, []byte{0xFF, 0xFE}) {
		return normalize(decodeUTF16LE(b[2:]))
	}
	if bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
		b = b[3:]
	}
	if len(b) >= 2 && len(b)%2 == 0 {
		zeros := 0
		for i := 1; i < len(b); i += 2 {
			if b[i] == 0 {
				zeros++
			}
		}
		if zeros*3 >= len(b)/2 {
			return normalize(decodeUTF16LE(b))
		}
	}
	return normalize(string(b))
}

func decodeUTF16LE(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\x00", "")
}
