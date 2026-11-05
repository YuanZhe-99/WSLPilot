//go:build windows

package vhdx

import (
	"os"

	"golang.org/x/sys/windows"
)

// openExclusive opens path with no sharing, which fails while Hyper-V or
// WSL has the disk attached.
func openExclusive(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}
