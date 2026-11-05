//go:build !windows

// Package secret protects small secrets for storage; only Windows (DPAPI)
// is supported.
package secret

import "errors"

// Protect is unsupported off Windows.
func Protect(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return "", errors.New("secret: only supported on Windows")
}

// Unprotect is unsupported off Windows.
func Unprotect(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	return "", errors.New("secret: only supported on Windows")
}
