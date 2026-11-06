# 包索引

| 包 | 职责 | 关键声明 |
|---|---|---|
| `cmd/wslpilot` | GUI 入口：单实例、托盘（打开、检查更新、退出）、主题、自带标题栏的主窗口、关闭到托盘、运行期间的计划与更新循环；`host` 用 MyGo 实现 `gui.Host` | `main`、`host` |
| `cmd/wslpilot-agent` | 后台 agent：`run-due`、`watch`、`install`、`uninstall` | `main`、`watch` |
| `cmd/wslpilot-cli` | 命令行入口 | `main` |
| `internal/version` | 发布版本号，与 `mygo.json` 保持一致 | `Version` |
| `internal/earlyenv` | MyGo 初始化时读取的环境变量（整帧绘制：`MYGO_PARTIAL=0`）；由 `cmd/wslpilot` 最先导入 | `FullFrames` |
| `internal/upstream` | WSLPilot 测试过的上游版本（`tested.json`） | `Tested`、`Manifest`、`Compare`、`NewerThanTested` |
| `internal/procexec` | 以参数向量启动子进程；输出解码；原始数据流 | `Runner`、`Run`、`Streamer`、`Stream`、`Spec`、`RunnerFrom`、`Decode`、`ExitError` |
| `internal/wsl` | `wsl.exe` 适配层：列表、生命周期、导入导出、迁移、压缩、稀疏、exec、文件读写 | `Client`、`Distro`、`List`、`ParseListVerbose`、`ParseListOnline`、`InstallArgs`、`ExecArgs`、`Compact`、`ReadFile`、`WriteFile` |
| `internal/wslc` | `wslc.exe` 适配层：容器、镜像、Volume、网络、stats、事件、build | `Client`、`WithSession`、`IsCLISession`、`Container`、`ContainerSpec`、`CreateArgs`、`Volume`、`Network`、`Event`、`ParseLines`、`ParseArray`、`HasNativeCompose` |
| `internal/capability` | 对 WSL、WSLc 与原生 Compose 的一次合并探测 | `Prober`、`Report` |
| `internal/store` | WSLPilot 元数据：带版本的 JSON 文档，原子写入 | `Store`、`Default`、`Load`、`Save`、`WriteFileAtomic`、`ErrNewerSchema` |
| `internal/jobs` | 长任务：进度、取消、日志、破坏性操作的审计日志 | `Manager`、`Job`、`Snapshot`、`Audit`、`ProgressWriter`、`FormatBytes` |
| `internal/configdoc` | 以原始文档方式编辑 `.wslconfig`、`wsl.conf`、`settings.yaml`；表单键模式；带冲突检测的保存 | `INI`、`YAML`、`Field`、`WSLConf`、`WSLConfig`、`WSLcSettings`、`ApplyINI`、`ApplyYAML`、`Snapshot.Save`、`DiffLines` |
| `internal/vhdx` | 用 diskpart 压缩 WSL 自身无法压缩的 VHDX | `Script`、`Compact`、`Elevator`、`ErrInUse` |
| `internal/elevate` | 通过 UAC 提示启动单个提权子进程 | `Run`、`ErrDeclined` |
| `internal/secret` | 以 DPAPI 保护保存的机密（仓库密码） | `Protect`、`Unprotect` |
| `internal/backup/wpbk` | 增量备份仓库（见 `backup-format.md`） | `Init`、`Open`、`Repo.Backup`、`Restore`、`Entries`、`Check`、`Forget`、`ApplyPolicy`、`Prune`、`RebuildIndex`、`ChangePassword`、`ReadConfig` |
| `internal/gui` | 原生 UI（`gui.md`）：窗口外壳与侧边栏、概览、WSL、容器、Compose、Volume、备份、终端、任务；带搜索的设置面板；应用内更新；确认对话框、配置表单、样式常量与图标 | `App`、`New`、`Host`、`Term`、`TitleBarOptions`、`UpdateLoop`、`confForm` |
| `locales` | 由 `locales/*.json` 生成的消息访问函数 | `catalog_gen.go` |
| `tools/checkdocs` | 校验 `doc/` 语言镜像 | `main` |
| `tools/icon` | 使用 MyGo 渲染器将应用图标 SVG 渲染为 PNG | `render`、`matte` |
| `tools/deps` | 比较 MyGo 与 WSL 的固定版本和最新发布 | `check` |
| `internal/core` | 为各进程装配服务；Compose 接线（项目数据与备份、自启） | `Core`、`New`、`BackupTarget`、`WireCompose`、`FindProject`、`ProjectData`、`MoveWSLcData`、`AgentPath` |
| `internal/compose` | Compose：Backend 接口、项目元数据、加载、校验、规划、builtin 与 native Backend、迁移（见 `compose.md`） | `Backend`、`Registry`、`Project`、`Projects`、`Load`、`Translate`、`Diff`、`Builtin`、`Native`、`Migrate` |
| `internal/volume` | 经辅助容器以 tar 导入导出 Volume | `Pipeline`、`Import`、`Export`、`Decompress`、`HelperImage` |
| `internal/backup` | 来源→tar→目录/归档/仓库；目录扫描、校验、恢复、保留（见 `backups.md`） | `Runner`、`DeleteVersion`、`Runner.BackupProject`、`ProjectManifest`、`ReadProjectManifest`、`ExtractProject`、`ProjectVolumeStream`、`FolderWriter`、`ArchiveWriter`、`RepoWriter`、`Scan`、`GroupBySource`、`Open`、`Verify`、`PlanRetention`、`Delete`、`RepoPassword` |
| `internal/scheduler` | 备份计划、到期计算、重试、保留、历史、锁、计划任务注册 | `Service`、`Plan`、`Schedule`、`Retry`、`RunDue`、`Run`、`Install` |
| `internal/restart` | 基于 `wslc events` 的容器重启策略 | `Watcher`、`Decide`、`Label` |
| `internal/wtprofile` | Windows Terminal fragment 配置 | `Fragment`、`ContainerShell`、`DistroShell`、`GUIDFor` |
| `tools/catalog` | 将 `locales/messages.tsv` 合并进 ARB 目录 | `main` |
