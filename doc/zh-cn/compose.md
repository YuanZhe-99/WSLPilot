# Compose

`internal/compose` 在 WSL Containers 上运行 Compose 项目。每个项目绑定一个 Backend；builtin Backend
通过 `internal/wslc` 适配层把服务翻译为 `wslc` 容器、网络和 Volume。用户的 Compose 文件留在原处，
从不改写。

## Backend 接口

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

约定：

- 每个方法都从上游读回状态（`wslc` 对象上的 label）；Backend 不缓存任何状态。
- `Validate` 返回 `Report{Errors, Warnings []Issue{Service, Key, Message}}`。其 error 返回值只用于
  加载或上游失败。只要 `Errors` 非空，`Up` 就返回 `*ValidationError` 且不做任何改动。
- `Up` 是幂等的：文件未变时再次运行不会创建、重建或删除任何东西。
- `Down` 删除带有该项目 label 的容器和网络；只有设置 `DownOptions.RemoveVolumes` 时才删除命名
  Volume（调用方须先确认）。
- 进度通过 `Progress func(Event{Service, Step, Message})` 上报；取消 `ctx` 会在 `wslc` 调用之间和
  调用期间停止操作。
- `Logs` 逐行合并各服务的日志，每行以 `<container> | ` 为前缀；设置 `Follow` 时在 `ctx` 结束后返回。
- native Backend（`Native`）是桩实现：`Detect` 报告能力探测结果（`wslc.HasNativeCompose`，在
  WSLc 3.0.3.0 上为 false），其余方法都返回 `ErrNativeUnavailable`。

## 项目元数据

每个项目由 `internal/store` 保存在数据目录下的 `projects/<id>/project.json`。文档是 store 的信封
格式，schema 版本为 1：

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

| 字段 | 含义 |
|---|---|
| `id` | 16–64 位小写十六进制数字；作为元数据目录名 |
| `name` | Compose 项目名：小写字母、数字、`-`、`_`；以字母或数字开头 |
| `backend` | `builtin` 或 `native`；只能通过 `Migrate` 改变 |
| `files` | Compose 文件的绝对路径，按顺序合并；从不复制 |
| `envFiles` | 用于变量插值的 env 文件绝对路径；为空时使用存在的 `<workingDir>\.env` |
| `profiles` | 启用的 Compose profile |
| `workingDir` | 项目目录；为空表示第一个文件所在的文件夹 |
| `autostart` | 由 WSLPilot agent 在登录时启动项目 |
| `volumes` | 按 Compose Volume 键：`native`（`wslc` Volume，默认）或带 Windows 绝对 `path` 的 `bind` |

加载使用 compose-go v2（Apache-2.0）：按顺序合并文件，解析 `include` 和 `extends`，先从操作系统环境
变量、再从 env 文件插值（与 Docker Compose 一样以 shell 为准），并应用 profile。`env_file` 与
`label_file` 在加载时合并进 `environment` 和 `labels`。只读取本地文件。

## 命名与 label

| 对象 | 名称 | Label |
|---|---|---|
| 容器 | `<project>-<service>-<n>`（设置了 `container_name` 且服务只有一个副本时用它） | `com.docker.compose.project`、`com.docker.compose.service`、`com.docker.compose.container-number`、`com.docker.compose.config-hash`、`com.docker.compose.project.config_files`、`com.docker.compose.project.working_dir`、`com.docker.compose.oneoff=False`、`io.wslpilot.backend=builtin`、`io.wslpilot.restart` |
| 网络 | `<project>_<key>`（默认网络 `<project>_default`），或网络的 `name` | `com.docker.compose.project`、`com.docker.compose.network`、`io.wslpilot.backend` |
| Volume | `<project>_<key>`，或 Volume 的 `name` | `com.docker.compose.project`、`com.docker.compose.volume`、`io.wslpilot.backend` |
| 构建的镜像 | 服务的 `image`，否则为 `<project>-<service>` | `build.labels` |

只创建有服务使用的网络和 Volume。Compose 文件中以 `com.docker.compose.` 或 `io.wslpilot.` 开头的
label 属于校验错误。

config hash 是服务的 `wslc.ContainerSpec` 及其额外网络连接的规范 JSON 的 SHA-256，不含容器名和
WSLPilot 自己的 label（restart label 保留）。所有副本共用同一个 hash；增加一个不改变任何内容的
Compose 文件不会重建任何容器。

