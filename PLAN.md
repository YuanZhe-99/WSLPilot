# PLAN：WSLPilot — WSL & WSLc 管理器

> 文档日期：2026-11-05
> 项目名称：**WSLPilot**（仓库目录名；正式名称如需变更请在 M0 结束前确定）
> 目标：基于 Go + MyGo 的 Windows 11（x64 / ARM64）原生桌面工具，统一管理 WSL 发行版、WSLc 容器 / 镜像 / 网络 / Volume、自研 Compose、备份与系统集成。
> 本文只放**计划与未完成工作**。已实现的行为、数据格式和接口契约一旦落地就移到 `doc/`，并从本文删除。
> 需求原文：用户在初始化会话中提供的《WSL & WSLc Manager — 项目需求》（第 1–10 节，下文以「需求 §N」引用）。

## 0. 核心决策

优先级固定为：**用户数据完整性 > 不破坏现有 WSL / WSLc 状态 > 与原生 CLI 兼容 > 可恢复的长期格式 > 功能覆盖 > 界面体验**。

| 主题 | 决策 | 理由 |
|---|---|---|
| UI 技术栈 | **MyGo 原生 UI（package `ui`，纯 Go）**，不使用 React + TypeScript | 需求 §1 允许「根据 MyGo 实际能力选择」。MyGo v0.7.4 的原生 UI 自带 Terminal 插件（libghostty-vt + PTY）、代码编辑器（诊断/折叠/查找）、表格、图表、Sidebar、托盘、`ui.Tester` 无窗口测试；无需 WebView2/Bun，单一 Go 代码库；与 Easytier-MyGo、Kyokai 的做法一致。若后续某页确需 Web 能力，MyGo 允许同一应用新增 Web 窗口，无需推翻架构 |
| 进程模型 | `wslpilot`（GUI，普通用户权限）+ `wslpilot-agent`（同一用户的后台调度器）+ `wslpilot-cli`（脚本 / 调试入口） | 需求 §6、§7 要求应用未打开时执行备份和自动化；三者共享 `internal/` 核心 |
| 后台运行方式 | 当前用户的「计划任务」(Task Scheduler，登录用户、非最高权限) 触发 `wslpilot-agent`，不安装 LocalSystem 服务 | WSL / WSLc 都是每用户的；用户级任务不需要管理员权限 |
| 与上游交互 | **CLI 优先**：`wsl.exe`、`wslc.exe`（优先 `--format json`）。`Microsoft.WSL.Containers` SDK 作为后续可替换 Adapter 评估 | 需求 §9 要求稳定 CLI / 官方 API；CLI 可在本机直接验证，SDK 需另行调研其 ABI 与无 cgo 调用方式 |
| 状态归属 | WSL / WSLc 的状态**永远以上游为准**，每次读取；WSLPilot 只保存自身元数据（Compose 项目、备份仓库、计划、偏好） | 需求 §1「不强制用户通过本软件管理」，用户用 CLI 改动后界面必须如实反映 |
| Compose | 可替换 Backend：`builtin`（自研，当前默认）与 `native`（上游 `wsl compose` / `wslc compose`，检测到后才可用）；**Backend 按项目固定**，切换必须用户确认 | 需求 §4 |
| Compose 解析 | 使用 `github.com/compose-spec/compose-go/v2`（Apache-2.0）加载、插值、合并、校验 | 官方参考实现；避免自写 YAML 语义 |
| 增量备份 | 自研版本化仓库格式 `wpbk`：内容定义分块 + BLAKE3 + zstd + pack 文件 + 快照清单 | 需求 §6.C 明确要求自定义格式；分块、哈希、压缩使用成熟库 |
| Linux 元数据 | 所有备份 / 导入导出以 **tar 流**为传输单位，在 Linux 侧打包解包（`tar --numeric-owner -p`），Windows 侧只搬运字节流 | NTFS 无法表达 uid/gid/mode/symlink/xattr；避免经 Windows 目录中转 |
| 配置文件编辑 | `.wslconfig`、`wsl.conf`、`settings.yaml`、Compose YAML 均作为**原始文档**编辑：只改动变更片段、保存前冲突检测、自动备份、校验后才应用 | 与 Easytier-MyGo 的 configdoc 原则相同；保留用户注释与格式 |
| Windows Terminal | 优先使用 **JSON Fragment 扩展**（`%LOCALAPPDATA%\Microsoft\Windows Terminal\Fragments\WSLPilot\`）新增 Profile；修改 / 隐藏既有 Profile 才写 `settings.json`，写前备份 | Fragment 是 WT 官方扩展机制，不侵入用户设置 |
| 权限 | GUI 永不以管理员身份运行；需要提权的操作（启用可选组件、`wsl --install` 首装、部分 `--update`）单独 `runas` 子进程并在 UI 明示 | 需求 §7 |
| 版本跟随 | Go、MyGo 跟随最新稳定版；WSL / WSLc 记录「已测试版本」，未测试版本标注并按功能探测降级 | 需求 §9 |
| 许可证 | GPL-3.0（与 Easytier-MyGo 一致，**待用户确认**） | 第三方库均需兼容 GPL-3.0 |

## 1. 已核对的事实（2026-11-05，本机 Windows 11 ARM64 26300）

只记录本次真实运行或阅读到的内容。「待验证」项必须形成实验结果后才可在实现中依赖。

| 事实 | 对方案的影响 | 来源 |
|---|---|---|
| MyGo 最新 tag v0.7.4，需要 Go ≥ 1.27.3，无 cgo；原生 UI 用 Direct3D 11，Windows 10/11 x64/arm64 | 直接锁定 v0.7.4；`go.mod` 声明 `go 1.27.3` | `github.com/egoist/mygo` README、`go.mod` |
| MyGo 提供 Tray、`App.SetOpenAtLogin`、Notification、Dialog、签名自动更新（GitHub Releases）、单实例锁、i18n（ARB 目录 + 生成 `catalog_gen.go`） | 需求 §7 的托盘、自启、更新、通知直接使用 MyGo | `docs/app.md`、`docs/menus.md`、`docs/updates.md`、`docs/i18n.md` |
| MyGo Terminal 插件：原生 UI 内运行任意程序于 PTY，Ghostty 终端模拟 | 需求 §2/§3/§8 的交互终端：`wsl.exe -d <distro>`、`wslc exec -it <ctr> sh` | `docs/plugins/terminal.md` |
| 本机 `wsl --version`：WSL 3.0.3.0，内核 6.18.54.1-1 | 首个已测试 WSL 版本 | 本机运行 |
| `wsl.exe` 输出到管道时默认 UTF-16LE；设置 `WSL_UTF8=1` 后为 UTF-8；标签与状态文字**随系统语言本地化** | 适配层必须设置 `WSL_UTF8=1` 并兼容 UTF-16；不能按英文标签解析 `--version`、`-l -v` | 本机运行 |
| `wsl --help` 含 `--manage <Distro> --move/--resize/--set-sparse`、`--export --format`、`--import --vhd`、`--import-in-place`、`--update`、`--unregister` | 发行版迁移用 `--manage --move`，不自行复制 VHDX | 本机 `wsl --help` |
| `wslc` 3.0.3.0 子命令：container / image / network / volume / registry / settings / system(session, events, info) / build / exec / attach / logs / stats / export / import / load / save / cp | 需求 §3 的大部分容器操作有 CLI 对应 | 本机 `wslc --help` |
| `wslc info --format json` 返回 Client.Version、KernelVersion、SettingsFile 与 Server.Sessions | 能力探测的首选入口（结构化、不随语言变化） | 本机运行 |
| `wslc stats` / `wslc info` 支持 `--format json`；`wslc inspect` 默认输出 JSON | 列表与资源占用优先用 JSON；无 JSON 的命令需录制 fixture 验证 | 本机 `--help` |
| `wslc run` 无 `--restart`；有 `--health-*`、`--mount`、`-v`、`-p`、`--network`、`--network-alias`、`--label`、`--cpus`、`-m` | **重启策略和自动启动需 WSLPilot 自行实现**（agent 监听 `wslc events` 并按策略重启） | 本机 `wslc run --help` |
| `wslc volume create -d guest|vhd -o ... -l ...`，默认驱动 `guest` | Volume 管理直接映射；两种驱动的语义差异待验证 | 本机运行 |
| `wslc container cp` 支持 `-` 从 stdin 读取，`-a` 仅为 Docker 兼容「接受」 | TAR 导入**不依赖** `cp` 保留属主；采用 helper 容器内 `tar -xp --numeric-owner`（见 §5） | 本机 `--help` |
| WSLc 设置文件 `%LOCALAPPDATA%\wslc\settings.yaml`（本机尚不存在） | 可视化编辑需处理「文件不存在 = 全部默认值」；键名需从上游源码 / 文档核对 | `wslc info` |
| WSL Containers 已 GA；原生 Compose 尚未提供，微软表示计划以 `wsl compose up` 形式支持现有 `compose.yaml`；有 NuGet 包 `Microsoft.WSL.Containers` | Native Compose Backend 的探测入口暂定为 `wslc compose` / `wsl compose` 子命令存在与否；SDK 待评估 | 新闻报道（Help Net Security 2026-09-30、next.ink），**未找到官方 API 文档，待核对** |

待验证（M1 前必须得出结论）：

1. `wsl --list --verbose` 在多语言系统下的列格式；默认发行版是否可改从注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Lxss` 稳定读取（属内部实现，仅作只读回退）。
2. `wslc list` / `images` / `volume list` / `network list` 是否有 JSON 输出（`--format` 未在所有命令出现）。
3. `wslc events` 的输出格式与是否可长连接。
4. Volume `guest` 与 `vhd` 驱动的数据位置、大小统计与迁移方式；WSLc 数据根目录能否迁移（需求 §3「数据存储位置迁移」）。
5. `wslc` 多 session（`--session`）对容器可见性的影响；WSLPilot 应使用默认 CLI session 以便与用户 CLI 看到同一组对象。
6. `Microsoft.WSL.Containers` SDK：是否为平面 C ABI、能否用 `golang.org/x/sys/windows` / purego 无 cgo 调用、与 CLI 版本的对应关系。
7. 运行中发行版 `wsl --export` 的一致性；`--format vhd` 是否要求停机。

