// Package upstream records the upstream versions WSLPilot was tested with.
//
// tested.json is the single machine-readable list: tools/deps compares it
// with the latest releases, and the app uses it to say when the installed
// WSL is newer than any version that passed the live tests.
package upstream

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
)

//go:embed tested.json
var testedJSON []byte

// Manifest is the content of tested.json.
type Manifest struct {
	// WSL lists the WSL release versions (GitHub tag form, e.g. "3.0.3")
	// that passed the fixtures and live tests, oldest first.
	WSL []string `json:"wsl"`
	// MyGo is the MyGo module version the release was built with.
	MyGo string `json:"mygo"`
}

// Tested returns the embedded manifest.
// Notes: the file is part of the build, so a parse error is a programming
// error and panics (a test guards it).
func Tested() Manifest {
	var m Manifest
	if err := json.Unmarshal(testedJSON, &m); err != nil {
		panic("upstream: tested.json: " + err.Error())
	}
	return m
}

// Compare orders two dotted version strings numerically ("3.0.10" > "3.0.9").
// Returns: -1, 0 or 1. A trailing ".0" is not significant, so the
// package version "3.0.3.0" equals the release tag "3.0.3". Non-numeric
// parts compare as 0.
func Compare(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(a, "v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

// NewerThanTested reports whether an installed WSL version is newer than
// every tested one.
// Inputs: version as `wsl --version` prints it (e.g. "3.0.5.0").
// Notes: an older untested version is not flagged: the parsers are
// version-grouped and fall back to the oldest fixtures.
func NewerThanTested(version string) bool {
	t := Tested().WSL
	if version == "" || len(t) == 0 {
		return false
	}
	return Compare(version, t[len(t)-1]) > 0
}