## 规划与调和

1. 服务按 `depends_on` 排序（同级按名称）；存在环属于校验错误。
2. 用 `--filter label=com.docker.compose.project=<name>` 找到已有容器，按服务和容器编号匹配。
3. config hash、名称或镜像 ID 不同的容器会被重建（停止、删除、创建）；已停止的匹配容器会被启动；
   运行中的匹配容器保持不变。
4. 超出 `scale` / `deploy.replicas` 的副本会被删除。已不在文件中的服务的容器是孤儿容器：会被报告，
   只有设置 `UpOptions.RemoveOrphans` 时才删除。
5. 每个服务启动前先等待其依赖满足条件（默认两分钟）：`service_started` 无需额外等待，
   `service_healthy` 轮询 `inspect` 直到 `State.Health.Status` 为 `healthy`（`unhealthy` 即失败），
   `service_completed_successfully` 等待退出码 0。

镜像遵循 `pull_policy`：`missing`（默认）只在缺失时拉取或构建，`always` 总是拉取，`never` 缺失时
失败，`build` 总是构建。`UpOptions.Build` 会重新构建带 `build` 的服务。

## 支持的键

| 键 | 翻译方式 |
|---|---|
| `image`、`pull_policy` | 镜像引用；`missing`、`if_not_present`、`always`、`never`、`build` |
| `build` | `wslc image build`：`context`、`dockerfile`、`args`、`target`、`labels`、`tags`、`pull`、`no_cache` |
| `command`、`entrypoint` | 见下方 entrypoint 规则 |
| `environment`、`env_file` | `--env KEY=VALUE`，已排序；env 文件由加载器合并 |
| `labels`、`label_file` | `--label` |
| `ports` | `--publish [host_ip:][published:]target[/udp]`；只支持 `tcp` 和 `udp` |
| `volumes` | 命名 Volume（`--volume <project>_<key>:target[:ro]`）、Bind Mount（Windows 绝对路径；缺失的文件夹会被创建，除非 `create_host_path: false`）、tmpfs |
| `networks` | 按优先级的第一个网络在创建时通过 `--network`、`--network-alias`（服务名加 `aliases`）和 `--ip`（`ipv4_address`）加入；其余网络用 `wslc network connect` |
| `network_mode` | 只能不设置或为 `none` |
| `hostname`、`domainname`、`user`、`working_dir` | `--hostname`、`--domainname`、`--user`、`--workdir` |
| `stop_signal`、`stop_grace_period` | `--stop-signal`、`--stop-timeout`（秒，向上取整） |
| `shm_size`、`ulimits` | `--shm-size`、`--ulimit name=soft:hard` |
| `dns`、`dns_search`、`dns_opt`、`tmpfs` | `--dns`、`--dns-search`、`--dns-option`、`--tmpfs`（仅路径） |
| `healthcheck` | 见下方 healthcheck 规则 |
| `cpus`、`mem_limit`、`deploy.resources.limits` | `--cpus`、`--memory`（如 `64M`） |
| `gpus`、`deploy.resources.*.devices` | 请求全部 GPU 设备时为 `--gpus all` |
| `tty`、`stdin_open` | `--tty`、`--interactive` |
| `restart`、`deploy.restart_policy.condition` | Label `io.wslpilot.restart=<policy>` |
| `depends_on` | 条件 `service_started`、`service_healthy`、`service_completed_successfully`；`required` |
| `scale`、`deploy.replicas`、`deploy.mode: replicated` | 容器从 1 开始编号 |
| `container_name`、`profiles`、`extends`、`attach` | 由加载器或命名规则处理 |

警告（会报告，但从不阻止）：`expose`（同一网络上的容器本来就能互访）、端口的 `name` /
`app_protocol`、Volume 的 `consistency`、`depends_on.restart`、网络的 `attachable`，以及没有服务
使用的顶层 `secrets` / `configs`。

## 不支持的键

