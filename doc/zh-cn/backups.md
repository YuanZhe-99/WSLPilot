# 备份、恢复与计划

`internal/backup` 包以 **tar 流**为单位工作：每个来源产生一个 tar 流，每种备份保存一个 tar 流，每次恢复消费一个 tar 流。
因此 Linux 元数据（属主、权限、链接、设备、xattr）始终在 tar 内传输，从不依赖 Windows 目录。增量仓库格式另见
[backup-format.md](backup-format.md)。

## 来源

| 来源 | 类型字符串 | tar 流来自 | 一致性 |
|---|---|---|---|
| WSL 发行版 | `wsl-distro` | `wsl --export <name> -` | 先终止，结束后再启动；用户选择运行中备份时除外 |
| WSLc Volume | `wslc-volume` | 辅助容器：`tar -cpf - --numeric-owner -C /src .` | 挂载它的运行中容器会被停止并在结束后启动；运行中备份时除外 |
| Compose 项目 | `compose-project` | 项目的 Compose 文件与 env 文件（Windows 文件），以及每个原生命名 Volume 各一个备份项 | Volume 同上 |

运行中备份会被标注：sidecar 与目录元数据中为 `consistent: false`，仓库快照带 `inconsistent` 标签。界面显示为“运行中备份”。

## Volume tar 管道

`internal/volume` 启动一个挂载目标 Volume 的辅助容器（`HelperImage`，label `io.wslpilot.helper=1`），通过
`wslc exec -i` 传输 tar 流（二进制安全，已在 WSLc 3.0.3.0 上验证）。导入接受 `.tar`、`.tar.gz` 与 `.tar.zst`，
按魔数识别并在 Windows 侧解压；拒绝 `.tar.xz`。导入会统计流经的条目数以及导入后 Volume 中的条目数。辅助容器中的
busybox tar 不写 xattr，因此 Volume 导出不含 xattr。残留的辅助容器由 `CleanupHelpers` 删除。

## 目录备份（格式 `wslpilot-folder`，版本 1）

```
<dir>/<kind>-<name>-<YYYYMMDD-HHMMSS>/
  .wslpilot/folder.json      metadata, written with "complete": false first
  .wslpilot/manifest.jsonl   one entry per tar entry, in tar order
  files/…                    regular files and directories, browsable in Explorer
```

`folder.json` 包含 `format`、`version`、`source {kind, name, host}`、`created`（RFC 3339 UTC）、`consistent`、
`plan`、`tool`、`entries`、`contentBytes` 与 `complete`。

manifest 每行是一个 JSON 对象：

| 字段 | 含义 |
|---|---|
| `name` | tar 名称；字节串（见下） |
| `type` | `file`、`dir`、`symlink`、`hardlink`、`char`、`block`、`fifo` |
| `mode`、`uid`、`gid`、`uname`、`gname` | 与 tar 头相同 |
| `mtime` | 带纳秒的 RFC 3339 |
| `linkname`、`devmajor`、`devminor` | 用于链接与设备 |
| `size`、`sha256`、`stored` | 普通文件：内容大小、SHA-256（十六进制）与 `files/` 下的路径 |
| `pax` | PAX 记录（xattr），值为字节串 |

字节串在是合法 UTF-8 时为 JSON 字符串，否则为 `{"base64": "…"}`。

`stored` 路径按 Windows 规则逐段编码：控制字符、`<>:"\|?*%`、非法 UTF-8 以及末尾的点或空格变为 `%XX`；保留设备名
（`CON`、`NUL`、`COM1`…）前加 `%`；与之前某个路径仅大小写不同的路径加后缀 `%~N`。

恢复时按 manifest 顺序重建 tar，从 `files/` 读取内容并校验每个 SHA-256。`complete` 为 false 的备份不提供恢复。

## 归档备份（格式 `wslpilot-archive`，版本 1）

单个文件 `<kind>-<name>-<time>.tar`、`.tar.gz` 或 `.tar.zst`，先写为 `.partial`，完成后重命名，旁边放置 sidecar
`<archive>.wpmeta.json`：

