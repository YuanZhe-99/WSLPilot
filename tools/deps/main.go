// Command deps compares WSLPilot's upstream pins with the latest releases.
//
//	go run ./tools/deps check [-strict]
//
// It reports the MyGo module pinned in go.mod against its latest version,
// and the newest WSL release in internal/upstream/tested.json against the
// latest stable WSL release on GitHub (pre-releases are shown, never
// required). With -strict an outdated pin is an error; the weekly upstream
// workflow uses it to open an issue. Run from the repository root.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/upstream"
)

const mygoModule = "github.com/egoist/mygo"

var httpClient = &http.Client{Timeout: 30 * time.Second}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "check" {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/deps check [-strict]")
		os.Exit(2)
	}
	if err := check(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "deps:", err)
		os.Exit(1)
	}
}

// check prints pinned and latest versions.
// Side effects: network requests to the Go proxy and the GitHub API
// (GITHUB_TOKEN, when set, raises the rate limit).
// Notes: with -strict a newer upstream version, or tested.json disagreeing
// with go.mod about MyGo, is an error.
func check(args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	strict := fs.Bool("strict", false, "fail when an upstream pin is outdated")
	fs.Parse(args)

	cur, latest, err := mygoVersions()
	if err != nil {
		return err
	}
	tested := upstream.Tested()
	stable, pre, err := wslReleases()
	if err != nil {
		return err
	}
	outdated := false
	report := func(name, pinned, latest string) {
		state := "current"
		if upstream.Compare(pinned, latest) < 0 {
			state = "UPDATE AVAILABLE"
			outdated = true
		}
		fmt.Printf("%-6s pinned %-9s latest %-9s %s\n", name, pinned, latest, state)
	}
	report("MyGo", cur, latest)
	report("WSL", tested.WSL[len(tested.WSL)-1], stable)
	if pre != "" && upstream.Compare(pre, stable) > 0 {
		fmt.Printf("%-6s pre-release %s (informational)\n", "WSL", pre)
	}
	if tested.MyGo != cur {
		fmt.Printf("internal/upstream/tested.json says MyGo %s, go.mod pins %s\n", tested.MyGo, cur)
		outdated = true
	}
	if outdated && *strict {
		return errors.New("upstream pins are outdated")
	}
	return nil
}

// mygoVersions returns the MyGo version in go.mod and the latest one.
func mygoVersions() (current, latest string, err error) {
	out, err := exec.Command("go", "list", "-m", "-u", "-json", mygoModule).Output()
	if err != nil {
		return "", "", fmt.Errorf("go list %s: %w", mygoModule, err)
	}
	var mod struct {
		Version string
		Update  *struct{ Version string }
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		return "", "", err
	}
	latest = mod.Version
	if mod.Update != nil {
		latest = mod.Update.Version
	}
	return mod.Version, latest, nil
}

// wslReleases returns the newest stable and newest pre-release WSL tags.
func wslReleases() (stable, pre string, err error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/microsoft/WSL/releases?per_page=30", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub microsoft/WSL releases: %s", resp.Status)
	}
	var rels []struct {
		Tag        string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return "", "", err
	}
	for _, r := range rels {
		if r.Draft {
			continue
		}
		if r.Prerelease {
			if pre == "" || upstream.Compare(r.Tag, pre) > 0 {
				pre = r.Tag
			}
		} else if stable == "" || upstream.Compare(r.Tag, stable) > 0 {
			stable = r.Tag
		}
	}
	if stable == "" {
		return "", "", errors.New("no stable WSL release found")
	}
	return stable, pre, nil
}
