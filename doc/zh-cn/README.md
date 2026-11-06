# WSLPilot 文档

**本文档是代码的权威描述。** `doc/en-us/` 为源文档；`doc/zh-cn/` 逐文件镜像。计划与未完成工作不写在这里，
只存在于仓库根目录的 `PLAN.md`。

WSLPilot 是 Windows 11 原生桌面工具，用于管理 WSL 发行版与 WSL 容器（WSLc），使用 Go 与
[MyGo](https://github.com/egoist/mygo) 原生 UI 编写。它调用官方 `wsl.exe` 与 `wslc.exe` 命令行工具，
从不依赖 Docker Desktop 或 Docker Engine。

| 文档 | 内容 |
|---|---|
| [architecture.md](architecture.md) | 进程、包、分层、横切规则 |
| [upstream.md](upstream.md) | 已验证的 `wsl.exe`、`wslc.exe` 与 MyGo 行为及特殊情况 |
| [platform-notes.md](platform-notes.md) | 各平台已实现与已验证的内容 |
| [development.md](development.md) | 环境、命令、开发循环 |
| [ci-cd.md](ci-cd.md) | 工作流、打包、上游更新流程 |
| [backup-format.md](backup-format.md) | `wpbk` 增量仓库格式，版本 1 |
| [backups.md](backups.md) | 备份类型、目录与归档格式、目录扫描、恢复、计划、重启策略 |
| [compose.md](compose.md) | Compose Backend、项目元数据、支持的键、迁移 |
| [app-data.md](app-data.md) | WSLPilot 保存什么、保存在哪里 |
| [gui.md](gui.md) | 窗口外壳、设置面板、应用内更新与各页面 |
| [translation-guide.md](translation-guide.md) | 镜像规则与术语表 |
| [version-history.md](version-history.md) | 发布记录及其测试所用的上游版本 |
| [functions/INDEX.md](functions/INDEX.md) | 各包职责与关键声明 |

## 固定版本

| 组件 | 版本 | 权威来源 |
|---|---|---|
| Go | 1.27.3 | `go.mod` |
| MyGo | v0.8.1 | `go.mod` |
| WSLPilot | 0.1.4 | `internal/version`、`mygo.json` |
| WSL（已测试，不随附） | 3.0.3.0 | `internal/upstream/tested.json`、`upstream.md` |
| WSLc（已测试，不随附） | 3.0.3.0 | `upstream.md` |