## 2. 需求可行性评估

| 需求 | 方案 | 限制 / 替代 |
|---|---|---|
| §2 检测、安装、导入、启停、终止、删除发行版 | `wsl --list --online`、`--install --no-launch`、`--import(--vhd)`、`-d X -- true`（启动）、`--terminate`、`--shutdown`、`--unregister` | 首装 WSL 需管理员；`--unregister` 必须先提供导出备份选项 |
| §2 默认发行版 / WSL 版本 | `--set-default`、`--set-version`、`--set-default-version` | WSL1↔2 转换耗时长，作为可取消长任务（取消 = 终止进程，需验证上游回滚行为） |
| §2 迁移到指定磁盘 | `wsl --manage X --move <dir>` | 需求中的「迁移」不自行复制 VHDX |
| §2+ VHDX 压缩（用户追加，官方未提供） | 见 §2.1 | 需要管理员权限与 `wsl --shutdown`；作为显式长任务 |
| §2 导出 / 导入 / 备份恢复 | `--export --format tar|tar.gz|vhd`、`--import` | 一致性：默认先 `--terminate`（用户确认），见 §6 |
| §2 状态、磁盘占用、资源 | 状态来自 CLI；VHDX 大小来自 BasePath 文件；内存 / CPU 通过 `wsl -d X -- cat /proc/...` 或 vmmem 进程 | 「可获取的」信息为准，界面注明来源 |
| §2 `.wslconfig` / `wsl.conf` | **可视化表单为主**：常用键（systemd、默认用户、Windows 互操作与 PATH、自动挂载与 metadata、主机名、DNS、时区；`.wslconfig` 的内存、处理器、交换区、网络模式、空闲超时、sparse 等）以通俗标签、默认值与说明呈现，保存前展示差异；**直接编辑原文件归入「高级」**。底层为原始 INI 文档 + 键模式库，只改变更行，保留注释与表单未覆盖的键（并在 UI 提示其存在）。`wsl.conf` 以 `wsl -d X -u root -- sh -c` 读写，写前备份到 `/etc/wsl.conf.wslpilot-<ts>`；WSLc `settings.yaml` 同一模式 | 修改后提示需要 `--shutdown` / `--terminate` 生效；键清单与默认值须对照微软官方文档核对后才写入键模式库 |
| §2 终端 / Windows Terminal | MyGo Terminal 插件；WT Fragment | WT 未安装时隐藏入口 |
| §2 更新 | `wsl --update`（含 `--pre-release` 选项）；发行版内更新以终端运行包管理器，不自动执行 | 发行版更新不做静默批处理 |
| §3 容器 / 镜像 / 网络 / 端口 / Volume / 日志 / exec / stats / inspect | 一一映射 `wslc` 子命令 | 「编辑容器配置」= 依据 inspect 生成新配置并重建容器（容器配置本身不可变），需确认 |
| §3 重启策略、自动启动 | `wslpilot-agent` 监听事件并按策略重启；策略存于 WSLPilot 元数据并写入容器 label `io.wslpilot.restart` | 应用自身实现，不是上游功能；agent 未运行时策略不生效，需在 UI 明示 |
| §3 WSLc 数据位置迁移 | 依赖待验证项 4 | 若上游无官方方式，标记「暂不支持」并记录替代方案（导出 Volume/镜像 → 新位置导入） |
| §4 Compose | 见 §4 | — |
| §5 TAR → Native Volume | 见 §5 | — |
| §6 备份 | 见 §6 | — |
| §7 托盘、自启、通知、自动更新 | MyGo 原生能力 | 自动更新需发布签名密钥（GitHub secrets） |
| §8 各页面 | 见 §7 | — |

