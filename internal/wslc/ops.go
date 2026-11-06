package wslc

import (
	"context"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/YuanZhe-99/WSLPilot/internal/procexec"
)

// PortBinding publishes a container port (`-p host:container/proto`).
// Notes: wslc binds published ports to 127.0.0.1 unless an address is
// given (verified on 3.0.3.0).
type PortBinding struct {
	HostIP        string
	HostPort      string // "" lets wslc choose
	ContainerPort string
	Protocol      string // tcp (default) | udp
}

// String renders the binding in `-p` syntax.
func (p PortBinding) String() string {
	s := p.ContainerPort
	if p.Protocol != "" && p.Protocol != "tcp" {
		s += "/" + p.Protocol
	}
	switch {
	case p.HostIP != "":
		return p.HostIP + ":" + p.HostPort + ":" + s
	case p.HostPort != "":
		return p.HostPort + ":" + s
	}
	return s
}

// MountSpec is a volume, bind or tmpfs mount for a new container.
type MountSpec struct {
	Type     string // volume | bind | tmpfs
	Source   string // volume name or Windows path; empty for tmpfs
	Target   string
	ReadOnly bool
}

// Health is a container health check.
type Health struct {
	Cmd         string // shell form; "" with Disable for none
	Interval    string
	Timeout     string
	StartPeriod string
	Retries     int
	Disable     bool
}

// ContainerSpec is everything WSLPilot can set on a new container. Only
// options that `wslc container create --help` lists (3.0.3.0) exist here.
type ContainerSpec struct {
	Name        string
	Image       string
	Entrypoint  string // a single executable, per wslc's --entrypoint
	Command     []string
	Env         []string // KEY=VALUE
	EnvFiles    []string
	Labels      map[string]string
	Ports       []PortBinding
	Mounts      []MountSpec
	Network     string
	Aliases     []string
	IP          string
	Hostname    string
	Domainname  string
	User        string
	WorkingDir  string
	CPUs        string
	Memory      string
	ShmSize     string
	StopSignal  string
	StopTimeout *int
	Ulimits     []string // name=soft[:hard]
	DNS         []string
	DNSSearch   []string
	DNSOptions  []string
	Tmpfs       []string
	Health      *Health
	Interactive bool
	TTY         bool
	GPUs        string
	Pull        string // always | missing | never
}

// CreateArgs builds the `wslc container create` argument vector.
// Notes: labels are emitted sorted so the same spec gives the same
// command (the Compose config hash relies on deterministic specs).
func CreateArgs(s ContainerSpec) []string {
	a := []string{"container", "create"}
	add := func(flag, v string) {
		if v != "" {
			a = append(a, flag, v)
		}
	}
	add("--name", s.Name)
	add("--entrypoint", s.Entrypoint)
	for _, e := range s.Env {
		a = append(a, "--env", e)
	}
	for _, f := range s.EnvFiles {
		a = append(a, "--env-file", f)
	}
	keys := make([]string, 0, len(s.Labels))
	for k := range s.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a = append(a, "--label", k+"="+s.Labels[k])
	}
	for _, p := range s.Ports {
		a = append(a, "--publish", p.String())
	}
	for _, m := range s.Mounts {
		switch m.Type {
		case "tmpfs":
			a = append(a, "--tmpfs", m.Target)
		default:
			v := m.Source + ":" + m.Target
			if m.ReadOnly {
				v += ":ro"
			}
			a = append(a, "--volume", v)
		}
	}
	add("--network", s.Network)
	for _, al := range s.Aliases {
		a = append(a, "--network-alias", al)
	}
	add("--ip", s.IP)
	add("--hostname", s.Hostname)
	add("--domainname", s.Domainname)
	add("--user", s.User)
	add("--workdir", s.WorkingDir)
	add("--cpus", s.CPUs)
	add("--memory", s.Memory)
	add("--shm-size", s.ShmSize)
	add("--stop-signal", s.StopSignal)
	if s.StopTimeout != nil {
		a = append(a, "--stop-timeout", strconv.Itoa(*s.StopTimeout))
	}
	for _, u := range s.Ulimits {
		a = append(a, "--ulimit", u)
	}
	for _, d := range s.DNS {
		a = append(a, "--dns", d)
	}
	for _, d := range s.DNSSearch {
		a = append(a, "--dns-search", d)
	}
	for _, d := range s.DNSOptions {
		a = append(a, "--dns-option", d)
	}
	for _, t := range s.Tmpfs {
		a = append(a, "--tmpfs", t)
	}
	if h := s.Health; h != nil {
		if h.Disable {
			a = append(a, "--no-healthcheck")
		} else {
			add("--health-cmd", h.Cmd)
			add("--health-interval", h.Interval)
			add("--health-timeout", h.Timeout)
			add("--health-start-period", h.StartPeriod)
			if h.Retries > 0 {
				a = append(a, "--health-retries", strconv.Itoa(h.Retries))
			}
		}
	}
	if s.Interactive {
		a = append(a, "--interactive")
	}
	if s.TTY {
		a = append(a, "--tty")
	}
	add("--gpus", s.GPUs)
	add("--pull", s.Pull)
	a = append(a, s.Image)
	return append(a, s.Command...)
}

