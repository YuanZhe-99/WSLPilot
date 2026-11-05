# Compose

`internal/compose` runs Compose projects on WSL Containers. A project is bound to one backend; the
builtin backend translates services into `wslc` containers, networks and volumes through the
`internal/wslc` adapter. The user's Compose files stay where they are and are never rewritten.

## Backend interface

```go
type Backend interface {
	ID() BackendID // "builtin" | "native"
	Detect(ctx context.Context) (Availability, error)
	Validate(ctx context.Context, p *Project) (Report, error)
	Up(ctx context.Context, p *Project, o UpOptions, progress Progress) error
	Down(ctx context.Context, p *Project, o DownOptions, progress Progress) error
	Start(ctx context.Context, p *Project, services []string, progress Progress) error
	Stop(ctx context.Context, p *Project, services []string, progress Progress) error
	Restart(ctx context.Context, p *Project, services []string, progress Progress) error
	Ps(ctx context.Context, p *Project) ([]ContainerStatus, error)
	Logs(ctx context.Context, p *Project, o LogsOptions, w io.Writer) error
}
```

Contract:

- Every method reads state back from upstream (labels on `wslc` objects); a backend keeps no cache.
- `Validate` returns a `Report{Errors, Warnings []Issue{Service, Key, Message}}`. Its error result
  is only for load or upstream failures. `Up` returns `*ValidationError` and changes nothing while
  `Errors` is non-empty.
- `Up` is idempotent: a second run with unchanged files creates, recreates and removes nothing.
- `Down` removes containers and networks labelled with the project; named volumes only with
  `DownOptions.RemoveVolumes` (the caller confirms first).
- Progress arrives through `Progress func(Event{Service, Step, Message})`; cancelling `ctx` stops
  the operation between and during `wslc` calls.
- `Logs` merges the services' logs line by line, each prefixed with `<container> | `; with `Follow`
  it returns when `ctx` ends.
- The native backend (`Native`) is a stub: `Detect` reports the capability probe
  (`wslc.HasNativeCompose`, false on WSLc 3.0.3.0) and every other method returns
  `ErrNativeUnavailable`.

## Project metadata

Each project is stored by `internal/store` as `projects/<id>/project.json` under the data
directory. The document is the store envelope with schema version 1:

```json
{
  "schemaVersion": 1,
  "data": {
    "schemaVersion": 1,
    "id": "3f9c0e2a7b1d4c55",
    "name": "my-app",
    "backend": "builtin",
    "files": ["C:\\Users\\me\\src\\my-app\\compose.yaml"],
    "envFiles": ["C:\\Users\\me\\src\\my-app\\.env.local"],
    "profiles": ["debug"],
    "workingDir": "C:\\Users\\me\\src\\my-app",
    "autostart": false,
    "volumes": {"data": {"mode": "bind", "path": "D:\\my-app-data"}}
  }
}
```

| Field | Meaning |
|---|---|
| `id` | 16–64 lowercase hex digits; names the metadata directory |
| `name` | Compose project name: lowercase letters, digits, `-`, `_`; starts with a letter or digit |
| `backend` | `builtin` or `native`; changes only through `Migrate` |
| `files` | Absolute paths of the Compose files, merged in order; never copied |
| `envFiles` | Absolute paths of env files for interpolation; empty uses `<workingDir>\.env` when present |
| `profiles` | Active Compose profiles |
| `workingDir` | Project directory; empty means the folder of the first file |
| `autostart` | The WSLPilot agent brings the project up at sign-in |
| `volumes` | Per Compose volume key: `native` (a `wslc` volume, the default) or `bind` with an absolute Windows `path` |

Loading uses compose-go v2 (Apache-2.0): files are merged in order, `include` and `extends` are
resolved, variables are interpolated from the OS environment first and then the env files (the
shell wins, as in Docker Compose), and profiles are applied. `env_file` and `label_file` are merged
into `environment` and `labels` at load time. Only local files are read.

## Naming and labels

