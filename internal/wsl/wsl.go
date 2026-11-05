// Package wsl adapts wsl.exe: it turns commands into Go types and back.
// See doc/en-us/upstream.md for the verified command contract and quirks.
//
// M0 implements detection only: the version report and the list of
// distribution names.
package wsl

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/YuanZhe-99/WSLPilot/internal/procexec"
)

// Exe is the program WSLPilot runs for WSL.
const Exe = "wsl.exe"

// Version is what `wsl --version` reports. Fields are empty when the line is
// missing (older releases print fewer components).
type Version struct {
	WSL     string
	Kernel  string
	WSLg    string
	Windows string
}

// ErrNoVersion means wsl.exe ran but printed no recognizable version. The
// inbox wsl.exe of Windows without the Store/MSI WSL package behaves so.
var ErrNoVersion = errors.New("wsl: no version in output")

// versionToken matches the value after the label on each line of
// `wsl --version`; labels are localized, values are not.
var versionToken = regexp.MustCompile(`[:：]\s*([0-9][0-9A-Za-z.\-+]*)\s*$`)

// ParseVersion reads the output of `wsl --version`.
//
// Notes: labels are localized ("WSL version", "WSL 版本"), so lines are
// matched by position, which has been stable since WSL 2.0: WSL, kernel,
// WSLg, MSRDC, Direct3D, DXCore, Windows. The Windows version is the last
// line rather than the seventh, in case a component is added in between.
func ParseVersion(out string) (Version, error) {
	var values []string
	for _, line := range strings.Split(out, "\n") {
		if m := versionToken.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			values = append(values, m[1])
		}
	}
	if len(values) == 0 {
		return Version{}, ErrNoVersion
	}
	v := Version{WSL: values[0]}
	if len(values) > 1 {
		v.Kernel = values[1]
	}
	if len(values) > 2 {
		v.WSLg = values[2]
	}
	if len(values) >= 7 {
		v.Windows = values[len(values)-1]
	}
	return v, nil
}

// ParseNames reads the output of `wsl --list --quiet`: one distribution name
// per line.
func ParseNames(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// Client runs wsl.exe through a Runner.
type Client struct {
	Run procexec.Runner
}

// New returns a Client that runs the real wsl.exe.
func New() *Client { return &Client{Run: procexec.Run} }

// Version runs `wsl --version`.
//
// Returns: ErrNoVersion, an *procexec.ExitError, or an exec error when WSL is
// not installed.
func (c *Client) Version(ctx context.Context) (Version, error) {
	out, err := c.Run(ctx, Exe, "--version")
	if err != nil {
		return Version{}, err
	}
	return ParseVersion(out)
}

// Names runs `wsl --list --quiet` and returns the registered distributions.
//
// Notes: with no distribution installed wsl.exe exits non-zero and prints
// a localized hint; that is reported as an empty list.
func (c *Client) Names(ctx context.Context) ([]string, error) {
	out, err := c.Run(ctx, Exe, "--list", "--quiet")
	if err != nil {
		var ee *procexec.ExitError
		if errors.As(err, &ee) {
			return nil, nil
		}
		return nil, err
	}
	return ParseNames(out), nil
}
