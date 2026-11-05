//go:build !windows

// Package elevate starts a program in a separate, elevated process. It is
// only implemented on Windows.
package elevate

import (
	"context"
	"errors"
)

// ErrDeclined means the user refused elevation.
var ErrDeclined = errors.New("elevate: the administrator prompt was declined")

// Run is unsupported off Windows.
func Run(ctx context.Context, program string, args []string) (int, error) {
	return 0, errors.New("elevate: only supported on Windows")
}