### 2.1 VHDX 压缩（用户追加需求）

WSL2 的 `ext4.vhdx` 只增不减：Linux 内删除文件后，Windows 侧文件大小不回落。官方只提供 `wsl --manage <Distro> --set-sparse true`（稀疏 VHD，自动回收，本机 `--help` 已确认存在），没有一次性手动压缩命令。WSLPilot 提供「压缩磁盘」：

1. 预估：显示 VHDX 文件当前大小，以及 Linux 内 `df` 的已用空间，二者之差即为可回收空间的上限，供用户判断是否值得做。
2. 准备（发行版运行中）：`wsl -d X -u root -- fstrim -av`，让文件系统把空闲块告知虚拟磁盘。
3. 停机：VHDX 挂在 WSL 的实用虚拟机上，必须 `wsl --shutdown`。这会同时停止**所有**发行版和 WSLc 容器，所以要列出将受影响的对象，由用户确认。
4. 压缩：放在单独提权的子进程里执行，GUI 本身不提权：
   - 首选 `diskpart`（Windows 家庭版也有）：`select vdisk file="…"` → `attach vdisk readonly` → `compact vdisk` → `detach vdisk`。脚本写入临时文件，路径要正确转义。
   - 可选 `Optimize-VHD -Mode Full`：需要 Hyper-V PowerShell 模块，仅在检测到时提供。
