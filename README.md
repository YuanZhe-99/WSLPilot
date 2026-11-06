<p align="center"><img src="resources/icon.png" width="96" alt="WSLPilot icon"></p>

# WSLPilot

A native Windows 11 (x64 / ARM64) desktop manager for **WSL** distributions and **WSL Containers
(WSLc)**, built with Go and [MyGo](https://github.com/egoist/mygo)'s native UI. It drives the
official `wsl.exe` and `wslc.exe` only — no Docker Desktop or Docker Engine — and everything you
do with those tools directly shows up in WSLPilot as it is.

**Current version: 0.1.0** · [Download](https://github.com/YuanZhe-99/WSLPilot/releases/latest) ·
[Version history](doc/en-us/version-history.md)

[中文说明](#中文)

## Features

- **WSL distributions**: install, import and export (tar, tar.gz, tar.xz, VHD), start and stop,
  default distribution, WSL 1/2 conversion, move to another drive, resize, sparse disks, **disk
  compaction** (`wsl --manage --compact`, or diskpart in a separate administrator window on older
  WSL), default user, package updates, terminal.
- **Settings as forms**: `.wslconfig`, `wsl.conf` and WSLc's `settings.yaml` with plain-language
  labels; editing the raw file is under Advanced. Only changed lines are rewritten, changes are
  previewed, and the file is backed up first.
- **WSL Containers**: containers, images, networks, logs, shell, run, edit configuration (with a
  safety export), restart policies (which WSLc lacks), moving container data to another folder,
  switching between WSLc sessions.
- **Compose**: a built-in backend for `compose.yaml` that reports every unsupported key instead
  of ignoring it; each project keeps its backend until you migrate it, so a future native WSL
  Compose never takes over by itself.
- **Volumes**: TAR import and export unpacked inside Linux, so owners, permissions and links are
  kept.
- **Backups, restore first**: browsable folders, single archives, or an incremental repository
  (deduplicated, optionally encrypted). Open backups from anywhere — another PC, a NAS, a plain
  tar — check them, and restore as new or over existing objects (a safety backup is made first).
  Plans with schedules, retention and retries run in the background without administrator rights.
- Tray, open at sign-in, notifications, light and dark themes, English and Simplified Chinese,
  signed automatic updates.

Requires Windows 11 with WSL 2. Container features need WSL 3.0 or later (WSL Containers).
Tested with WSL 3.0.3.0.

## Install

Download `wslpilot-setup-<version>-<arch>.exe` (x64: `amd64`, ARM: `arm64`) from the
[latest release](https://github.com/YuanZhe-99/WSLPilot/releases/latest) and run it; it installs
for the current user and updates itself. A portable `.zip` is there too (no automatic updates).
The builds are not code-signed yet, so Windows SmartScreen may ask you to confirm the first run.

## Build from source

```bash
go tool mygo dev                                   # run with live reload
scripts/build.sh -skip-installer windows/arm64     # package the app, agent and CLI into build/
```

Requirements and the full check list: [doc/en-us/development.md](doc/en-us/development.md).

## Documentation

- [doc/en-us](doc/en-us/README.md) · [doc/zh-cn](doc/zh-cn/README.md)
- [AGENTS.md](AGENTS.md) — rules for coding agents working on this repository

## License

GPL-3.0 — see [LICENSE](LICENSE). Third-party credits, including the Tux-based icon:
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## 中文

WSLPilot 是 Windows 11（x64 / ARM64）原生桌面工具，统一管理 **WSL 发行版**与 **WSL 容器（WSLc）**。
基于 Go 与 MyGo 原生 UI，只调用官方 `wsl.exe` 与 `wslc.exe`，不依赖 Docker Desktop；你直接用这两个工具做的修改，
WSLPilot 都会如实显示。当前版本 **0.1.0**，[下载](https://github.com/YuanZhe-99/WSLPilot/releases/latest)。

- **WSL 发行版**：安装、导入导出（tar、tar.gz、tar.xz、VHD）、启停、默认发行版、WSL 1/2 转换、迁移到其它驱动器、
  调整大小、稀疏磁盘、**压缩磁盘**、默认用户、软件包更新、终端。
- **设置表单化**：`.wslconfig`、`wsl.conf` 与 WSLc 的 `settings.yaml` 以通俗的表单呈现，直接编辑原文件归入“高级”；
  只改动修改过的行，保存前预览差异并备份。
- **WSL 容器**：容器、镜像、网络、日志、Shell、运行、编辑配置（先做安全导出）、重启策略、迁移容器数据、切换 WSLc session。
- **Compose**：内置 Backend，不支持的键一律报告而不是忽略；项目的 Backend 固定，未来的原生 Compose 不会自行接管。
- **Volume**：TAR 导入导出在 Linux 内解包，保留属主、权限与链接。
- **以恢复为中心的备份**：可浏览的目录、单个归档或增量仓库（去重，可选加密）。可打开任意来源的备份（其它电脑、NAS、普通 tar），
  校验后恢复为新对象或覆盖已有对象（会先做安全备份）。计划支持时间表、保留策略与重试，无需管理员权限即可在后台运行。
- 托盘、登录时启动、通知、浅色与深色主题、中英文界面、签名自动更新。

安装：从 [最新 Release](https://github.com/YuanZhe-99/WSLPilot/releases/latest) 下载对应架构的
`wslpilot-setup-<version>-<arch>.exe` 运行即可（按当前用户安装，可自动更新）；也提供便携版 `.zip`（不自动更新）。
安装包尚未进行代码签名，首次运行时 Windows SmartScreen 可能需要确认。

需要 Windows 11 与 WSL 2；容器功能需要 WSL 3.0 及以上（WSL Containers）。以 GPL-3.0 授权。
