# 应用数据

WSLPilot 只保存自身的元数据（行为契约：状态归上游所有）。所有数据位于 `%APPDATA%\WSLPilot`，设置了 `$WSLPILOT_DATA`
时位于该目录（测试、便携使用）。

## 文档

每个 JSON 文档都是由 `internal/store` 原子写入（临时文件、fsync、重命名）的信封：

```json
{ "schemaVersion": 1, "data": { } }
```

遇到 `schemaVersion` 比自身所知更新的文档时，程序拒绝读取（`ErrNewerSchema`），也绝不改写它。

| 路径 | 内容 | Schema |
|---|---|---|
| `settings.json` | `theme`（`system`、`light`、`dark`）、`language`（`""` 为跟随 Windows，`en`、`zh-Hans`；MyGo 自己也会保存该选择）、`backupDir`、`helperImage`（Volume 辅助镜像，留空为 `alpine:3.20`）；窗口：`closeQuits`、`closeHinted`、`relaunchHidden`、`sidebarRail`（侧边栏收起为图标）、`loginWindow`（登录时打开窗口，而不是只在托盘中启动）；更新：`noUpdateCheck`、`lastUpdateCheck`、`announcedVersion`、`autoInstall`（`""`、`idle`、`daily`）、`autoInstallHour`（见 `gui.md`） | 1 |
| `locations.json` | 从外部打开的备份位置：`path`、`readOnly`、`label` | 1 |
| `projects/<id>/project.json` | 一个 Compose 项目（见 [compose.md](compose.md)） | 1 |
| `plans/<id>.json` | 一个备份计划（见 [backups.md](backups.md)） | 1 |
| `state/<id>.json` | 计划的 `lastRun`、`lastResult`、`nextRun`、`attempt`、`lastSuccess` | 1 |

## 日志与锁

| 路径 | 内容 |
|---|---|
| `history/<plan id>.jsonl` | 每次计划运行一条记录：`start`、`end`、`result`、`items`、`expired`、`attempt` |
| `logs/audit.jsonl` | 破坏性操作：`time`、`action`（例如 `wsl.unregister`）、`target`、`detail`、`result` |
| `logs/agent.log` | wslpilot-agent 的日志，超过 1 MiB 时轮转 |
| `locks/scheduler.lock` | 正在运行计划的进程 ID 与时间 |

## 应用数据之外的文件

| 路径 | 写入时机 |
|---|---|
| `<备份目录>`（默认 `%USERPROFILE%\WSLPilot Backups`） | 手动备份（`Repository\`）、危险操作前的安全备份（`Safety\`） |
| `<备份目录>\Safety\container-<name>-<time>.tar` | “编辑配置”替换容器之前导出的容器自身文件 |
| `<备份目录>\WSLc move <time>\` | 迁移 WSLc 数据到新 `storagePath` 时导出的镜像（`images.tar`）与 Volume（`volumes\*.tar`）；旧 session 存储也保留在原处 |
| `<file>.wslpilot-<time>.bak` | 替换 Windows 配置文件（`.wslconfig`、`settings.yaml`、Compose 文件）之前 |
| `/etc/wsl.conf.wslpilot-<time>` | 在发行版内替换其 `wsl.conf` 之前 |
| `%LOCALAPPDATA%\Microsoft\Windows Terminal\Fragments\WSLPilot\wslpilot.json` | WSLPilot 添加的 Windows Terminal 配置 |
| 计划任务 `WSLPilot\RunDue`、`HKCU\…\Run\WSLPilotAgent` | 安装后台 agent 时 |