5. 校验：比较压缩前后的文件大小；重新启动发行版并检查 `wsl -d X -- true`。失败时确保 VHDX 已 detach，并在日志里记录 diskpart 的输出。
6. 安全：压缩前检查目标盘的剩余空间；如果最近没有备份，默认勾选「先导出备份」；禁止对正在被其它进程打开的 VHDX 执行。

同一流程也用于 WSLc 的 `vhd` 驱动 Volume 和 WSLc 自身的数据磁盘（这两者的位置取决于 §1 待验证项 4）。

待验证：已设为 sparse 的 VHDX 能否再用 diskpart 压缩，以及效果如何；`attach vdisk readonly` 对 ext4 VHDX 做 compact 实际能回收多少（对照 `fstrim` 前后的结果）；ARM64 上 diskpart 与 Optimize-VHD 的行为。

## 3. 架构

```
cmd/
  wslpilot/           GUI（MyGo 原生 UI）
  wslpilot-agent/     后台调度：计划备份、重启策略、Compose 自启（M5）
  wslpilot-cli/       命令行入口，复用同一核心（M2）
internal/
  version/            版本号（与 mygo.json 一致）
  procexec/           子进程执行：编码、超时、取消、流式输出、参数安全（M1）
  wsl/                wsl.exe 适配：探测、发行版、导入导出、迁移（M0 起）
  wslc/               wslc.exe 适配：探测、容器、镜像、网络、Volume、事件（M0 起）
  capability/         汇总 WSL / WSLc / Native Compose 能力与版本兼容矩阵（M1）
  configdoc/          原始文档编辑：INI(.wslconfig / wsl.conf)、YAML(settings.yaml)（M2）
  wtprofile/          Windows Terminal Fragment 与 settings.json（M2）
  compose/            模型、规划器、项目存储；backend/builtin、backend/native（M3）
  volume/             Volume 管理、TAR 导入导出、helper 容器（M4）
  backup/             备份编排；folder、archive、repo(wpbk) 三种模式（M5–M6）
  scheduler/          计划、保留策略、重试；Task Scheduler 注册（M5）
  jobs/               长任务：进度、取消、日志、通知（M1）
  store/              WSLPilot 自身元数据（JSON 文件 + 版本号 + 原子写）（M1）
  gui/                视图（只调用上述包，不直接拼 CLI 参数）
locales/              ARB 目录与生成的 catalog_gen.go
tools/checkdocs/      文档镜像校验
```

