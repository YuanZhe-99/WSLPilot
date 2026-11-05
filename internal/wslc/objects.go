package wslc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/YuanZhe-99/WSLPilot/internal/procexec"
)

// ContainerSummary is one line of `wslc container list --all --format json`.
// Notes: the list's Labels field is a comma-joined string whose values can
// contain commas (WSLc's own metadata label holds JSON), so labels are
// read from Inspect, never from this summary.
type ContainerSummary struct {
	ID           string `json:"ID"`
	Names        string `json:"Names"`
	Image        string `json:"Image"`
	Command      string `json:"Command"`
	CreatedAt    string `json:"CreatedAt"`
	State        string `json:"State"`  // created, running, exited, …
	Status       string `json:"Status"` // human text, e.g. "Up 3 seconds"
	Ports        string `json:"Ports"`
	Networks     string `json:"Networks"`
	HealthStatus string `json:"HealthStatus"`
	Mounts       string `json:"Mounts"`
}

// Running reports whether the container's State is "running".
func (c ContainerSummary) Running() bool { return c.State == "running" }

// Container is the part of `wslc container inspect` WSLPilot uses.
type Container struct {
	ID      string `json:"Id"`
	Name    string `json:"Name"` // with a leading "/"
	Created string `json:"Created"`
	ImageID string `json:"Image"`
	Config  struct {
		Image       string            `json:"Image"`
		Cmd         []string          `json:"Cmd"`
		Entrypoint  []string          `json:"Entrypoint"`
		Env         []string          `json:"Env"`
		Labels      map[string]string `json:"Labels"`
		User        string            `json:"User"`
		WorkingDir  string            `json:"WorkingDir"`
		StopTimeout *int              `json:"StopTimeout"`
		Healthcheck *struct {
			Test []string `json:"Test"`
		} `json:"Healthcheck"`
	} `json:"Config"`
	HostConfig struct {
		Memory      int64  `json:"Memory"`
		NanoCpus    int64  `json:"NanoCpus"`
		NetworkMode string `json:"NetworkMode"`
	} `json:"HostConfig"`
	Labels map[string]string `json:"Labels"`
	Mounts []Mount           `json:"Mounts"`
	Ports  map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	} `json:"Ports"`
	NetworkSettings struct {
		Networks map[string]struct {
			Aliases   []string `json:"Aliases"`
			IPAddress string   `json:"IPAddress"`
			Gateway   string   `json:"Gateway"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
	State struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		ExitCode   int    `json:"ExitCode"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
		Health     *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
}

// DisplayName is the container name without the leading slash.
func (c Container) DisplayName() string { return strings.TrimPrefix(c.Name, "/") }

// Mount is one entry of a container's Mounts.
type Mount struct {
	Type        string `json:"Type"` // volume, bind, tmpfs
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	ReadWrite   bool   `json:"ReadWrite"`
}

// Image is one line of `wslc image list --format json`.
type Image struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	Digest     string `json:"Digest"`
	Size       string `json:"Size"`
	CreatedAt  string `json:"CreatedAt"`
	Containers string `json:"Containers"`
}

// Ref is "repository:tag", or the ID for a dangling image.
func (i Image) Ref() string {
	if i.Repository == "" || i.Repository == "<none>" {
		return i.ID
	}
	return i.Repository + ":" + i.Tag
}

// ImageDetail is the part of `wslc image inspect` WSLPilot uses.
type ImageDetail struct {
	ID           string   `json:"Id"`
	RepoTags     []string `json:"RepoTags"`
	RepoDigests  []string `json:"RepoDigests"`
	Architecture string   `json:"Architecture"`
	Os           string   `json:"Os"`
	Created      string   `json:"Created"`
	Size         int64    `json:"Size"`
	Config       struct {
		Cmd          []string            `json:"Cmd"`
		Entrypoint   []string            `json:"Entrypoint"`
		Env          []string            `json:"Env"`
		ExposedPorts map[string]struct{} `json:"ExposedPorts"`
		Labels       map[string]string   `json:"Labels"`
		User         string              `json:"User"`
		WorkingDir   string              `json:"WorkingDir"`
		StopSignal   string              `json:"StopSignal"`
	} `json:"Config"`
}