| 字段 | 含义 |
|---|---|
| `format`、`version` | `wslpilot-archive`、1 |
| `source`、`created`、`consistent`、`plan`、`tool` | 与目录备份相同 |
| `compression` | `none`、`gzip`、`zstd`（内容也会按魔数识别） |
| `size`、`sha256` | 归档文件的大小与哈希 |
| `entries`、`contentBytes` | 其中 tar 的条目数与内容字节数 |

校验归档时比较 SHA-256 与条目数是否与 sidecar 一致。

## 目录：打开任意备份

`backup.Scan(location)` 列出一个位置（向下两层目录）中的内容：

| 发现 | 备份项类型 | 可恢复为 |
|---|---|---|
| `.wslpilot/folder.json` | `folder` | 发行版、Volume，或恢复文件到发行版 |
| `config.json` + `snapshots/`（wpbk） | `repo`，每个快照一项 | 同上 |
| 带 sidecar 的 `*.tar*` | `archive` | 同上 |
| 不带 sidecar 的 `*.tar*` | `tar`（外部归档） | 同上；界面会说明来源与一致性未知 |
| `*.vhdx`、`*.vhd` | `vhdx` | 仅新发行版（`wsl --import --vhd`） |

默认备份目录以外的位置记录在应用数据中（`locations.json`），且为**只读**：不加锁、不清理、不删除，仓库以
`ReadOnly` 打开。用户可以将其接管为本机位置。`GroupBySource` 汇总所有位置中同一来源的各个版本。

## 恢复

每次恢复都会先对备份项运行 `Verify`，失败即停止。

| 方式 | 过程 |
|---|---|
| 新发行版 | `wsl --import <name> <folder> -`，tar 从 stdin 输入（VHDX 项使用 `--import --vhd`） |
| 覆盖发行版 | 先把当前发行版安全备份到 `<备份目录>\Safety`，然后注销，再以相同名称与目录导入 |
| 恢复文件到发行版 | 以 root 执行 `tar -xpf - --numeric-owner -C <dir>`，可只恢复某个路径 |
| 新 Volume | 创建 Volume，由辅助容器解包 tar |
| 覆盖 Volume | 安全备份、清空 Volume、解包 tar |
| 项目文件 | 把 Compose 与 env 文件写入 Windows 目录（它们本就是 Windows 文件） |

覆盖已有对象时需要用户输入其名称，并写入审计日志。

## 计划与调度

`internal/scheduler` 保存计划；计划只负责产生备份，备份属于目标位置。

| 字段 | 含义 |
|---|---|
| `targets` | 要备份的来源 |
| `destination` | `kind`（`folder`、`archive`、`repo`）、`path`、`compression` |
| `schedule` | `every`（`hourly`、`daily`、`weekly`、`monthly`、`manual`）、`at`（HH:MM）、`weekday`、`day`（1–28） |
| `retention` | 按来源分别计算的 `keepLast`、`keepDaily`、`keepWeekly`、`keepMonthly` |
| `retry` | 失败后的最多重试次数 `max`；延迟从 `initial` 开始，每次加倍，直到 `limit` |
| `live` | 不停止运行中的来源直接备份 |
| `notify`、`notifySuccess` | 通知 |

保留策略只删除该计划自己产生的备份（`plan` 字段或 `plan:<id>` 标签）；在仓库中则 forget 快照并清理。每次运行会向计划历史追加一条记录。

窗口打开时计划每分钟检查一次；安装 agent 后，还会由 `wslpilot-agent run-due` 运行，它由每用户的计划任务
（`WSLPilot\RunDue`，每 15 分钟，“仅交互”，不提权）启动。锁文件 `locks/scheduler.lock`（进程 ID 与时间；进程不存在或超过 24 小时即视为过期）
防止两者同时运行计划。

## 重启策略与自启

WSLc 没有 `--restart`。容器通过 label `io.wslpilot.restart` 携带策略（`no`、`always`、`unless-stopped`、`on-failure[:N]`）。
`wslpilot-agent watch` 在登录时由 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 启动，监听 `wslc events`，
在策略要求时于 `die` 之后重新启动容器，两次重启之间依次等待 1 秒、2 秒、4 秒……最多 1 分钟。死亡之前出现的 `stop` 或 `kill`
视为用户停止；之后的 `start` 会重新启用策略。同一进程还会启动标记为自启的 Compose 项目，并每 5 分钟运行到期计划。没有任何
WSLPilot 进程运行时策略不生效；界面会说明这一点。
