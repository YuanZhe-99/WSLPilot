//go:build !windows

// Package elevate starts a program in a separate, elevated process. It is
// only implemented on Windows.
package elevate

import (
	"context"
	"errors"
	"strings"
)

// ErrDeclined means the user refused elevation.
var ErrDeclined = errors.New("elevate: the administrator prompt was declined")

// Run is unsupported off Windows.
func Run(ctx context.Context, program, params string) (int, error) {
	return 0, errors.New("elevate: only supported on Windows")
}

// Args joins arguments with spaces (no quoting off Windows).
func Args(args []string) string { return strings.Join(args, " ") }
