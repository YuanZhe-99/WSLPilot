package compose

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/YuanZhe-99/WSLPilot/internal/wslc"
)

// Labels WSLPilot puts on the objects it creates. The com.docker.compose
// names follow Docker Compose, so a future upstream Compose can adopt them.
const (
	LabelProject     = "com.docker.compose.project"
	LabelService     = "com.docker.compose.service"
	LabelNumber      = "com.docker.compose.container-number"
	LabelConfigHash  = "com.docker.compose.config-hash"
	LabelConfigFiles = "com.docker.compose.project.config_files"
	LabelWorkingDir  = "com.docker.compose.project.working_dir"
	LabelOneoff      = "com.docker.compose.oneoff"
	LabelNetwork     = "com.docker.compose.network"
	LabelVolume      = "com.docker.compose.volume"
	LabelBackend     = "io.wslpilot.backend"
	// LabelRestart carries the Compose restart policy; WSLc has no
	// --restart, so the WSLPilot agent applies it.
	LabelRestart = "io.wslpilot.restart"
)

// NetworkPlan is one network the project uses.
type NetworkPlan struct {
	Key      string // Compose key, e.g. "default"
	Name     string // wslc name, e.g. "demo_default"
	External bool
	Options  wslc.NetworkOptions
}

// VolumePlan is one named volume the project uses.
type VolumePlan struct {
	Key      string
	Name     string
	External bool
	Driver   string
	Opts     map[string]string
	Labels   map[string]string
	// Bind is the Windows folder replacing the volume in bind mode.
	Bind string
}

// Attachment is a further network a container joins after creation.
type Attachment struct {
	Network string
	Aliases []string
}

// ContainerPlan is one desired container of a service.
type ContainerPlan struct {
	Name   string
	Number int
	Spec   wslc.ContainerSpec
	Extra  []Attachment
	Hash   string
}

// Dependency is one depends_on entry.
type Dependency struct {
	Service   string
	Condition string // service_started | service_healthy | service_completed_successfully
	Required  bool
}

// ServicePlan is the desired state of one service.
type ServicePlan struct {
	Name       string
	Image      string
	Build      *wslc.BuildOptions
	PullPolicy string // always | missing | never | build
	DependsOn  []Dependency
	Containers []ContainerPlan
	// BindDirs are host folders of bind mounts to create when missing.
	BindDirs []string
	// HasHealth reports a healthcheck, needed by service_healthy waits.
	HasHealth bool
}

// Plan is the desired state of a whole project, services in dependency order.
type Plan struct {
	Project  string
	Networks []NetworkPlan
	Volumes  []VolumePlan
	Services []ServicePlan
}

// Service returns the named service plan, or nil.
func (p *Plan) Service(name string) *ServicePlan {
	for i := range p.Services {
		if p.Services[i].Name == name {
			return &p.Services[i]
		}
	}
	return nil
}

// ContainerName is the Compose container name `<project>-<service>-<n>`.
func ContainerName(project, service string, n int) string {
	return project + "-" + service + "-" + strconv.Itoa(n)
}

// ResourceName is the Compose network or volume name `<project>_<key>`.
func ResourceName(project, key string) string { return project + "_" + key }

// ImageName is the image name of a built service without `image`.
func ImageName(project, service string) string { return project + "-" + service }

// Translate turns a loaded project into a plan for the builtin backend and
// reports every key it cannot honor.
//
// Inputs: p from Load; meta for volume modes and the label of config files.
// Returns: the plan (meaningful only when the report has no errors) and
// the report.
// Notes: pure; touches neither wslc nor the file system.
func Translate(p *types.Project, meta *Project) (*Plan, Report) {
	var r Report
	plan := &Plan{Project: p.Name}
	checkTopLevel(&r, p)

	usedNets := map[string]bool{}
	usedVols := map[string]bool{}
	names := p.ServiceNames()
	var services []ServicePlan
	for _, name := range names {
		s := p.Services[name]
		sp := translateService(&r, p, meta, &s, usedNets, usedVols)
		services = append(services, sp)
	}
	order, err := topoOrder(p.Services)
	if err != nil {
		r.errorf("", "depends_on", "%v", err)
		order = names
	}
	byName := map[string]ServicePlan{}
	for _, s := range services {
		byName[s.Name] = s
	}
	for _, n := range order {
		plan.Services = append(plan.Services, byName[n])
	}
	for _, s := range plan.Services {
		for _, d := range s.DependsOn {
			if d.Condition == types.ServiceConditionHealthy {
				if dep := plan.Service(d.Service); dep != nil && !dep.HasHealth {
					r.errorf(s.Name, "depends_on."+d.Service, "condition service_healthy needs a healthcheck on %q", d.Service)
				}
			}
		}
	}
	plan.Networks = translateNetworks(&r, p, usedNets)
	plan.Volumes = translateVolumes(&r, p, meta, usedVols)
	return plan, r
}