分层规则：

- `gui` → 业务包（compose、volume、backup…）→ 适配层（wsl、wslc）→ `procexec`。下层不得引用上层。
- 适配层只做「命令 ↔ 类型」转换，每个解析器配 `testdata/` fixture（按 WSL / WSLc 版本分目录）。
- 适配层定义接口（如 `wslc.Engine`），CLI 实现为默认；SDK 实现作为可选替换，同一套 contract 测试。
- 所有破坏性操作经过 `jobs` 统一的「确认 → 预备份 → 执行 → 校验」流程。
- 测试与实机验证使用的资源一律以 `wslpilot-test-` 为前缀，绝不触碰用户已有对象。

## 4. Compose

### 4.1 Backend 接口（M3 落地后移入 `doc/en-us/compose.md`）

```go
type Backend interface {
	ID() BackendID                 // "builtin" | "native"
	Detect(ctx) (Availability, error)
	Validate(ctx, *Project) (Report, error)  // 含不支持特性清单
	Up(ctx, *Project, UpOptions, Progress) error
	Down(ctx, *Project, DownOptions, Progress) error
	Restart / Stop / Start / Ps / Logs(ctx, ...)
}
```

- 项目元数据 `projects/<id>/project.json`：`schemaVersion`、`backend`、Compose 文件路径（原文件留在用户目录，不复制改写）、`.env`、自启开关、Volume 选择（native / bind）。
- 容器、网络、Volume 命名与 label 采用 Compose 约定：`<project>-<service>-<n>`，label `com.docker.compose.project`、`com.docker.compose.service`、`com.docker.compose.container-number`、`com.docker.compose.config-hash`，外加 `io.wslpilot.backend=builtin`。好处：未来上游原生 Compose 若沿用同一约定即可接管；状态从 `wslc` label 反查，不依赖本地缓存。

### 4.2 Builtin Backend（首版范围）

