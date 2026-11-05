//go:build windows

// Package elevate starts a program in a separate, elevated process through
// the UAC prompt and waits for it. WSLPilot's own processes never run
// elevated; only the child does.
package elevate

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrDeclined means the user answered No to the UAC prompt.
var ErrDeclined = errors.New("elevate: the administrator prompt was declined")

var (
	shell32         = windows.NewLazySystemDLL("shell32.dll")
	shellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

// shellExecuteInfo is SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	swHide                = 0
	errorCancelled        = 1223
)

// Run starts program with args through the "runas" verb, hidden, and waits.
// Returns: the exit code; ErrDeclined when the prompt was refused.
// Notes: cancelling ctx stops waiting but leaves the elevated child to
// finish, since this process may not terminate it.
func Run(ctx context.Context, program string, args []string) (int, error) {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(program)
	if err != nil {
		return 0, err
	}
	params, err := windows.UTF16PtrFromString(joinArgs(args))
	if err != nil {
		return 0, err
	}
	info := shellExecuteInfo{fMask: seeMaskNoCloseProcess | seeMaskNoAsync, lpVerb: verb, lpFile: file, lpParameters: params, nShow: swHide}
	info.cbSize = uint32(unsafe.Sizeof(info))
	r, _, callErr := shellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		if en, ok := callErr.(syscall.Errno); ok && en == errorCancelled {
			return 0, ErrDeclined
		}
		return 0, callErr
	}
	defer windows.CloseHandle(info.hProcess)
	done := make(chan struct{})
	go func() {
		windows.WaitForSingleObject(info.hProcess, windows.INFINITE)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return 0, err
	}
	return int(code), nil
}

// joinArgs quotes arguments the way the C runtime parses them.
func joinArgs(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = windows.EscapeArg(a)
	}
	return strings.Join(q, " ")
}
