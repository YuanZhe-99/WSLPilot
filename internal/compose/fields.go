package compose

import (
	"reflect"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
)

// serviceSupported lists the service keys the builtin backend translates
// (some only partly; translate reports the unsupported parts).
// Notes: TestServiceFieldsClassified fails when compose-go gains a key that
// is in neither list, so a new key is classified before it can be used.
var serviceSupported = keySet(
	"name", "profiles", "deploy", "restart", "scale", "attach", "container_name", "extends",
	"command", "cpus", "dns", "dns_opt", "dns_search", "domainname", "entrypoint",
	"environment", "env_file", "gpus", "hostname", "image", "labels", "label_file",
	"mem_limit", "network_mode", "networks", "pull_policy", "shm_size", "stop_grace_period",
	"stop_signal", "tmpfs", "ulimits", "user", "volumes", "working_dir",
	"build", "depends_on", "expose", "healthcheck", "ports", "stdin_open", "tty",
)

// serviceUnsupported lists the service keys the builtin backend cannot
// honor; a non-empty value is a validation error.
var serviceUnsupported = keySet(
	"provider", "external_links", "links", "net", "pre_start", "post_start", "pre_stop", "develop",
	"annotations", "blkio_config", "cap_add", "cap_drop", "cgroup_parent", "cgroup",
	"cpu_count", "cpu_percent", "cpu_period", "cpu_quota", "cpu_rt_period", "cpu_rt_runtime",
	"cpu_shares", "cpuset", "configs", "credential_spec", "device_cgroup_rules", "devices",
	"extra_hosts", "group_add", "init", "ipc", "isolation", "logging", "log_driver", "log_opt",
	"mem_reservation", "memswap_limit", "mem_swappiness", "mac_address", "models",
	"oom_kill_disable", "oom_score_adj", "pid", "pids_limit", "platform", "privileged",
	"pull_refresh_after", "read_only", "runtime", "secrets", "security_opt", "storage_opt",
	"sysctls", "umask", "use_api_socket", "userns_mode", "uts", "volume_driver", "volumes_from",
	"dockerfile",
)

// keySet builds a set of keys.
func keySet(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// field is one Compose key of a struct, found by reflection.
type field struct {
	Key   string
	Value reflect.Value
}

// extensionsType is compose-go's x-* extension map; it is never a key.
var extensionsType = reflect.TypeOf(types.Extensions{})

// fields returns the Compose keys of a struct value (embedded structs with
// an inline yaml tag are flattened), sorted by key.
// Notes: fields tagged yaml:"-" are internal to compose-go and skipped, as
// is the x-* extension map.
func fields(v reflect.Value) []field {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	var res []field
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() || sf.Type == extensionsType {
			continue
		}
		tag := sf.Tag.Get("yaml")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if sf.Anonymous && strings.Contains(opts, "inline") {
			res = append(res, fields(v.Field(i))...)
			continue
		}
		if name == "" {
			name = strings.ToLower(sf.Name)
		}
		res = append(res, field{Key: name, Value: v.Field(i)})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Key < res[j].Key })
	return res
}

// isSet reports whether a field holds a value, honoring compose-go's own
// IsZero methods (OptOut is "zero" when true, ShellCommand when nil).
func isSet(v reflect.Value) bool {
	if z, ok := v.Interface().(interface{ IsZero() bool }); ok {
		if v.Kind() == reflect.Pointer && v.IsNil() {
			return false
		}
		return !z.IsZero()
	}
	return !v.IsZero()
}

// extraKeys returns the set keys of v that are not in allowed.
// Inputs: v is a compose-go struct or pointer; allowed are its keys the
// caller translates.
func extraKeys(v any, allowed ...string) []string {
	ok := keySet(allowed...)
	var res []string
	for _, f := range fields(reflect.ValueOf(v)) {
		if !ok[f.Key] && isSet(f.Value) {
			res = append(res, f.Key)
		}
	}
	return res
}

// rejectExtra reports every set key of v outside allowed as an error under
// the key path prefix.
func (r *Report) rejectExtra(service, prefix string, v any, allowed ...string) {
	for _, k := range extraKeys(v, allowed...) {
		r.errorf(service, joinKey(prefix, k), "not supported by the builtin backend")
	}
}

// joinKey joins Compose key path parts with dots.
func joinKey(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, ".")
}

// checkServiceKeys reports the service's unsupported and unknown keys.
// Notes: an unknown key (a compose-go addition not yet classified) is an
// error too, so nothing new is silently ignored.
func checkServiceKeys(r *Report, s *types.ServiceConfig) {
	for _, f := range fields(reflect.ValueOf(s)) {
		if serviceSupported[f.Key] || !isSet(f.Value) {
			continue
		}
		if serviceUnsupported[f.Key] {
			r.errorf(s.Name, f.Key, "not supported by the builtin backend")
		} else {
			r.errorf(s.Name, f.Key, "not known to this WSLPilot version")
		}
	}
}