// checkTopLevel reports top-level sections the builtin backend cannot use.
func checkTopLevel(r *Report, p *types.Project) {
	for _, k := range sortedKeys(p.Secrets) {
		r.warnf("", "secrets."+k, "secrets are not supported by the builtin backend; defined but unused here")
	}
	for _, k := range sortedKeys(p.Configs) {
		r.warnf("", "configs."+k, "configs are not supported by the builtin backend; defined but unused here")
	}
	for _, k := range sortedKeys(p.Models) {
		r.errorf("", "models."+k, "not supported by the builtin backend")
	}
	for _, k := range sortedKeys(p.Jobs) {
		r.errorf("", "jobs."+k, "not supported by the builtin backend")
	}
}

// translateNetworks plans the networks services use.
func translateNetworks(r *Report, p *types.Project, used map[string]bool) []NetworkPlan {
	var res []NetworkPlan
	for _, key := range sortedKeys(p.Networks) {
		if !used[key] {
			continue
		}
		n := p.Networks[key]
		path := "networks." + key
		r.rejectExtra("", path, n, "name", "driver", "ipam", "external", "internal", "labels", "attachable", "enable_ipv4", "enable_ipv6")
		np := NetworkPlan{Key: key, Name: n.Name, External: bool(n.External)}
		if np.Name == "" {
			np.Name = ResourceName(p.Name, key)
		}
		if np.External {
			res = append(res, np)
			continue
		}
		switch n.Driver {
		case "", "bridge":
		default:
			r.errorf("", path+".driver", "driver %q is not supported by the builtin backend", n.Driver)
		}
		if n.Attachable {
			r.warnf("", path+".attachable", "ignored: wslc networks are always attachable")
		}
		if n.EnableIPv6 != nil && *n.EnableIPv6 {
			r.errorf("", path+".enable_ipv6", "IPv6 networks are not supported by the builtin backend")
		}
		if n.EnableIPv4 != nil && !*n.EnableIPv4 {
			r.errorf("", path+".enable_ipv4", "disabling IPv4 is not supported by the builtin backend")
		}
		r.rejectExtra("", path+".ipam", n.Ipam, "driver", "config")
		if n.Ipam.Driver != "" && n.Ipam.Driver != "default" {
			r.errorf("", path+".ipam.driver", "IPAM driver %q is not supported", n.Ipam.Driver)
		}
		if len(n.Ipam.Config) > 1 {
			r.errorf("", path+".ipam.config", "only one IPAM pool is supported")
		}
		np.Options = wslc.NetworkOptions{Internal: n.Internal, Labels: map[string]string{}}
		if len(n.Ipam.Config) > 0 && n.Ipam.Config[0] != nil {
			pool := n.Ipam.Config[0]
			r.rejectExtra("", path+".ipam.config", pool, "subnet", "gateway", "ip_range")
			np.Options.Subnet, np.Options.Gateway, np.Options.IPRange = pool.Subnet, pool.Gateway, pool.IPRange
		}
		for k, v := range n.Labels {
			np.Options.Labels[k] = v
		}
		np.Options.Labels[LabelProject] = p.Name
		np.Options.Labels[LabelNetwork] = key
		np.Options.Labels[LabelBackend] = string(BackendBuiltin)
		res = append(res, np)
	}
	return res
}