| Object | Name | Labels |
|---|---|---|
| Container | `<project>-<service>-<n>` (`container_name` when set and the service has one replica) | `com.docker.compose.project`, `com.docker.compose.service`, `com.docker.compose.container-number`, `com.docker.compose.config-hash`, `com.docker.compose.project.config_files`, `com.docker.compose.project.working_dir`, `com.docker.compose.oneoff=False`, `io.wslpilot.backend=builtin`, `io.wslpilot.restart` |
| Network | `<project>_<key>` (default network `<project>_default`), or the network's `name` | `com.docker.compose.project`, `com.docker.compose.network`, `io.wslpilot.backend` |
| Volume | `<project>_<key>`, or the volume's `name` | `com.docker.compose.project`, `com.docker.compose.volume`, `io.wslpilot.backend` |
| Built image | the service's `image`, else `<project>-<service>` | the `build.labels` |

Only networks and volumes a service uses are created. Labels starting with `com.docker.compose.` or
`io.wslpilot.` in a Compose file are validation errors.

The config hash is the SHA-256 of the canonical JSON of the service's `wslc.ContainerSpec` and its
extra network attachments, with the container name and WSLPilot's own labels left out (the restart
label stays in). All replicas share it, and adding a Compose file that changes nothing recreates
nothing.

## Planning and reconciliation

1. Services are sorted by `depends_on` (ties by name); a cycle is a validation error.
2. Existing containers are found with `--filter label=com.docker.compose.project=<name>` and
   matched by service and container number.
3. A match with a different config hash, name or image ID is recreated (stop, remove, create);
   a stopped match is started; a running match is kept.
4. Replicas beyond `scale` / `deploy.replicas` are removed. Containers of services no longer in the
   files are orphans: reported, and removed only with `UpOptions.RemoveOrphans`.
5. Before each service, its dependencies' conditions are awaited (default two minutes):
   `service_started` needs nothing more, `service_healthy` polls `inspect` until
   `State.Health.Status` is `healthy` (`unhealthy` fails), `service_completed_successfully` waits
   for exit code 0.

Images follow `pull_policy`: `missing` (default) pulls or builds only when absent, `always` pulls,
`never` fails when absent, `build` always builds. `UpOptions.Build` rebuilds services with `build`.

## Supported keys

| Key | Translation |
|---|---|
| `image`, `pull_policy` | Image reference; `missing`, `if_not_present`, `always`, `never`, `build` |
| `build` | `wslc image build`: `context`, `dockerfile`, `args`, `target`, `labels`, `tags`, `pull`, `no_cache` |
| `command`, `entrypoint` | See the entrypoint rule below |
| `environment`, `env_file` | `--env KEY=VALUE`, sorted; env files are merged by the loader |
| `labels`, `label_file` | `--label` |
| `ports` | `--publish [host_ip:][published:]target[/udp]`; `tcp` and `udp` only |
| `volumes` | Named (`--volume <project>_<key>:target[:ro]`), bind (absolute Windows path; missing folders are created unless `create_host_path: false`), tmpfs |
| `networks` | First network by priority at create with `--network`, `--network-alias` (service name plus `aliases`) and `--ip` (`ipv4_address`); further networks with `wslc network connect` |
| `network_mode` | Only unset or `none` |
| `hostname`, `domainname`, `user`, `working_dir` | `--hostname`, `--domainname`, `--user`, `--workdir` |
| `stop_signal`, `stop_grace_period` | `--stop-signal`, `--stop-timeout` (seconds, rounded up) |
| `shm_size`, `ulimits` | `--shm-size`, `--ulimit name=soft:hard` |
| `dns`, `dns_search`, `dns_opt`, `tmpfs` | `--dns`, `--dns-search`, `--dns-option`, `--tmpfs` (path only) |
| `healthcheck` | See the healthcheck rule below |
| `cpus`, `mem_limit`, `deploy.resources.limits` | `--cpus`, `--memory` (e.g. `64M`) |
| `gpus`, `deploy.resources.*.devices` | `--gpus all` for GPU requests of all devices |
| `tty`, `stdin_open` | `--tty`, `--interactive` |
| `restart`, `deploy.restart_policy.condition` | Label `io.wslpilot.restart=<policy>` |
| `depends_on` | Conditions `service_started`, `service_healthy`, `service_completed_successfully`; `required` |
| `scale`, `deploy.replicas`, `deploy.mode: replicated` | Containers numbered from 1 |
| `container_name`, `profiles`, `extends`, `attach` | Applied by the loader or naming |