// Volume is `wslc volume inspect` output (also enough for lists).
type Volume struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"` // guest | vhd
	CreatedAt  string            `json:"CreatedAt"`
	Labels     map[string]string `json:"Labels"`
	Mountpoint string            `json:"Mountpoint"`
	Options    map[string]string `json:"Options"`
	Status     struct {
		// HostPath is the VHDX of a vhd volume on the Windows side.
		HostPath  string `json:"HostPath"`
		SizeBytes string `json:"SizeBytes"`
	} `json:"Status"`
}

// Network is `wslc network inspect` output.
type Network struct {
	ID       string            `json:"Id"`
	Name     string            `json:"Name"`
	Driver   string            `json:"Driver"`
	Created  string            `json:"Created"`
	Internal bool              `json:"Internal"`
	Labels   map[string]string `json:"Labels"`
	IPAM     struct {
		Config []struct {
			Subnet  string `json:"Subnet"`
			Gateway string `json:"Gateway"`
		} `json:"Config"`
	} `json:"IPAM"`
	Containers map[string]struct {
		Name        string `json:"Name"`
		IPv4Address string `json:"IPv4Address"`
	} `json:"Containers"`
}

// Builtin reports whether the network is one WSLc creates itself.
func (n Network) Builtin() bool {
	return n.Name == "bridge" || n.Name == "host" || n.Name == "none"
}

// Stats is one line of `wslc stats --format json`.
type Stats struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
	MemPerc  string `json:"MemPerc"`
	NetIO    string `json:"NetIO"`
	BlockIO  string `json:"BlockIO"`
	PIDs     int    `json:"PIDs"`
}

// Event is one line of `wslc events --format json` (Docker's event shape).
type Event struct {
	Type   string `json:"Type"`   // container, network, image, volume
	Action string `json:"Action"` // start, die, stop, kill, connect, …
	Actor  struct {
		ID         string            `json:"ID"`
		Attributes map[string]string `json:"Attributes"`
	} `json:"Actor"`
	Time     int64 `json:"time"`
	TimeNano int64 `json:"timeNano"`
}

// At returns the event time.
func (e Event) At() time.Time { return time.Unix(0, e.TimeNano) }

// ParseLines decodes JSON-lines output (one object per line) into T.
func ParseLines[T any](out string) ([]T, error) {
	var res []T
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, fmt.Errorf("wslc: %w in %q", err, line)
		}
		res = append(res, v)
	}
	return res, nil
}

// ParseArray decodes the JSON array printed by the inspect commands.
func ParseArray[T any](out string) ([]T, error) {
	var res []T
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("wslc inspect: %w", err)
	}
	return res, nil
}

// ErrNotFound means an inspected object does not exist.
var ErrNotFound = errors.New("wslc: object not found")

// global prefixes the session option to args when the client has one.
func (c *Client) global(args ...string) []string {
	if c.Session == "" {
		return args
	}
	return append([]string{"--session", c.Session}, args...)
}

// run runs wslc with the client's global options.
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	return c.Run(ctx, Exe, c.global(args...)...)
}

// Containers lists all containers (any state), optionally filtered
// (e.g. "label=com.docker.compose.project=demo").
func (c *Client) Containers(ctx context.Context, filters ...string) ([]ContainerSummary, error) {
	a := []string{"container", "list", "--all", "--no-trunc", "--format", "json"}
	for _, f := range filters {
		a = append(a, "--filter", f)
	}
	out, err := c.run(ctx, a...)
	if err != nil {
		return nil, err
	}
	return ParseLines[ContainerSummary](out)
}

