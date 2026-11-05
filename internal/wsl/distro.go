package wsl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YuanZhe-99/WSLPilot/internal/procexec"
)

// Distro is one registered distribution as WSLPilot shows it. Every field
// is read back from wsl.exe (and, for paths, from WSL's per-user registry);
// nothing is cached between reads.
type Distro struct {
	Name    string
	Default bool
	Running bool
	// Version is 1 or 2 (0 when unknown).
	Version int

	// Registry details (Windows only; empty when unavailable).
	GUID       string
	BasePath   string // folder holding the VHDX (WSL 2) or rootfs (WSL 1)
	VHDPath    string // full path of the WSL 2 disk
	Flavor     string // e.g. "debian", from the distribution's os-release
	OSVersion  string
	DefaultUID int

	// DiskBytes is the size of the VHDX file on the Windows side (0 when
	// unknown or WSL 1).
	DiskBytes int64
}

// VerboseRow is one row of `wsl --list --verbose`.
type VerboseRow struct {
	Name    string
	Default bool
	Version int
}

// ParseListVerbose reads `wsl --list --verbose`.
//
// Notes: the header and the state column are localized, so only the
// language-independent parts are read: the leading "*" marking the default,
// the name (WSL names never contain spaces) and the trailing version digit.
// The header is recognized as the first line whose last field is not a
// number.
func ParseListVerbose(out string) []VerboseRow {
	var rows []VerboseRow
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.Atoi(f[len(f)-1])
		if err != nil {
			continue // header or a localized hint
		}
		r := VerboseRow{Version: v}
		if f[0] == "*" {
			r.Default = true
			f = f[1:]
		}
		if len(f) < 2 {
			continue
		}
		r.Name = f[0]
		rows = append(rows, r)
	}
	return rows
}

// OnlineDistro is one entry of `wsl --list --online`.
type OnlineDistro struct {
	Name         string
	FriendlyName string
}

// ParseListOnline reads `wsl --list --online`.
//
// Notes: the introduction and the header are localized. The list is the
// block after the last blank line; its first line is the header, every
// following line is "<name> <friendly name>".
func ParseListOnline(out string) []OnlineDistro {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	start := 0
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			start = i + 1
		}
	}
	var res []OnlineDistro
	for i := start + 1; i < len(lines); i++ {
		f := strings.Fields(lines[i])
		if len(f) == 0 {
			continue
		}
		d := OnlineDistro{Name: f[0], FriendlyName: f[0]}
		if len(f) > 1 {
			d.FriendlyName = strings.Join(f[1:], " ")
		}
		res = append(res, d)
	}
	return res
}

// List returns every registered distribution with its state and paths.
//
// Side effects: runs `wsl --list --verbose` and `wsl --list --running
// --quiet`, reads HKCU\...\Lxss, stats VHDX files.
// Notes: no distribution at all is an empty list, not an error.
func (c *Client) List(ctx context.Context) ([]Distro, error) {
	out, err := c.Run(ctx, Exe, "--list", "--verbose")
	if err != nil {
		if isExit(err) {
			return nil, nil
		}
		return nil, err
	}
	rows := ParseListVerbose(out)
	running := map[string]bool{}
	if out, err := c.Run(ctx, Exe, "--list", "--running", "--quiet"); err == nil {
		for _, n := range ParseNames(out) {
			running[n] = true
		}
	}
	reg := c.registry()
	ds := make([]Distro, 0, len(rows))
	for _, r := range rows {
		d := Distro{Name: r.Name, Default: r.Default, Version: r.Version, Running: running[r.Name]}
		if e, ok := reg[strings.ToLower(r.Name)]; ok {
			d.GUID, d.BasePath, d.Flavor, d.OSVersion, d.DefaultUID = e.GUID, e.BasePath, e.Flavor, e.OSVersion, e.DefaultUID
			if e.VHDFileName != "" && d.Version == 2 {
				d.VHDPath = filepath.Join(e.BasePath, e.VHDFileName)
				if st, err := os.Stat(d.VHDPath); err == nil {
					d.DiskBytes = st.Size()
				}
			}
		}
		ds = append(ds, d)
	}
	return ds, nil
}

// Get returns one distribution by name (case-insensitive).
func (c *Client) Get(ctx context.Context, name string) (Distro, error) {
	ds, err := c.List(ctx)
	if err != nil {
		return Distro{}, err
	}
	for _, d := range ds {
		if strings.EqualFold(d.Name, name) {
			return d, nil
		}
	}
	return Distro{}, ErrNotFound
}

// ErrNotFound means no distribution has the requested name.
var ErrNotFound = errors.New("wsl: distribution not found")

// ListOnline returns the distributions `wsl --install` can install.
// Side effects: wsl.exe downloads the distribution manifest.
func (c *Client) ListOnline(ctx context.Context) ([]OnlineDistro, error) {
	out, err := c.Run(ctx, Exe, "--list", "--online")
	if err != nil {
		return nil, err
	}
	return ParseListOnline(out), nil
}

// RegEntry is one distribution's key under HKCU\...\Lxss.
type RegEntry struct {
	GUID, Name, BasePath, VHDFileName, Flavor, OSVersion string
	DefaultUID                                           int
}

// registry returns registry entries keyed by lower-case name, or nil.
func (c *Client) registry() map[string]RegEntry {
	read := c.ReadRegistry
	if read == nil {
		read = readLxss
	}
	es := read()
	m := make(map[string]RegEntry, len(es))
	for _, e := range es {
		m[strings.ToLower(e.Name)] = e
	}
	return m
}

func isExit(err error) bool {
	var ee *procexec.ExitError
	return errors.As(err, &ee)
}