- 支持：`image`、`command`/`entrypoint`、`environment`/`env_file`、`ports`、`volumes`（named / bind / tmpfs）、`networks`（含 aliases）、`depends_on`（`service_started` / `service_healthy` / `service_completed_successfully`）、`healthcheck`、`restart`（由 agent 实现）、`deploy.resources.limits`（cpus / memory）、`labels`、`hostname`、`user`、`working_dir`、`stop_signal`、`stop_grace_period`、`shm_size`、`ulimits`、`dns*`、`profiles`、`extends`、`include`、多文件合并、变量插值。
- 规划：拓扑排序 → 对每个服务计算 config-hash → 与现有容器 diff → 只重建变更服务（`up` 幂等）。
- **不支持特性必须显式报告**：`build`（首版可选调用 `wslc build`，待评估）、`secrets`/`configs`（可转换为只读 bind，需确认）、`network_mode: service:x`、`ipc`、`pid`、`devices`、`privileged`、`cap_add` 等——Validate 返回错误级别，`up` 拒绝执行而非静默忽略。

### 4.3 Native Backend 与迁移

- 探测：`wslc compose --help` / `wsl compose --help` 的退出码与版本；探测结果只用于**提示**。
- 已有项目绝不自动切换。迁移向导：Validate(native) → 对比两边的资源命名 / label → `builtin down`（保留 Volume）→ `native up` → 失败时回滚到 builtin。
- 新项目在 native 可用后才可选择 native；默认仍为 builtin，直到 native 在已测试版本矩阵中通过 contract 测试。
- 上游原生实现覆盖的功能，builtin 不再扩展（只修 bug）。

## 5. Volume 与 TAR 导入导出

- 导入（tar → Native Volume），不经 Windows 目录解压：
  1. `wslc volume create`（目标已存在时：确认 + 先导出备份）。
  2. 启动 helper 容器：挂载目标 Volume 于 `/dst`，镜像为 WSLPilot 自带的最小 rootfs（通过 `wslc import` 从内嵌 tar 导入，固定摘要，避免联网与第三方镜像漂移；需带 GNU/busybox tar，许可证待确认）。
  3. Windows 侧把 tar 文件流式写入 `wslc exec -i helper tar -xpf - --numeric-owner -C /dst`（支持 .tar / .tar.gz / .tar.zst）。
  4. 校验：条目数、总字节、可选逐文件哈希清单；删除 helper。
- 导出：`tar -cpf - --numeric-owner --xattrs -C /src .` 经 stdout 流到目标文件。
- 待验证：stdin/stdout 二进制流是否原样通过 `wslc exec -i`（CRLF、缓冲、大文件）；xattr / ACL 支持；若不可靠，改用 `container cp - helper:/dst`（并测量属主是否保留）。
- 备份 / 恢复 / 迁移复用同一 tar 流管道。

## 6. 备份与恢复

### 6.1 统一模型

`Source`（发行版 / Volume / Compose 项目）→ 产出一个或多个 **tar 流** → `Sink`（folder / archive / repo）。恢复是逆过程。一致性策略按 Source 决定：

| Source | 默认一致性策略 |
|---|---|
| WSL 发行版 | 用户确认后 `wsl --terminate` → `wsl --export`；运行中导出仅作为「可能不一致」的显式选项 |
| WSLc Volume | 停止挂载该 Volume 的容器（记录并在结束后恢复原状态）→ helper tar；或用户选择「不停止」并标注 |
| Compose 项目 | 项目配置文件 + `.env` + 项目元数据 + 其 named volumes（按 Volume 策略） |

### 6.2 三种模式

