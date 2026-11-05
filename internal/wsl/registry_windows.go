//go:build windows

package wsl

import (
	"golang.org/x/sys/windows/registry"
)

// lxssKey is where WSL keeps its per-user distribution registrations.
const lxssKey = `Software\Microsoft\Windows\CurrentVersion\Lxss`

// readLxss reads WSL's per-user registry, read-only.
//
// Notes: this is WSL's internal storage, not a public contract, so it is
// used only for what the CLI does not print (paths, flavor, default uid);
// names, state and versions always come from wsl.exe. Any read error
// yields fewer entries, never a failure.
func readLxss() []RegEntry {
	root, err := registry.OpenKey(registry.CURRENT_USER, lxssKey, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer root.Close()
	subs, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	var es []RegEntry
	for _, guid := range subs {
		k, err := registry.OpenKey(root, guid, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		e := RegEntry{GUID: guid}
		e.Name, _, _ = k.GetStringValue("DistributionName")
		e.BasePath, _, _ = k.GetStringValue("BasePath")
		e.VHDFileName, _, _ = k.GetStringValue("VhdFileName")
		e.Flavor, _, _ = k.GetStringValue("Flavor")
		e.OSVersion, _, _ = k.GetStringValue("OsVersion")
		if uid, _, err := k.GetIntegerValue("DefaultUid"); err == nil {
			e.DefaultUID = int(uid)
		}
		k.Close()
		if e.Name != "" {
			es = append(es, e)
		}
	}
	return es
}
