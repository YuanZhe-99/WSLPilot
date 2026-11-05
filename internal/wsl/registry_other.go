//go:build !windows

package wsl

// readLxss has no registry to read off Windows.
func readLxss() []RegEntry { return nil }