- **Folder**：目录结构可浏览；Linux 元数据另存 `.wslpilot/manifest.jsonl`（路径、uid、gid、mode、mtime、symlink 目标、xattr、硬链接组）；恢复时由 manifest + 文件重建 tar 流。特殊文件（设备、FIFO）只存于 manifest。
- **Archive**：`.tar` / `.tar.zst` / `.tar.gz` + 旁置 `*.wpmeta.json`（格式版本、来源、时间、SHA-256、工具版本）；恢复前可校验。
- **Incremental（`wpbk` 仓库）**：
  - 布局：`config.json`（`format: "wpbk", version: 1`）、`packs/`、`index/`、`snapshots/`、`locks/`。
  - 分块：FastCDC（内容定义，平均 1 MiB），块 ID = BLAKE3-256；zstd 压缩；块打包为 ~32 MiB pack，pack 尾部自带索引，可脱离 `index/` 重建。
  - 快照：按 tar 条目记录元数据 + 内容块列表（而非整个 tar 的字节分块），以便逐文件浏览与恢复。
  - 操作：backup、restore（任意快照）、check（结构 / 全量读取）、forget（保留策略）、prune（重打包回收空间）、upgrade（格式版本迁移，旧版本只读支持至少一个大版本）。
  - 加密：v1 预留 `encryption` 字段，首版不加密（**待用户决定是否首版需要**）。
  - 库选型（待核对许可证与 ARM64 性能）：`github.com/klauspost/compress/zstd`、`lukechampine.com/blake3` 或 `github.com/zeebo/blake3`、FastCDC 实现。
  - 格式规范写入 `doc/en-us/backup-format.md`，附带「只用规范即可手工恢复」的验收测试。

### 6.3 自动备份

- 计划（每小时 / 每天 / 每周 / cron 表达式）、保留策略（最近 N 个、按日/周/月分层）、失败重试（指数退避、上限）、通知、日志与历史、空间统计。
- 应用未运行：用户级计划任务按最近一次到期时间唤醒 `wslpilot-agent run-due`；GUI 运行时由 GUI 进程内调度，二者通过仓库 `locks/` 与 agent 锁文件互斥。

### 6.4 以恢复为中心：打开任意来源的备份（用户追加需求）

备份页以「能恢复什么」为主视图，而不是以计划为主；计划只负责产生备份，不拥有备份。

- **统一目录**：按备份对象（发行版、Volume、Compose 项目）汇总所有位置、所有类型（Folder / Archive / 增量仓库）的版本，并注明每个版本由哪个计划或哪次手动操作产生。
- **打开备份**：可以选择或拖入任意文件夹、归档（`.tar`、`.tar.gz`、`.tar.zst`，有无 `.wpmeta.json` 均可）、`wsl --export` 生成的 `.tar` / `.vhdx`、`wpbk` 增量仓库，包括来自其它电脑或 NAS 的备份。打开后先做完整性校验，再列出其中的内容。
- **只读**：外部打开的位置默认只读，不加锁、不执行 forget / prune，避免破坏别人或别的电脑的仓库。用户可以显式把它「接管」为本机位置。
- **非 WSLPilot 产生的 tar**：识别为「普通归档」，可以恢复为新发行版（`wsl --import`）或新 Volume（§5 的 helper 流程）。缺少元数据时，界面需要说明哪些信息无法确认。
- **恢复目标**：默认恢复为新的发行版 / Volume / 项目；覆盖现有对象需要确认，并且先备份现有对象；支持只恢复部分文件到文件夹或运行中的发行版。
- **格式兼容**：恢复路径必须能读取所有已发布的旧格式版本（wpbk v1 起），这一点写入 `backup-format.md` 的验收测试。

## 7. 界面

Sidebar：Dashboard / WSL / WSLc / Compose / Volumes / Backups / Terminal / Settings（M0 已建空壳）。

