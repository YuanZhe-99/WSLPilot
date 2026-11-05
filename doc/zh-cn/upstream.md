# 上游契约

这里只记录真实运行中观察到或从官方源码中读到的行为，并注明观察时的版本。未经验证的假设放在 `PLAN.md`。

## 已测试版本

机器可读的列表是 `internal/upstream/tested.json`；它与本表保持一致。

| 组件 | 版本 | 机器 |
|---|---|---|
| WSL | 3.0.3.0（内核 6.18.54.1-1） | Windows 11 ARM64，build 26300 |
| WSLc | 3.0.3.0（session manager 3.0.3） | Windows 11 ARM64，build 26300 |
| MyGo | v0.7.4 | Windows 11 ARM64，build 26300 |

## wsl.exe

| 行为 | 处理方式 |
|---|---|
| 输出到管道时为无 BOM 的 UTF-16LE；设置 `WSL_UTF8=1` 后为 UTF-8。行尾为 CRLF。 | `procexec.Run` 设置 `WSL_UTF8=1`，`procexec.Decode` 两者都接受。 |
| `--version` 的标签以及 `--list --verbose` 的表头与状态文字均本地化。 | `wsl.ParseVersion` 按行位置匹配，不匹配任何标签。 |
| `--version` 各行顺序：WSL、内核、WSLg、MSRDC、Direct3D、DXCore、Windows。 | 按位置解析；Windows 取最后一行。 |
| `--list --quiet` 每行输出一个名称。 | `wsl.ParseNames`。 |
| 未知的第一个参数（`wsl.exe compose`）会作为 Linux 命令在默认发行版中运行（找不到时退出码 127）。 | 从不用 `wsl.exe <单词>` 探测功能。 |
| `--help` 列出 `--manage <Distro> --move/--resize/--set-sparse`、`--export --format`、`--import --vhd`、`--import-in-place`、`--update`、`--unregister`。 | `wsl.Client` 的方法。 |
| `--list --verbose`：行首 `*` 表示默认发行版；名称不含空格；最后一列是版本数字。状态列已本地化。 | `wsl.ParseListVerbose` 只读取 `*`、名称与版本；运行状态取自 `--list --running --quiet`。 |
| `--list --online`：本地化的说明与表头，最后一个空行之后每行为 `<名称> <显示名称>`。 | `wsl.ParseListOnline`。 |
| 不输出路径。`HKCU\Software\Microsoft\Windows\CurrentVersion\Lxss\{GUID}` 保存 `DistributionName`、`BasePath`、`VhdFileName`、`Flavor`、`OsVersion`、`DefaultUid`；WSL 还会为每个发行版写入 Windows Terminal fragment（`TerminalProfilePath`）。 | 只读，仅用于路径与详细信息（`wsl.Client.List`）。 |
| `--manage <Distro> --compact` 已存在（3.0.3.0），无需提权，可对运行中的发行版执行并会终止它。一个删除了 200 MB 的 281 MB 磁盘缩小到 72 MB。 | `wsl.Client.Compact`；`SupportsCompact` 在 `--help` 中查找字面量 `--compact`。 |
| `--manage --set-sparse true` 成功，并提示稀疏 VHD 仍属实验性。`--manage --move` 移动磁盘并更新 `BasePath`。 | `SetSparse`、`Move`。 |
| `--export <Distro> -` 把 tar 流输出到 stdout；读取方必须读完，否则 wsl.exe 会阻塞。 | `wsl.Client.Export` 传入 writer。 |
| 发行版运行时以及 `--terminate` 之后，`--export --format vhd` 都返回 `ERROR_SHARING_VIOLATION`（实用虚拟机仍挂载着磁盘）；`wsl --shutdown` 之后不到一秒即可复制磁盘，且该副本可用 `--import --vhd` 导入。 | VHD 导出会在警告后先关闭 WSL。 |
| `wsl --shutdown` 不会停止 WSL 容器的 session 虚拟机（`vmmemwslc-*` 继续运行）。 | 关闭确认中说明容器会继续运行。 |
| `--install` 选项：`--name`、`--location`、`--version`、`--fixed-vhd`、`--vhd-size`、`--web-download`、`--from-file`、`--no-launch`。 | `wsl.InstallArgs` 总是附加 `--no-launch`。 |
| `--distribution X --user U --exec argv…` 不经 Linux shell 运行 argv。 | `wsl.ExecArgs`。 |

Fixture：`internal/wsl/testdata/3.0.3.0/`（真实录制）、`internal/wsl/testdata/synthetic/`（手写的本地化输出，已标明）。

## wslc.exe

