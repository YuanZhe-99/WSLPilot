//go:build !windows

package vhdx

import "os"

// openExclusive opens path for writing (no exclusive mode off Windows).
func openExclusive(path string) (*os.File, error) { return os.OpenFile(path, os.O_RDWR, 0) }