Warnings (reported, never block): `expose` (containers on a shared network reach each other
anyway), port `name` / `app_protocol`, volume `consistency`, `depends_on.restart`, network
`attachable`, and top-level `secrets` / `configs` that no service uses.

## Unsupported keys

Each of these is a validation error when set: `privileged`, `cap_add`, `cap_drop`, `devices`,
`device_cgroup_rules`, `network_mode` other than `none`, `pid`, `ipc`, `uts`, `userns_mode`,
`cgroup`, `cgroup_parent`, `secrets`, `configs`, `sysctls`, `extra_hosts`, `links`,
`external_links`, `security_opt`, `platform`, `init`, `logging`, `log_driver`, `log_opt`,
`mac_address`, `read_only`, `runtime`, `isolation`, `storage_opt`, `oom_kill_disable`,
`oom_score_adj`, `pids_limit`, `mem_reservation`, `memswap_limit`, `mem_swappiness`, `cpu_count`,
`cpu_percent`, `cpu_period`, `cpu_quota`, `cpu_rt_period`, `cpu_rt_runtime`, `cpu_shares`,
`cpuset`, `blkio_config`, `credential_spec`, `group_add`, `umask`, `use_api_socket`,
`volume_driver`, `volumes_from`, `annotations`, `models`, `provider`, `develop`, `pre_start`,
`post_start`, `pre_stop`, `pull_refresh_after`, `net`, `dockerfile`; top-level `models` and `jobs`.

Partly supported keys report their unsupported parts the same way: anonymous volumes, bind
`propagation` / `selinux` / `recursive`, volume `nocopy` / `subpath`, tmpfs `size` / `mode`, `tmpfs`
options, cleared `command` or `entrypoint` (`[]`), `healthcheck.start_interval`, a healthcheck
without `test`, GPU requests with a `count` or `device_ids`, other device requests,
`deploy.placement`, `deploy.update_config`, `deploy.resources.reservations` cpus and memory,
`deploy.resources.limits.pids`, `deploy.mode: global`, `ipv4_address` on a second network or with
more than one replica, `pull_policy` values other than those above, unknown `restart` values, and
the `build` keys not listed above. Top-level networks: drivers other than `bridge`, `driver_opts`,
IPAM drivers and options, more than one pool, IPv6. Top-level volumes: drivers other than `local`,
`guest` and `vhd` (which needs `driver_opts.SizeBytes`), and `local` driver options. External
networks and volumes must exist; `Validate` checks them.

A key compose-go knows but this list does not classify is reported as "not known to this WSLPilot
version". A unit test walks compose-go's `ServiceConfig` and fails when a key is in neither list.

## Mapping rules

- **Entrypoint.** `wslc --entrypoint` takes one executable. An entrypoint `[a, b, c]` becomes
  `--entrypoint a` with `b c` placed before the command's words. Docker also drops the image's
  `CMD` when the entrypoint is overridden, so the result is equivalent.
- **Restart.** WSLc has no `--restart`. The policy (`no`, `always`, `unless-stopped`,
  `on-failure[:N]`; `deploy.restart_policy.condition` `none` / `on-failure` / `any` when `restart`
  is unset) is stored as the label `io.wslpilot.restart`, and the WSLPilot agent restarts
  containers on `die` events.
- **Healthcheck.** `wslc` takes one shell command (`--health-cmd`). `CMD-SHELL x` passes `x`;
  `CMD a b` is shell-quoted into `a b`; `NONE` or `disable: true` gives `--no-healthcheck`.
  Durations are written in Go syntax (`1m30s`, `500ms`).
- **Short volume syntax on Windows.** compose-go reads a one-letter source such as `v:/data` as a
  drive letter; use longer volume names.

## Migration

`Migrate(ctx, registry, projects, project, to, confirm, progress)` is the only way a project's
backend changes, and the UI calls it only after the user confirmed. Nothing switches a backend
automatically, including WSLPilot or WSLc updates.

1. Without `confirm` it returns `ErrNotConfirmed` and touches nothing.
2. The target must be available (`Detect`) and accept the project (`Validate`).
3. The current backend runs `Down` keeping volumes, then the target runs `Up`.
4. When `Up` fails, the target runs `Down` (volumes kept) and the old backend runs `Up` again; the
   metadata keeps the old backend.
5. Only after success is the new backend saved to `project.json`.
