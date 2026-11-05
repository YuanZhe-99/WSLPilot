// Package wslc adapts wslc.exe, the WSL Containers CLI: it turns commands
// into Go types and back. See doc/en-us/upstream.md for the verified command
// contract and quirks.
//
// M0 implements detection only: `wslc info --format json` and the probe for
// a native Compose command.
package wslc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/YuanZhe-99/WSLPilot/internal/procexec"
)

// Exe is the program WSLPilot runs for WSL Containers.
const Exe = "wslc.exe"

// Info is the part of `wslc info --format json` WSLPilot uses.
type Info struct {
	Client struct {
		Version        string `json:"Version"`
		KernelVersion  string `json:"KernelVersion"`
		WindowsVersion string `json:"WindowsVersion"`
		SettingsFile   string `json:"SettingsFile"`
	} `json:"Client"`
	Server struct {
		SessionManagerVersion string    `json:"SessionManagerVersion"`
		Sessions              []Session `json:"Sessions"`
	} `json:"Server"`
}

// Session is a WSLc session listed by `wslc info`.
type Session struct {
	ID         int    `json:"ID"`
	Name       string `json:"Name"`
	CreatorPid int    `json:"CreatorPid"`
}

// ParseInfo decodes the output of `wslc info --format json`.
func ParseInfo(out string) (Info, error) {
	var info Info
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return Info{}, fmt.Errorf("wslc info: %w", err)
	}
	if info.Client.Version == "" {
		return Info{}, errors.New("wslc info: no client version")
	}
	return info, nil
}

// Client runs wslc.exe through a Runner.
type Client struct {
	Run procexec.Runner
}

// New returns a Client that runs the real wslc.exe.
func New() *Client { return &Client{Run: procexec.Run} }

// Info runs `wslc info --format json`.
//
// Returns: an exec error when wslc.exe is not installed (WSL before the
// WSL Containers release).
// Side effects: wslc starts its session manager, and a CLI session, if none
// runs.
func (c *Client) Info(ctx context.Context) (Info, error) {
	out, err := c.Run(ctx, Exe, "info", "--format", "json")
	if err != nil {
		return Info{}, err
	}
	return ParseInfo(out)
}

// HasNativeCompose reports whether this wslc has a `compose` command.
//
// Notes: wslc exits 1 for an unknown command (verified on 3.0.3.0), so a
// zero exit of `wslc compose --help` means the command exists. The result
// only informs the user; it never switches a project's backend. Never probe
// with `wsl.exe compose`: wsl.exe runs unknown words as a Linux command in
// the default distribution.
func (c *Client) HasNativeCompose(ctx context.Context) (bool, error) {
	_, err := c.Run(ctx, Exe, "compose", "--help")
	if err == nil {
		return true, nil
	}
	var ee *procexec.ExitError
	if errors.As(err, &ee) {
		return false, nil
	}
	return false, err
}