// InspectContainers returns full details of the given containers.
// Returns: ErrNotFound when any of them is missing (wslc exits 1 and
// prints "[]" for the batch).
func (c *Client) InspectContainers(ctx context.Context, ids ...string) ([]Container, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out, err := c.run(ctx, append([]string{"container", "inspect"}, ids...)...)
	if err != nil {
		if isExit(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return ParseArray[Container](out)
}

// ContainersDetailed lists every container (filtered) with inspect detail.
func (c *Client) ContainersDetailed(ctx context.Context, filters ...string) ([]Container, error) {
	sum, err := c.Containers(ctx, filters...)
	if err != nil || len(sum) == 0 {
		return nil, err
	}
	ids := make([]string, len(sum))
	for i, s := range sum {
		ids[i] = s.ID
	}
	return c.InspectContainers(ctx, ids...)
}

// Images lists images.
func (c *Client) Images(ctx context.Context) ([]Image, error) {
	out, err := c.run(ctx, "image", "list", "--no-trunc", "--format", "json")
	if err != nil {
		return nil, err
	}
	return ParseLines[Image](out)
}

// InspectImage returns one image's details.
func (c *Client) InspectImage(ctx context.Context, ref string) (ImageDetail, error) {
	out, err := c.run(ctx, "image", "inspect", ref)
	if err != nil {
		if isExit(err) {
			return ImageDetail{}, ErrNotFound
		}
		return ImageDetail{}, err
	}
	ds, err := ParseArray[ImageDetail](out)
	if err != nil || len(ds) == 0 {
		return ImageDetail{}, ErrNotFound
	}
	return ds[0], nil
}

// Volumes lists volumes with full detail (list, then one inspect).
func (c *Client) Volumes(ctx context.Context, filters ...string) ([]Volume, error) {
	a := []string{"volume", "list", "--quiet"}
	for _, f := range filters {
		a = append(a, "--filter", f)
	}
	out, err := c.run(ctx, a...)
	if err != nil {
		return nil, err
	}
	names := nonEmptyLines(out)
	if len(names) == 0 {
		return nil, nil
	}
	return c.InspectVolumes(ctx, names...)
}

// InspectVolumes returns the given volumes.
func (c *Client) InspectVolumes(ctx context.Context, names ...string) ([]Volume, error) {
	out, err := c.run(ctx, append([]string{"volume", "inspect"}, names...)...)
	if err != nil {
		if isExit(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	vs, err := ParseArray[Volume](out)
	sort.Slice(vs, func(i, j int) bool { return vs[i].Name < vs[j].Name })
	return vs, err
}

// Networks lists networks with full detail.
func (c *Client) Networks(ctx context.Context) ([]Network, error) {
	out, err := c.run(ctx, "network", "list", "--quiet", "--no-trunc")
	if err != nil {
		return nil, err
	}
	ids := nonEmptyLines(out)
	if len(ids) == 0 {
		return nil, nil
	}
	out, err = c.run(ctx, append([]string{"network", "inspect"}, ids...)...)
	if err != nil {
		return nil, err
	}
	ns, err := ParseArray[Network](out)
	sort.Slice(ns, func(i, j int) bool { return ns[i].Name < ns[j].Name })
	return ns, err
}

// Stats returns a one-shot resource sample of running containers.
func (c *Client) Stats(ctx context.Context) ([]Stats, error) {
	out, err := c.run(ctx, "stats", "--no-trunc", "--format", "json")
	if err != nil {
		return nil, err
	}
	return ParseLines[Stats](out)
}

// Events streams container events until ctx ends, calling fn for each.
// Notes: `wslc events` keeps the connection open (verified on 3.0.3.0);
// a malformed line is skipped rather than ending the stream.
func (c *Client) Events(ctx context.Context, fn func(Event)) error {
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := c.Stream(ctx, procexec.Spec{Name: Exe, Args: c.global("system", "events", "--format", "json"), Stdout: pw})
		pw.CloseWithError(err)
		done <- err
	}()
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			fn(e)
		}
	}
	pr.Close()
	return <-done
}

func nonEmptyLines(out string) []string {
	var res []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			res = append(res, l)
		}
	}
	return res
}

func isExit(err error) bool {
	var ee *procexec.ExitError
	return errors.As(err, &ee)
}