// translateVolumes plans the named volumes services use.
func translateVolumes(r *Report, p *types.Project, meta *Project, used map[string]bool) []VolumePlan {
	var res []VolumePlan
	for _, key := range sortedKeys(p.Volumes) {
		if !used[key] {
			continue
		}
		v := p.Volumes[key]
		path := "volumes." + key
		r.rejectExtra("", path, v, "name", "driver", "driver_opts", "external", "labels")
		vp := VolumePlan{Key: key, Name: v.Name, External: bool(v.External)}
		if vp.Name == "" {
			vp.Name = ResourceName(p.Name, key)
		}
		if c, ok := meta.Volumes[key]; ok && c.Mode == VolumeBind {
			vp.Bind = c.Path
			res = append(res, vp)
			continue
		}
		if vp.External {
			res = append(res, vp)
			continue
		}
		switch v.Driver {
		case "", "local":
			if len(v.DriverOpts) > 0 {
				r.errorf("", path+".driver_opts", "driver options of the local driver are not supported by the builtin backend")
			}
		case "guest":
			vp.Driver = "guest"
			if len(v.DriverOpts) > 0 {
				r.errorf("", path+".driver_opts", "the guest driver takes no options")
			}
		case "vhd":
			vp.Driver = "vhd"
			for k := range v.DriverOpts {
				if k != "SizeBytes" {
					r.errorf("", path+".driver_opts."+k, "the vhd driver only takes SizeBytes")
				}
			}
			if v.DriverOpts["SizeBytes"] == "" {
				r.errorf("", path+".driver_opts", "the vhd driver needs SizeBytes")
			}
			vp.Opts = map[string]string(v.DriverOpts)
		default:
			r.errorf("", path+".driver", "driver %q is not supported by the builtin backend (use local, guest or vhd)", v.Driver)
		}
		vp.Labels = map[string]string{}
		for k, val := range v.Labels {
			vp.Labels[k] = val
		}
		vp.Labels[LabelProject] = p.Name
		vp.Labels[LabelVolume] = key
		vp.Labels[LabelBackend] = string(BackendBuiltin)
		res = append(res, vp)
	}
	return res
}

// reservedLabel reports labels WSLPilot sets itself.
func reservedLabel(k string) bool {
	return strings.HasPrefix(k, "com.docker.compose.") || strings.HasPrefix(k, "io.wslpilot.")
}

// restartPolicy matches the Compose restart values.
var restartPolicy = regexp.MustCompile(`^(no|always|unless-stopped|on-failure(:[0-9]+)?)$`)