- 视图是状态的函数；后台结果经 `Window.Update` 发布；所有 I/O 不在 UI 线程。
- 全局任务面板：进度、取消、错误详情、日志；完成通知。
- 危险操作（删除、覆盖、迁移、unregister、prune）统一确认对话框：说明影响、提供「先备份」默认勾选、需要输入名称确认的场景（unregister、删除带数据的 Volume）。
- 配置类页面统一模式：常用设置表单化，原文件编辑为「高级」；待保存变更以差异预览呈现；注明是否需要重启。
- 界面原型（设计稿）：https://claude.ai/artifact/H1EtLxkwG9h3yURRJNxprT（Dashboard、WSL、容器、Compose、Volume、备份、发行版设置）。
- 深色 / 浅色 / 跟随系统（`mygo.Theme.SetSource`）；布局数值、颜色为命名 token。
- 界面字符串走 ARB（en、zh-Hans 首批；zh-Hant、ja 视需要）。

## 8. 里程碑

每个里程碑结束：本地全套检查通过、文档更新、`version-history.md` 记录。

| 里程碑 | 内容 | 验收 |
|---|---|---|
| **M0 初始化**（本次） | 仓库结构、AGENTS / PLAN / 文档、MyGo 原生 UI 壳、版本号、WSL/WSLc 探测、CI | `go test ./...`、`go tool mygo vet .`、`i18n check`、`checkdocs` 通过；应用可启动并显示探测结果 |
| M1 基础设施 | `procexec`（UTF-16/UTF-8、取消、流式）、`capability`、`jobs`、`store`、fixture 体系、gated live tests；解决 §1 待验证 1–5、7 | 适配层 contract 测试；实机记录写入 `doc/en-us/upstream.md` |
| M2 WSL 管理 | 发行版全生命周期、迁移、导入导出、VHDX 压缩与 sparse 开关、`.wslconfig` / `wsl.conf` 编辑、终端、WT Profile、更新；`wslpilot-cli` | 在 `wslpilot-test-*` 发行版上完成全部操作的实机验证 |
| M3 WSLc 与 Compose builtin | 容器 / 镜像 / 网络 / 日志 / stats / exec 终端 / settings.yaml；Compose 加载、校验、up/down/restart/redeploy、日志聚合、不支持特性报告 | Compose 规划器单元测试 + 多服务示例项目实机 up/down |
| M4 Volume | Volume 管理、helper rootfs、TAR 导入导出、迁移、Compose 中 native/bind 选择 | 权限 / 属主 / symlink / 硬链接 / 大文件往返测试 |
| M5 备份 Folder + Archive + 调度 | 统一模型、两种模式、计划任务、保留策略、通知、`wslpilot-agent`、重启策略与 Compose 自启 | 备份 → 删除 → 恢复往返，比对 manifest |
| M6 增量仓库 wpbk | 格式规范、backup/restore/check/forget/prune/upgrade | 故障注入（截断 pack、损坏索引）后 check 能定位、可恢复的数据能恢复 |
| M7 系统集成与发布 | 托盘、自启、应用自动更新、安装包、x64 + ARM64 发布、签名 | 两种架构真机安装 / 更新验证 |
| M8 Native Compose | 上游发布后：native backend、迁移向导、兼容矩阵 | 依上游时间表 |

## 9. 测试策略

- 解析器：表驱动 + 按上游版本归档的 fixture（含中文系统输出）。
- 业务逻辑（Compose 规划、保留策略、wpbk）：纯单元测试，不需要 WSL。
- 实机测试：`WSLPILOT_LIVE=1` 才运行；只操作 `wslpilot-test-` 前缀资源；结束时清理。
- UI：`ui.Tester`；涉及布局的变更需截图检查原生窗口。
- 每个修复的 bug 附回归测试。

## 10. 待用户决定

1. 正式产品名（目前为 WSLPilot）。图标已定为方案 B1（Windows 窗口 + 终端 + 走出窗口的扁平企鹅），源文件为 `resources/icon.svg`；16px 托盘小图标可能需要单独简化，留待 M7 处理。
2. 许可证（暂定 GPL-3.0）。
3. Git 远程：是否与其它项目一样使用私有 Gitea `origin` + GitHub `github` 双远程。
4. wpbk 首版是否需要加密。
5. helper rootfs 的来源（自建最小 rootfs vs. 固定摘要的上游镜像）。
