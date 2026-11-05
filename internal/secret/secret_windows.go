//go:build windows

// Package secret protects small secrets (backup repository passwords) for
// storage with Windows DPAPI in the current user's scope: only this user,
// on this PC, can read them back. Nothing is ever stored in plain text.
package secret

import (
	"encoding/base64"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// entropy ties protected values to WSLPilot.
var entropy = []byte("WSLPilot secret v1")

// cryptprotectUIForbidden makes DPAPI fail instead of showing a prompt.
const cryptprotectUIForbidden = 0x1

// Protect encrypts s for the current user and returns it as base64.
func Protect(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	in := blob([]byte(s))
	ent := blob(entropy)
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, &ent, 0, nil, cryptprotectUIForbidden, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, out.Size)), nil
}

// Unprotect reverses Protect.
// Returns: an error when the value was protected by another user or PC.
func Unprotect(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(p)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", errors.New("secret: empty value")
	}
	in := blob(raw)
	ent := blob(entropy)
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, &ent, 0, nil, cryptprotectUIForbidden, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}

func blob(b []byte) windows.DataBlob {
	return windows.DataBlob{Size: uint32(len(b)), Data: unsafe.SliceData(b)}
}