| 行为 | 处理方式 |
|---|---|
| `wslc info --format json` 返回 `Client`（Version、KernelVersion、WindowsVersion、SettingsFile 等）与 `Server`（SessionManagerVersion、Sessions）。 | `wslc.ParseInfo`；主要的检测入口。 |
| 未知命令退出码为 1。3.0.3.0 中不存在 `wslc compose`。 | `wslc.HasNativeCompose` 将 `wslc compose --help` 退出码 0 视为存在。 |
| 设置文件为 `%LOCALAPPDATA%\wslc\settings.yaml`，可能不存在。 | 路径取自 `wslc info` 的输出。 |
| `wslc run` 没有 `--restart` 选项。 | 重启策略由 WSLPilot 实现（agent）。 |
| `wslc volume create` 驱动：`guest`（默认）与 `vhd`。 | — |
| `wslc container cp` 可从 `-`（stdin）读取数据流；`-a` 仅为 Docker 兼容而接受。 | 不假定 `cp` 会保留属主。 |
| 带 `--format json` 的 `list` 命令输出 JSON Lines（每行一个对象，与 Docker `--format json` 相同）；`inspect` 输出缩进的 JSON 数组。 | `wslc.ParseLines`、`wslc.ParseArray`。 |
| 列表中的 `Labels` 字段是逗号拼接的字符串，其值可能含逗号（WSLc 自身的 `com.microsoft.wsl.container.metadata` label 是 JSON）。 | Label 只从 `inspect` 读取。 |
| `inspect` 不存在的对象时退出码为 1，并输出 `[]`。 | `wslc.ErrNotFound`。 |
| `container list` 与 `volume list` 支持 `--filter label=k=v` 与 `--filter name=…`。 | Compose 按 label 查找容器。 |
| 未指定地址的发布端口绑定到 `127.0.0.1`（`session.defaultBindingAddress`）。 | 在端口列中显示。 |
| `volume create -d vhd` 必须带 `-o SizeBytes=<n>`；之后 `inspect` 显示 `Status.HostPath`，即 `%LOCALAPPDATA%\wslc\sessions\<session>\volumes\` 下的 VHDX。 | `CreateVolume`；vhd Volume 可用 `vhdx` 压缩。 |
| `-v "C:\path with space:/b:ro"` 绑定挂载 Windows 目录（virtiofs，只读生效）。 | `MountSpec` 渲染为 `source:target[:ro]`。 |
| 二进制数据经 `wslc exec -i` 的 stdin 与 stdout 原样传输（3 MB tar，双向 SHA-256 一致）；属主、权限、符号链接与硬链接经 `tar -xp --numeric-owner` 保留（alpine 中的 busybox tar 1.36.1）。 | Volume 导入导出管道。 |
| `container export` 拒绝运行中的容器（`WSLC_E_CONTAINER_IS_RUNNING`），并留下空的 `-o` 文件。 | 导出前先停止；失败的输出文件会被删除。 |
| `system events --format json` 持续输出 Docker 形式的事件（`Type`、`Action`、`Actor.Attributes`、`timeNano`），连接保持打开。 | `wslc.Client.Events`；重启策略监听 `die`。 |
| `image save` 用 `--output` 写出，`image load` 用 `--input` 读取；`network connect` 使用 `--network-alias`。 | `Save`、`Load`、`Connect`。 |
| `wslc build` 存在（`--file`、`--tag`、`--build-arg`、`--target`、`--progress plain`）。 | 通过它支持 Compose 的 `build`。 |
| 带 `--health-cmd` 时 `inspect` 报告 `State.Health.Status`（`healthy`…）；`--memory 64M` 得到 `HostConfig.Memory` 64 MiB；容器的 `Image` 等于镜像 inspect 的 `Id`；label 同时出现在 `Config.Labels` 与顶层 `Labels` 中。 | Compose 据此等待健康状态，并按 ID 检测镜像变化。 |
| `system session terminate` 停止 CLI session 虚拟机；下一条命令会启动新的 session。 | `TerminateSession`（使设置生效、可压缩 vhd Volume）。 |
| `settings.yaml` 键（Microsoft Learn，2026-10-16）：`session.cpuCount`、`memorySize`、`maxStorageSize`、`storagePath`、`defaultBindingAddress`、`hostLoopback`、`idleTimeout` 以及 `credentialStore`；所有值都接受 `default`。修改 `storagePath` 不会移动已有数据。 | `configdoc.WSLcSettings`；数据位置迁移靠导出再导入。 |

Fixture：`internal/wslc/testdata/3.0.3.0/`（用户名已替换为 `user`）。

## 配置文件

`wsl.conf` 与 `.wslconfig` 的键与默认值取自 Microsoft Learn《Advanced settings configuration in WSL》（2026-10-16），
`settings.yaml` 的取自《WSL container settings》（同日）。`internal/configdoc/schema.go` 复制了表单展示的子集。

| 行为 | 处理方式 |
|---|---|
| 节名与键名不区分大小写；微软自己的示例写作 `localhostforwarding`。 | `configdoc.INI` 不区分大小写查找键，并保留原有拼写。 |
| `.wslconfig` 的路径值必须把反斜杠写两次。 | `Field.Encode` / `Decode` 配合 `EscapeBackslashes`。 |
| `wsl.conf` 的修改在发行版停止后生效（最后一个 shell 关闭约 8 秒后）；`.wslconfig` 在 `wsl --shutdown` 后生效；WSLc session 设置在 `wslc system session terminate` 后生效。 | `Field.Effect` 决定重启提示。 |
| `wslc settings` 创建的文件中所有设置都被注释掉并设为 `default`。 | `configdoc.YAML.Set` 会取消这类行的注释。 |

## MyGo

| 行为 | 处理方式 |
|---|---|
| 原生 UI 不需要 WebView2；在 Windows 10/11 x64 与 arm64 上用 Direct3D 11 绘制。 | WSLPilot 只使用原生 UI。 |
| `mygo generate` 根据 ARB 目录生成 `locales/catalog_gen.go`。 | 编辑目录后运行；该文件需提交。 |
