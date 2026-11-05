package procexec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
)

// Spec describes a streaming child process.
type Spec struct {
	// Name is the program, resolved on PATH; Args its argument vector.
	Name string
	Args []string
	// Stdin, when set, is copied to the child's standard input; the pipe
	// is closed when it reaches EOF.
	Stdin io.Reader
	// Stdout receives the raw bytes the child writes (no decoding: tar
	// streams and logs pass through unchanged). Nil discards them.
	Stdout io.Writer
	// Stderr receives the child's standard error as it is written. The
	// last 64 KiB is also kept for the ExitError.
	Stderr io.Writer
	// Env adds variables to the inherited environment (after WSL_UTF8=1).
	Env []string
}

// Streamer runs a Spec to completion. Adapters hold one so tests can
// substitute recorded streams.
type Streamer func(ctx context.Context, s Spec) error

// Stream is the real Streamer.
//
// Returns: nil on exit code 0; an *ExitError carrying the tail of stderr
// otherwise; ctx.Err() when the context ended the process.
// Side effects: starts a hidden child process with WSL_UTF8=1. Cancelling
// ctx kills it.
func Stream(ctx context.Context, s Spec) error {
	cmd := exec.CommandContext(ctx, s.Name, s.Args...)
	cmd.Env = append(append(os.Environ(), "WSL_UTF8=1"), s.Env...)
	hideWindow(cmd)
	cmd.Stdin = s.Stdin
	if s.Stdout != nil {
		cmd.Stdout = s.Stdout
	}
	tail := &tailBuffer{max: 64 << 10}
	if s.Stderr != nil {
		cmd.Stderr = io.MultiWriter(s.Stderr, tail)
	} else {
		cmd.Stderr = tail
	}
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return &ExitError{Name: s.Name, Args: s.Args, Code: ee.ExitCode(), Stderr: Decode(tail.Bytes())}
		}
		return err
	}
	return nil
}

// RunnerFrom adapts a Streamer to a Runner: stdout is buffered and decoded.
// Tests use it to serve both from one fake.
func RunnerFrom(st Streamer) Runner {
	return func(ctx context.Context, name string, args ...string) (string, error) {
		var out bytes.Buffer
		err := st(ctx, Spec{Name: name, Args: args, Stdout: &out})
		return Decode(out.Bytes()), err
	}
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	buf []byte
	max int
}

// Write appends p, dropping the oldest bytes beyond max.
func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.max:]...)
	}
	return len(p), nil
}

// Bytes returns the kept bytes.
func (t *tailBuffer) Bytes() []byte { return t.buf }
