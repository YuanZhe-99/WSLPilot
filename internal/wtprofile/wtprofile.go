// Package wtprofile adds Windows Terminal profiles through a JSON fragment
// extension (%LOCALAPPDATA%\Microsoft\Windows Terminal\Fragments\WSLPilot),
// Windows Terminal's own mechanism for apps, so the user's settings.json
// is never edited. WSL already writes a fragment per distribution
// (verified on 3.0.3.0); WSLPilot adds shells into WSLc containers and
// distribution shells with a chosen user or folder.
package wtprofile

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/YuanZhe-99/WSLPilot/internal/store"
)

// Profile is one Windows Terminal profile in the fragment.
type Profile struct {
	GUID              string `json:"guid"`
	Name              string `json:"name"`
	Commandline       string `json:"commandline"`
	StartingDirectory string `json:"startingDirectory,omitempty"`
	Icon              string `json:"icon,omitempty"`
	TabTitle          string `json:"tabTitle,omitempty"`
}

// fragment is the file's shape.
type fragment struct {
	Profiles []Profile `json:"profiles"`
}

// Dir returns the fragment folder, or "" when LOCALAPPDATA is unset.
func Dir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return ""
	}
	return filepath.Join(base, "Microsoft", "Windows Terminal", "Fragments", "WSLPilot")
}

// Installed reports whether Windows Terminal is present (its packaged
// settings folder or the fragments root exists).
func Installed() bool {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return false
	}
	for _, p := range []string{
		filepath.Join(base, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe"),
		filepath.Join(base, "Packages", "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe"),
		filepath.Join(base, "Microsoft", "Windows Terminal"),
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// Fragment edits one fragment file.
type Fragment struct{ Path string }

// Default returns the fragment WSLPilot owns.
func Default() (*Fragment, error) {
	d := Dir()
	if d == "" {
		return nil, errors.New("wtprofile: LOCALAPPDATA is not set")
	}
	return &Fragment{Path: filepath.Join(d, "wslpilot.json")}, nil
}

// List returns the profiles in the fragment.
func (f *Fragment) List() ([]Profile, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var fr fragment
	if err := json.Unmarshal(b, &fr); err != nil {
		return nil, err
	}
	return fr.Profiles, nil
}

// Put adds or replaces a profile (matched by GUID).
func (f *Fragment) Put(p Profile) error {
	ps, err := f.List()
	if err != nil {
		return err
	}
	replaced := false
	for i := range ps {
		if ps[i].GUID == p.GUID {
			ps[i], replaced = p, true
		}
	}
	if !replaced {
		ps = append(ps, p)
	}
	return f.write(ps)
}

// Remove deletes a profile by GUID.
func (f *Fragment) Remove(guid string) error {
	ps, err := f.List()
	if err != nil {
		return err
	}
	kept := ps[:0]
	for _, p := range ps {
		if p.GUID != guid {
			kept = append(kept, p)
		}
	}
	return f.write(kept)
}

func (f *Fragment) write(ps []Profile) error {
	sort.Slice(ps, func(i, j int) bool { return ps[i].Name < ps[j].Name })
	b, err := json.MarshalIndent(fragment{Profiles: ps}, "", "  ")
	if err != nil {
		return err
	}
	return store.WriteFileAtomic(f.Path, append(b, '\n'))
}

// namespace is WSLPilot's UUIDv5 namespace for profile GUIDs.
var namespace = [16]byte{0x5b, 0x1e, 0x0c, 0x7a, 0x3d, 0x52, 0x4b, 0x5e, 0x9a, 0x61, 0x2f, 0x0d, 0x8c, 0x77, 0x41, 0x3e}

// GUIDFor returns a stable profile GUID for a key such as
// "container:web" (UUIDv5), so re-adding a profile replaces it.
func GUIDFor(key string) string {
	h := sha1.New()
	h.Write(namespace[:])
	h.Write([]byte(key))
	s := h.Sum(nil)
	s[6] = s[6]&0x0f | 0x50
	s[8] = s[8]&0x3f | 0x80
	return fmt.Sprintf("{%x-%x-%x-%x-%x}", s[0:4], s[4:6], s[6:8], s[8:10], s[10:16])
}

// ContainerShell is a profile that opens a shell in a WSLc container.
func ContainerShell(container, shell string) Profile {
	if shell == "" {
		shell = "sh"
	}
	return Profile{GUID: GUIDFor("container:" + container), Name: container + " (WSLc)",
		Commandline: "wslc.exe exec -it " + container + " " + shell, TabTitle: container}
}

// DistroShell is a profile for a distribution with a user and folder.
func DistroShell(distro, user, dir string) Profile {
	cmd := "wsl.exe -d " + distro
	name := distro
	if user != "" {
		cmd += " -u " + user
		name += " (" + user + ")"
	}
	if dir != "" {
		cmd += " --cd " + dir
	}
	return Profile{GUID: GUIDFor("distro:" + distro + ":" + user + ":" + dir), Name: name, Commandline: cmd}
}