// Create creates a container and returns its ID.
func (c *Client) Create(ctx context.Context, s ContainerSpec) (string, error) {
	out, err := c.run(ctx, CreateArgs(s)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Start starts a container.
func (c *Client) Start(ctx context.Context, id string) error {
	return c.do(ctx, "container", "start", id)
}

// Stop stops containers; timeout < 0 uses the container's own.
func (c *Client) Stop(ctx context.Context, timeout int, ids ...string) error {
	a := []string{"container", "stop"}
	if timeout >= 0 {
		a = append(a, "--time", strconv.Itoa(timeout))
	}
	return c.do(ctx, append(a, ids...)...)
}

// Restart restarts containers.
func (c *Client) Restart(ctx context.Context, ids ...string) error {
	return c.do(ctx, append([]string{"container", "restart"}, ids...)...)
}

// Kill sends SIGKILL to containers.
func (c *Client) Kill(ctx context.Context, ids ...string) error {
	return c.do(ctx, append([]string{"container", "kill"}, ids...)...)
}

// Remove deletes containers; force also removes running ones.
func (c *Client) Remove(ctx context.Context, force, anonVolumes bool, ids ...string) error {
	a := []string{"container", "remove"}
	if force {
		a = append(a, "--force")
	}
	if anonVolumes {
		a = append(a, "--volumes")
	}
	return c.do(ctx, append(a, ids...)...)
}

// LogOptions select which log lines to stream.
type LogOptions struct {
	Follow     bool
	Tail       int // 0 = all
	Timestamps bool
	Since      string
}

// Logs streams a container's log to w until it ends (or ctx, with Follow).
func (c *Client) Logs(ctx context.Context, id string, o LogOptions, w io.Writer) error {
	a := []string{"container", "logs"}
	if o.Follow {
		a = append(a, "--follow")
	}
	if o.Tail > 0 {
		a = append(a, "--tail", strconv.Itoa(o.Tail))
	}
	if o.Timestamps {
		a = append(a, "--timestamps")
	}
	if o.Since != "" {
		a = append(a, "--since", o.Since)
	}
	a = append(a, id)
	return c.Stream(ctx, procexec.Spec{Name: Exe, Args: c.global(a...), Stdout: w, Stderr: w})
}

// ExecArgs builds `wslc exec` for argv in a running container.
func ExecArgs(id, user string, interactive, tty bool, argv ...string) []string {
	a := []string{"container", "exec"}
	if interactive {
		a = append(a, "--interactive")
	}
	if tty {
		a = append(a, "--tty")
	}
	if user != "" {
		a = append(a, "--user", user)
	}
	a = append(a, id)
	return append(a, argv...)
}

// Exec runs argv in a running container and returns its stdout.
func (c *Client) Exec(ctx context.Context, id, user string, argv ...string) (string, error) {
	return c.run(ctx, ExecArgs(id, user, false, false, argv...)...)
}

// ExecStream runs argv with raw stdin/stdout (tar pipes).
// Notes: binary data passes through `wslc exec -i` unchanged (verified
// on 3.0.3.0 with a 3 MB tar, both directions).
func (c *Client) ExecStream(ctx context.Context, id, user string, stdin io.Reader, stdout io.Writer, argv ...string) error {
	return c.Stream(ctx, procexec.Spec{Name: Exe, Args: c.global(ExecArgs(id, user, stdin != nil, false, argv...)...), Stdin: stdin, Stdout: stdout})
}

// Pull downloads an image, streaming progress text to log.
func (c *Client) Pull(ctx context.Context, ref string, log io.Writer) error {
	return c.Stream(ctx, procexec.Spec{Name: Exe, Args: c.global("image", "pull", ref), Stdout: log, Stderr: log})
}

// RemoveImages deletes images.
func (c *Client) RemoveImages(ctx context.Context, force bool, refs ...string) error {
	a := []string{"image", "remove"}
	if force {
		a = append(a, "--force")
	}
	return c.do(ctx, append(a, refs...)...)
}

// Tag adds a tag to an image.
func (c *Client) Tag(ctx context.Context, src, dst string) error {
	return c.do(ctx, "image", "tag", src, dst)
}

// BuildOptions are `wslc build` options.
type BuildOptions struct {
	Context    string
	Dockerfile string
	Tags       []string
	Args       []string // KEY=VALUE
	Target     string
	Labels     map[string]string
	Pull       bool
	NoCache    bool
}

// Build builds an image, streaming plain progress to log.
func (c *Client) Build(ctx context.Context, o BuildOptions, log io.Writer) error {
	a := []string{"image", "build", "--progress", "plain"}
	if o.Dockerfile != "" {
		a = append(a, "--file", o.Dockerfile)
	}
	for _, t := range o.Tags {
		a = append(a, "--tag", t)
	}
	for _, b := range o.Args {
		a = append(a, "--build-arg", b)
	}
	if o.Target != "" {
		a = append(a, "--target", o.Target)
	}
	keys := make([]string, 0, len(o.Labels))
	for k := range o.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a = append(a, "--label", k+"="+o.Labels[k])
	}
	if o.Pull {
		a = append(a, "--pull")
	}
	if o.NoCache {
		a = append(a, "--no-cache")
	}
	a = append(a, o.Context)
	return c.Stream(ctx, procexec.Spec{Name: Exe, Args: c.global(a...), Stdout: log, Stderr: log})
}

// Save writes images as a tar file (`image save --output`).
func (c *Client) Save(ctx context.Context, file string, refs ...string) error {
	return c.do(ctx, append([]string{"image", "save", "--output", file}, refs...)...)
}

// Load reads an image tar file made by Save (`image load --input`).
func (c *Client) Load(ctx context.Context, file string, log io.Writer) error {
	return c.Stream(ctx, procexec.Spec{Name: Exe, Args: c.global("image", "load", "--input", file), Stdout: log, Stderr: log})
}

// ImportRootfs creates an image from a root filesystem tar read from r.
func (c *Client) ImportRootfs(ctx context.Context, r io.Reader, ref string) error {
	return c.Stream(ctx, procexec.Spec{Name: Exe, Args: c.global("image", "import", "-", ref), Stdin: r})
}

// ExportContainer writes a stopped container's filesystem tar to w.
// Notes: wslc refuses running containers (WSLC_E_CONTAINER_IS_RUNNING).
func (c *Client) ExportContainer(ctx context.Context, id string, w io.Writer) error {
	return c.Stream(ctx, procexec.Spec{Name: Exe, Args: c.global("container", "export", id), Stdout: w})
}

// CreateVolume creates a volume. Driver "vhd" needs opts["SizeBytes"]
// (verified on 3.0.3.0: "Missing required option: 'SizeBytes'").
func (c *Client) CreateVolume(ctx context.Context, name, driver string, opts, labels map[string]string) error {
	a := []string{"volume", "create"}
	if driver != "" {
		a = append(a, "--driver", driver)
	}
	for _, k := range sortedKeys(opts) {
		a = append(a, "--opt", k+"="+opts[k])
	}
	for _, k := range sortedKeys(labels) {
		a = append(a, "--label", k+"="+labels[k])
	}
	return c.do(ctx, append(a, name)...)
}

// RemoveVolumes deletes volumes and their data.
func (c *Client) RemoveVolumes(ctx context.Context, names ...string) error {
	return c.do(ctx, append([]string{"volume", "remove"}, names...)...)
}

// NetworkOptions are `wslc network create` options.
type NetworkOptions struct {
	Driver   string
	Subnet   string
	Gateway  string
	IPRange  string
	Internal bool
	Labels   map[string]string
}

// CreateNetwork creates a network.
func (c *Client) CreateNetwork(ctx context.Context, name string, o NetworkOptions) error {
	a := []string{"network", "create"}
	if o.Driver != "" {
		a = append(a, "--driver", o.Driver)
	}
	if o.Subnet != "" {
		a = append(a, "--subnet", o.Subnet)
	}
	if o.Gateway != "" {
		a = append(a, "--gateway", o.Gateway)
	}
	if o.IPRange != "" {
		a = append(a, "--ip-range", o.IPRange)
	}
	if o.Internal {
		a = append(a, "--internal")
	}
	for _, k := range sortedKeys(o.Labels) {
		a = append(a, "--label", k+"="+o.Labels[k])
	}
	return c.do(ctx, append(a, name)...)
}

// RemoveNetworks deletes networks.
func (c *Client) RemoveNetworks(ctx context.Context, names ...string) error {
	return c.do(ctx, append([]string{"network", "remove"}, names...)...)
}

// Connect attaches a container to a network with optional aliases.
func (c *Client) Connect(ctx context.Context, network, id string, aliases ...string) error {
	a := []string{"network", "connect"}
	for _, al := range aliases {
		a = append(a, "--network-alias", al)
	}
	return c.do(ctx, append(a, network, id)...)
}

// Disconnect detaches a container from a network.
func (c *Client) Disconnect(ctx context.Context, network, id string) error {
	return c.do(ctx, "network", "disconnect", network, id)
}

// Prune removes unused objects of kind: "container" (stopped ones),
// "image" (dangling ones, every unused one with all), "volume" or
// "network". Destructive; callers confirm first.
// Inputs: filters are --filter values, e.g. "label=k=v" (verified on
// WSLc 3.0.3.0 for containers and networks).
// Returns: the command's report ("Deleted Containers:" …).
func (c *Client) Prune(ctx context.Context, kind string, all bool, filters ...string) (string, error) {
	args := []string{kind, "prune", "--force"}
	if all {
		args = append(args, "--all")
	}
	for _, f := range filters {
		args = append(args, "--filter", f)
	}
	return c.run(ctx, args...)
}

// do runs a command whose output is not needed.
func (c *Client) do(ctx context.Context, args ...string) error {
	_, err := c.run(ctx, args...)
	return err
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TerminateSession stops the WSLc session VM (all containers stop); the
// next wslc command starts a new one, reading settings.yaml again.
func (c *Client) TerminateSession(ctx context.Context) error {
	return c.do(ctx, "system", "session", "terminate")
}