以下键一旦设置即为校验错误：`privileged`、`cap_add`、`cap_drop`、`devices`、
`device_cgroup_rules`、`none` 以外的 `network_mode`、`pid`、`ipc`、`uts`、`userns_mode`、
`cgroup`、`cgroup_parent`、`secrets`、`configs`、`sysctls`、`extra_hosts`、`links`、
`external_links`、`security_opt`、`platform`、`init`、`logging`、`log_driver`、`log_opt`、
`mac_address`、`read_only`、`runtime`、`isolation`、`storage_opt`、`oom_kill_disable`、
`oom_score_adj`、`pids_limit`、`mem_reservation`、`memswap_limit`、`mem_swappiness`、`cpu_count`、
`cpu_percent`、`cpu_period`、`cpu_quota`、`cpu_rt_period`、`cpu_rt_runtime`、`cpu_shares`、
`cpuset`、`blkio_config`、`credential_spec`、`group_add`、`umask`、`use_api_socket`、
`volume_driver`、`volumes_from`、`annotations`、`models`、`provider`、`develop`、`pre_start`、
`post_start`、`pre_stop`、`pull_refresh_after`、`net`、`dockerfile`；以及顶层的 `models` 和 `jobs`。

部分支持的键以同样方式报告其不支持的部分：匿名 Volume，Bind Mount 的 `propagation` / `selinux` /
`recursive`，Volume 的 `nocopy` / `subpath`，tmpfs 的 `size` / `mode`，`tmpfs` 选项，清空的
`command` 或 `entrypoint`（`[]`），`healthcheck.start_interval`，没有 `test` 的 healthcheck，带
`count` 或 `device_ids` 的 GPU 请求，其他设备请求，`deploy.placement`，`deploy.update_config`，
`deploy.resources.reservations` 的 cpus 和 memory，`deploy.resources.limits.pids`，
`deploy.mode: global`，第二个网络上或多副本时的 `ipv4_address`，上面列出以外的 `pull_policy` 值，
未知的 `restart` 值，以及上面未列出的 `build` 键。顶层网络：`bridge` 以外的驱动、`driver_opts`、
IPAM 驱动和选项、多于一个地址池、IPv6。顶层 Volume：`local`、`guest` 和 `vhd`（需要
`driver_opts.SizeBytes`）以外的驱动，以及 `local` 驱动的选项。外部网络和 Volume 必须已存在；
`Validate` 会检查。

compose-go 认识而本清单未归类的键会被报告为 "not known to this WSLPilot version"。一个单元测试会
遍历 compose-go 的 `ServiceConfig`，任何键不在两个清单之一时即失败。

## 映射规则

- **Entrypoint。** `wslc --entrypoint` 只接受一个可执行文件。entrypoint `[a, b, c]` 变为
  `--entrypoint a`，`b c` 放在 command 的各词之前。Docker 覆盖 entrypoint 时同样会丢弃镜像的
  `CMD`，因此结果等价。
- **Restart。** WSLc 没有 `--restart`。策略（`no`、`always`、`unless-stopped`、`on-failure[:N]`；
  未设置 `restart` 时取 `deploy.restart_policy.condition` 的 `none` / `on-failure` / `any`）保存为
  label `io.wslpilot.restart`，由 WSLPilot agent 在 `die` 事件时重启容器。
- **Healthcheck。** `wslc` 只接受一条 shell 命令（`--health-cmd`）。`CMD-SHELL x` 直接传 `x`；
  `CMD a b` 经 shell 引用后拼为 `a b`；`NONE` 或 `disable: true` 对应 `--no-healthcheck`。时长按 Go
  语法书写（`1m30s`、`500ms`）。
- **Windows 上的 Volume 短语法。** compose-go 会把 `v:/data` 这样的单字母来源当作盘符；请使用更长的
  Volume 名。

## 迁移

`Migrate(ctx, registry, projects, project, to, confirm, progress)` 是改变项目 Backend 的唯一途径，
界面只在用户确认后调用它。任何情况都不会自动切换 Backend，包括 WSLPilot 或 WSLc 更新。

1. 没有 `confirm` 时返回 `ErrNotConfirmed`，不做任何改动。
2. 目标 Backend 必须可用（`Detect`）且接受该项目（`Validate`）。
3. 当前 Backend 执行 `Down` 并保留 Volume，然后目标 Backend 执行 `Up`。
4. `Up` 失败时，目标 Backend 执行 `Down`（保留 Volume），旧 Backend 重新执行 `Up`；元数据保留旧
   Backend。
5. 只有成功后才把新 Backend 写入 `project.json`。