// translateService validates one service and builds its plan.
func translateService(r *Report, p *types.Project, meta *Project, s *types.ServiceConfig, usedNets, usedVols map[string]bool) ServicePlan {
	name := s.Name
	checkServiceKeys(r, s)
	sp := ServicePlan{Name: name}
	var spec wslc.ContainerSpec

	// Image and build.
	sp.Image = s.Image
	if s.Build != nil {
		r.rejectExtra(name, "build", s.Build, "context", "dockerfile", "args", "labels", "target", "tags", "pull", "no_cache")
		if sp.Image == "" {
			sp.Image = ImageName(p.Name, name)
		}
		b := &wslc.BuildOptions{Context: s.Build.Context, Dockerfile: s.Build.Dockerfile, Target: s.Build.Target,
			Tags: append([]string{sp.Image}, s.Build.Tags...), Pull: s.Build.Pull, NoCache: s.Build.NoCache}
		for _, k := range sortedKeys(s.Build.Args) {
			if v := s.Build.Args[k]; v != nil {
				b.Args = append(b.Args, k+"="+*v)
			}
		}
		if len(s.Build.Labels) > 0 {
			b.Labels = map[string]string(s.Build.Labels)
		}
		sp.Build = b
	}
	if sp.Image == "" {
		r.errorf(name, "image", "a service needs an image or a build section")
	}
	spec.Image = sp.Image
	switch s.PullPolicy {
	case "", types.PullPolicyMissing, types.PullPolicyIfNotPresent:
		sp.PullPolicy = types.PullPolicyMissing
	case types.PullPolicyAlways, types.PullPolicyNever, types.PullPolicyBuild:
		sp.PullPolicy = s.PullPolicy
		if s.PullPolicy == types.PullPolicyBuild && s.Build == nil {
			r.errorf(name, "pull_policy", "pull_policy build needs a build section")
		}
	default:
		r.errorf(name, "pull_policy", "pull_policy %q is not supported by the builtin backend", s.PullPolicy)
	}

	// Command and entrypoint. wslc --entrypoint takes one executable, so a
	// longer entrypoint becomes entrypoint[0] with the rest before command.
	if s.Command != nil && len(s.Command) == 0 {
		r.errorf(name, "command", "clearing the image's command is not supported by the builtin backend")
	}
	if s.Entrypoint != nil {
		if len(s.Entrypoint) == 0 {
			r.errorf(name, "entrypoint", "clearing the image's entrypoint is not supported by the builtin backend")
		} else {
			spec.Entrypoint = s.Entrypoint[0]
			spec.Command = append(spec.Command, s.Entrypoint[1:]...)
		}
	}
	spec.Command = append(spec.Command, s.Command...)

	// Environment (env_file is already merged in by the loader) and labels.
	for _, k := range sortedKeys(s.Environment) {
		if v := s.Environment[k]; v != nil {
			spec.Env = append(spec.Env, k+"="+*v)
		}
	}
	spec.Labels = map[string]string{}
	for k, v := range s.Labels {
		if reservedLabel(k) {
			r.errorf(name, "labels."+k, "label is reserved for WSLPilot")
			continue
		}
		spec.Labels[k] = v
	}
	restart := s.Restart
	if s.Deploy != nil {
		d := s.Deploy
		r.rejectExtra(name, "deploy", d, "mode", "replicas", "resources", "restart_policy")
		if d.Mode != "" && d.Mode != "replicated" {
			r.errorf(name, "deploy.mode", "mode %q is not supported by the builtin backend", d.Mode)
		}
		r.rejectExtra(name, "deploy.resources", d.Resources, "limits", "reservations")
		if l := d.Resources.Limits; l != nil {
			r.rejectExtra(name, "deploy.resources.limits", l, "cpus", "memory", "devices")
			if l.NanoCPUs > 0 {
				spec.CPUs = formatCPUs(float64(l.NanoCPUs))
			}
			if l.MemoryBytes > 0 {
				spec.Memory = formatBytes(int64(l.MemoryBytes))
			}
			gpuRequests(r, name, "deploy.resources.limits.devices", l.Devices, &spec)
		}
		if rs := d.Resources.Reservations; rs != nil {
			r.rejectExtra(name, "deploy.resources.reservations", rs, "devices")
			gpuRequests(r, name, "deploy.resources.reservations.devices", rs.Devices, &spec)
		}
		if rp := d.RestartPolicy; rp != nil {
			r.rejectExtra(name, "deploy.restart_policy", rp, "condition")
			if restart == "" {
				switch rp.Condition {
				case "none":
					restart = "no"
				case "on-failure":
					restart = "on-failure"
				case "", "any":
					restart = "always"
				default:
					r.errorf(name, "deploy.restart_policy.condition", "unknown condition %q", rp.Condition)
				}
			}
		}
	}
	if restart != "" {
		if !restartPolicy.MatchString(restart) {
			r.errorf(name, "restart", "unknown restart policy %q", restart)
		}
		spec.Labels[LabelRestart] = restart
	}
	if s.CPUS > 0 && spec.CPUs == "" {
		spec.CPUs = formatCPUs(float64(s.CPUS))
	}
	if s.MemLimit > 0 && spec.Memory == "" {
		spec.Memory = formatBytes(int64(s.MemLimit))
	}
	gpuRequests(r, name, "gpus", s.Gpus, &spec)

	// Ports.
	for i, pc := range s.Ports {
		key := fmt.Sprintf("ports[%d]", i)
		r.rejectExtra(name, key, pc, "name", "mode", "host_ip", "target", "published", "protocol", "app_protocol")
		if pc.Name != "" || pc.AppProtocol != "" {
			r.warnf(name, key, "name and app_protocol are ignored by the builtin backend")
		}
		proto := strings.ToLower(pc.Protocol)
		if proto != "" && proto != "tcp" && proto != "udp" {
			r.errorf(name, key+".protocol", "protocol %q is not supported", pc.Protocol)
		}
		if pc.Mode != "" && pc.Mode != "ingress" && pc.Mode != "host" {
			r.errorf(name, key+".mode", "mode %q is not supported", pc.Mode)
		}
		spec.Ports = append(spec.Ports, wslc.PortBinding{HostIP: pc.HostIP, HostPort: pc.Published, ContainerPort: strconv.FormatUint(uint64(pc.Target), 10), Protocol: proto})
	}
	if len(s.Expose) > 0 {
		r.warnf(name, "expose", "ignored: containers on a shared network reach each other's ports without it")
	}

	// Mounts.
	for i, v := range s.Volumes {
		key := fmt.Sprintf("volumes[%d]", i)
		r.rejectExtra(name, key, v, "type", "source", "target", "read_only", "bind", "volume", "tmpfs", "consistency")
		if v.Consistency != "" {
			r.warnf(name, key+".consistency", "ignored on Windows")
		}
		switch v.Type {
		case types.VolumeTypeVolume:
			r.rejectExtra(name, key+".volume", v.Volume)
			if v.Source == "" {
				r.errorf(name, key, "anonymous volumes are not supported by the builtin backend; name the volume")
				continue
			}
			vc, ok := p.Volumes[v.Source]
			if !ok {
				r.errorf(name, key, "volume %q is not declared", v.Source)
				continue
			}
			usedVols[v.Source] = true
			if c, ok := meta.Volumes[v.Source]; ok && c.Mode == VolumeBind {
				spec.Mounts = append(spec.Mounts, wslc.MountSpec{Type: "bind", Source: c.Path, Target: v.Target, ReadOnly: v.ReadOnly})
				sp.BindDirs = append(sp.BindDirs, c.Path)
				continue
			}
			vn := vc.Name
			if vn == "" {
				vn = ResourceName(p.Name, v.Source)
			}
			spec.Mounts = append(spec.Mounts, wslc.MountSpec{Type: "volume", Source: vn, Target: v.Target, ReadOnly: v.ReadOnly})
		case types.VolumeTypeBind:
			if v.Bind != nil {
				r.rejectExtra(name, key+".bind", v.Bind, "create_host_path")
			}
			if !filepath.IsAbs(v.Source) {
				r.errorf(name, key+".source", "bind source %q is not an absolute Windows path", v.Source)
			}
			spec.Mounts = append(spec.Mounts, wslc.MountSpec{Type: "bind", Source: v.Source, Target: v.Target, ReadOnly: v.ReadOnly})
			if v.Bind == nil || bool(v.Bind.CreateHostPath) {
				sp.BindDirs = append(sp.BindDirs, v.Source)
			}
		case types.VolumeTypeTmpfs:
			r.rejectExtra(name, key+".tmpfs", v.Tmpfs)
			if v.ReadOnly {
				r.errorf(name, key+".read_only", "read-only tmpfs is not supported by the builtin backend")
			}
			spec.Mounts = append(spec.Mounts, wslc.MountSpec{Type: "tmpfs", Target: v.Target})
		default:
			r.errorf(name, key+".type", "volume type %q is not supported by the builtin backend", v.Type)
		}
	}
	for _, t := range s.Tmpfs {
		if strings.Contains(t, ":") {
			r.errorf(name, "tmpfs", "tmpfs options (%q) are not supported by the builtin backend", t)
			continue
		}
		spec.Tmpfs = append(spec.Tmpfs, t)
	}

	// Networks. The first network by priority is joined at create; the
	// others through `wslc network connect` (aliases only: no fixed IP).
	var extra []Attachment
	switch s.NetworkMode {
	case "":
		for i, key := range s.NetworksByPriority() {
			nc := s.Networks[key]
			path := "networks." + key
			nw, ok := p.Networks[key]
			if !ok {
				r.errorf(name, path, "network %q is not declared", key)
				continue
			}
			usedNets[key] = true
			nn := nw.Name
			if nn == "" {
				nn = ResourceName(p.Name, key)
			}
			var aliases []string
			if nc.ServiceNameAliasEnabled() {
				aliases = append(aliases, name)
			}
			if nc != nil {
				r.rejectExtra(name, path, nc, "aliases", "ipv4_address", "priority", "use_service_name_alias")
				aliases = append(aliases, nc.Aliases...)
			}
			if i == 0 {
				spec.Network = nn
				spec.Aliases = aliases
				if nc != nil {
					spec.IP = nc.Ipv4Address
				}
				continue
			}
			if nc != nil && nc.Ipv4Address != "" {
				r.errorf(name, path+".ipv4_address", "a fixed address is only supported on the service's first network")
			}
			extra = append(extra, Attachment{Network: nn, Aliases: aliases})
		}
	case "none":
		spec.Network = "none"
	default:
		r.errorf(name, "network_mode", "network_mode %q is not supported by the builtin backend", s.NetworkMode)
	}

	spec.Hostname = s.Hostname
	spec.Domainname = s.DomainName
	spec.User = s.User
	spec.WorkingDir = s.WorkingDir
	spec.StopSignal = s.StopSignal
	if s.StopGracePeriod != nil {
		sec := int(math.Ceil(time.Duration(*s.StopGracePeriod).Seconds()))
		spec.StopTimeout = &sec
	}
	if s.ShmSize > 0 {
		spec.ShmSize = formatBytes(int64(s.ShmSize))
	}
	for _, k := range sortedKeys(s.Ulimits) {
		u := s.Ulimits[k]
		if u == nil {
			continue
		}
		if u.Single != 0 {
			spec.Ulimits = append(spec.Ulimits, fmt.Sprintf("%s=%d", k, u.Single))
		} else {
			spec.Ulimits = append(spec.Ulimits, fmt.Sprintf("%s=%d:%d", k, u.Soft, u.Hard))
		}
	}
	spec.DNS = append(spec.DNS, s.DNS...)
	spec.DNSSearch = append(spec.DNSSearch, s.DNSSearch...)
	spec.DNSOptions = append(spec.DNSOptions, s.DNSOpts...)
	spec.TTY = s.Tty
	spec.Interactive = s.StdinOpen
	if h := s.HealthCheck; h != nil {
		spec.Health = translateHealth(r, name, h)
		sp.HasHealth = spec.Health != nil && !spec.Health.Disable
	}

	// Dependencies.
	for _, dep := range sortedKeys(s.DependsOn) {
		d := s.DependsOn[dep]
		path := "depends_on." + dep
		r.rejectExtra(name, path, d, "condition", "restart", "required")
		if d.Restart {
			r.warnf(name, path+".restart", "ignored: dependents are not restarted when %q is recreated", dep)
		}
		cond := d.Condition
		switch cond {
		case "":
			cond = types.ServiceConditionStarted
		case types.ServiceConditionStarted, types.ServiceConditionHealthy, types.ServiceConditionCompletedSuccessfully:
		default:
			r.errorf(name, path+".condition", "unknown condition %q", cond)
		}
		if _, ok := p.Services[dep]; !ok && d.Required {
			r.errorf(name, path, "service %q is not part of the project (check profiles)", dep)
		}
		sp.DependsOn = append(sp.DependsOn, Dependency{Service: dep, Condition: cond, Required: d.Required})
	}

	// Replicas.
	scale := s.GetScale()
	if s.ContainerName != "" && scale > 1 {
		r.errorf(name, "container_name", "container_name cannot be used with more than one replica")
	}
	if spec.IP != "" && scale > 1 {
		r.errorf(name, "networks", "a fixed ipv4_address cannot be used with more than one replica")
	}
	hash := ConfigHash(spec, extra)
	for n := 1; n <= scale; n++ {
		c := ContainerPlan{Number: n, Hash: hash, Extra: extra}
		c.Name = ContainerName(p.Name, name, n)
		if s.ContainerName != "" {
			c.Name = s.ContainerName
		}
		c.Spec = spec
		c.Spec.Name = c.Name
		c.Spec.Labels = map[string]string{}
		for k, v := range spec.Labels {
			c.Spec.Labels[k] = v
		}
		c.Spec.Labels[LabelProject] = p.Name
		c.Spec.Labels[LabelService] = name
		c.Spec.Labels[LabelNumber] = strconv.Itoa(n)
		c.Spec.Labels[LabelConfigHash] = hash
		c.Spec.Labels[LabelOneoff] = "False"
		c.Spec.Labels[LabelBackend] = string(BackendBuiltin)
		c.Spec.Labels[LabelWorkingDir] = p.WorkingDir
		c.Spec.Labels[LabelConfigFiles] = strings.Join(p.ComposeFiles, ",")
		sp.Containers = append(sp.Containers, c)
	}
	return sp
}

