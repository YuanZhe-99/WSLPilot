//go:build e2e

package main

// Builds tagged e2e link MyGo's automation plugin, so an agent or a test
// runner can read and drive the window (doc/en-us/development.md,
// "Driving the window"). It listens only when MYGO_AUTOMATION names a
// socket; release builds leave it out.
import _ "github.com/egoist/mygo/plugins/automation"
