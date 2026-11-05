# 包索引

| 包 | 职责 | 关键声明 |
|---|---|---|
| `cmd/wslpilot` | GUI 入口：单实例、主题、主窗口 | `main` |
| `internal/version` | 发布版本号，与 `mygo.json` 保持一致 | `Version` |
| `internal/upstream` | WSLPilot 测试过的上游版本（`tested.json`） | `Tested`、`Manifest`、`Compare`、`NewerThanTested` |
| `internal/procexec` | 以参数向量启动子进程；输出解码；原始数据流 | `Runner`、`Run`、`Streamer`、`Stream`、`Spec`、`Decode`、`ExitError` |
| `internal/wsl` | `wsl.exe` 适配层：列表、生命周期、导入导出、迁移、压缩、稀疏、exec、文件读写 | `Client`、`Distro`、`List`、`ParseListVerbose`、`ParseListOnline`、`InstallArgs`、`ExecArgs`、`Compact`、`ReadFile`、`WriteFile` |
| `internal/wslc` | `wslc.exe` 适配层：容器、镜像、Volume、网络、stats、事件、build | `Client`、`Container`、`ContainerSpec`、`CreateArgs`、`Volume`、`Network`、`Event`、`ParseLines`、`ParseArray`、`HasNativeCompose` |
| `internal/capability` | 对 WSL、WSLc 与原生 Compose 的一次合并探测 | `Prober`、`Report` |
| `internal/store` | WSLPilot 元数据：带版本的 JSON 文档，原子写入 | `Store`、`Default`、`Load`、`Save`、`WriteFileAtomic`、`ErrNewerSchema` |
| `internal/jobs` | 长任务：进度、取消、日志、破坏性操作的审计日志 | `Manager`、`Job`、`Snapshot`、`Audit`、`ProgressWriter`、`FormatBytes` |
| `internal/configdoc` | 以原始文档方式编辑 `.wslconfig`、`wsl.conf`、`settings.yaml`；表单键模式；带冲突检测的保存 | `INI`、`YAML`、`Field`、`WSLConf`、`WSLConfig`、`WSLcSettings`、`ApplyINI`、`ApplyYAML`、`Snapshot.Save`、`DiffLines` |
| `internal/vhdx` | 用 diskpart 压缩 WSL 自身无法压缩的 VHDX | `Script`、`Compact`、`Elevator`、`ErrInUse` |
| `internal/elevate` | 通过 UAC 提示启动单个提权子进程 | `Run`、`ErrDeclined` |
| `internal/backup/wpbk` | 增量备份仓库（见 `backup-format.md`） | `Init`、`Open`、`Repo.Backup`、`Restore`、`Entries`、`Check`、`Forget`、`ApplyPolicy`、`Prune`、`RebuildIndex` |
| `internal/gui` | 原生 UI：侧边栏、页面、概览 | `App`、`New`、`App.View`、`App.Refresh` |
| `locales` | 由 `locales/*.json` 生成的消息访问函数 | `catalog_gen.go` |
| `tools/checkdocs` | 校验 `doc/` 语言镜像 | `main` |
| `tools/icon` | 使用 MyGo 渲染器将应用图标 SVG 渲染为 PNG | `render`、`matte` |
| `tools/deps` | 比较 MyGo 与 WSL 的固定版本和最新发布 | `check` |