// translateHealth maps a Compose healthcheck to wslc's options.
// Notes: wslc takes the check as one shell command (--health-cmd), so the
// CMD form is shell-quoted; NONE and disable turn the check off.
func translateHealth(r *Report, service string, h *types.HealthCheckConfig) *wslc.Health {
	r.rejectExtra(service, "healthcheck", h, "test", "timeout", "interval", "retries", "start_period", "disable")
	if h.Disable || (len(h.Test) > 0 && h.Test[0] == "NONE") {
		return &wslc.Health{Disable: true}
	}
	hc := &wslc.Health{}
	switch {
	case len(h.Test) == 0:
		r.errorf(service, "healthcheck.test", "a healthcheck without test (inheriting the image's) is not supported by the builtin backend")
		return nil
	case h.Test[0] == "CMD-SHELL":
		hc.Cmd = strings.Join(h.Test[1:], " ")
	case h.Test[0] == "CMD":
		hc.Cmd = shellJoin(h.Test[1:])
	default:
		r.errorf(service, "healthcheck.test", "test must start with CMD, CMD-SHELL or NONE")
		return nil
	}
	if hc.Cmd == "" {
		r.errorf(service, "healthcheck.test", "the test command is empty")
	}
	dur := func(d *types.Duration) string {
		if d == nil {
			return ""
		}
		return time.Duration(*d).String()
	}
	hc.Interval, hc.Timeout, hc.StartPeriod = dur(h.Interval), dur(h.Timeout), dur(h.StartPeriod)
	if h.Retries != nil {
		hc.Retries = int(*h.Retries)
	}
	return hc
}

// gpuRequests maps GPU device requests to --gpus all, the only form wslc has.
func gpuRequests(r *Report, service, key string, reqs []types.DeviceRequest, spec *wslc.ContainerSpec) {
	for i, d := range reqs {
		path := fmt.Sprintf("%s[%d]", key, i)
		r.rejectExtra(service, path, d, "capabilities", "driver", "count")
		gpu := false
		for _, c := range d.Capabilities {
			if c == "gpu" {
				gpu = true
			}
		}
		if key != "gpus" && !gpu {
			r.errorf(service, path, "only GPU device requests are supported by the builtin backend")
			continue
		}
		if d.Count != 0 && d.Count != -1 {
			r.errorf(service, path+".count", "only all GPUs can be requested (wslc --gpus all)")
			continue
		}
		spec.GPUs = "all"
	}
}

// shellJoin quotes argv for a POSIX shell.
func shellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		if a != "" && strings.Trim(a, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:=@%+,") == "" {
			q[i] = a
		} else {
			q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(q, " ")
}

// formatBytes renders a byte count in wslc's size syntax (e.g. 512M, 1G),
// using the largest unit that divides it exactly.
func formatBytes(n int64) string {
	for _, u := range []struct {
		s string
		v int64
	}{{"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10}} {
		if n%u.v == 0 {
			return strconv.FormatInt(n/u.v, 10) + u.s
		}
	}
	return strconv.FormatInt(n, 10)
}

// formatCPUs renders a CPU count without trailing zeros.
func formatCPUs(f float64) string {
	return strconv.FormatFloat(math.Round(f*1000)/1000, 'f', -1, 64)
}

// hashInput is what the config hash covers.
type hashInput struct {
	Spec  wslc.ContainerSpec
	Extra []Attachment
}

// ConfigHash returns the SHA-256 of the canonical JSON of a service's
// container spec and extra networks.
// Notes: Name and WSLPilot's own labels are excluded, so all replicas of a
// service share the hash and adding a Compose file that changes nothing
// recreates nothing. encoding/json sorts map keys, which makes it canonical.
func ConfigHash(spec wslc.ContainerSpec, extra []Attachment) string {
	spec.Name = ""
	labels := map[string]string{}
	for k, v := range spec.Labels {
		if !reservedLabel(k) || k == LabelRestart {
			labels[k] = v
		}
	}
	spec.Labels = labels
	b, _ := json.Marshal(hashInput{Spec: spec, Extra: extra})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// topoOrder sorts services so each comes after the ones it depends on.
// Returns: an error naming the services in a dependency cycle.
// Notes: ties are broken by name, so the order is deterministic.
func topoOrder(services types.Services) ([]string, error) {
	indeg := map[string]int{}
	dependents := map[string][]string{}
	for name, s := range services {
		if _, ok := indeg[name]; !ok {
			indeg[name] = 0
		}
		for dep := range s.DependsOn {
			if _, ok := services[dep]; !ok {
				continue
			}
			indeg[name]++
			dependents[dep] = append(dependents[dep], name)
		}
	}
	var ready, order []string
	for n, d := range indeg {
		if d == 0 {
			ready = append(ready, n)
		}
	}
	sort.Strings(ready)
	for len(ready) > 0 {
		n := ready[0]
		ready = ready[1:]
		order = append(order, n)
		for _, m := range dependents[n] {
			indeg[m]--
			if indeg[m] == 0 {
				ready = append(ready, m)
				sort.Strings(ready)
			}
		}
	}
	if len(order) != len(indeg) {
		var cyc []string
		for n, d := range indeg {
			if d > 0 {
				cyc = append(cyc, n)
			}
		}
		sort.Strings(cyc)
		return nil, fmt.Errorf("dependency cycle between %s", strings.Join(cyc, ", "))
	}
	return order, nil
}

// sortedKeys returns a map's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
