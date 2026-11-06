<p align="center"><img src="resources/icon.png" width="96" alt="WSLPilot icon"></p>

<h1 align="center">WSLPilot</h1>

<p align="center">
A desktop app for managing WSL and WSL Containers on Windows 11.<br>
<a href="https://github.com/YuanZhe-99/WSLPilot/releases/latest">Download</a> ·
<a href="doc/en-us/version-history.md">What's new</a> ·
<a href="#中文">中文</a>
</p>

WSLPilot puts your WSL distributions, containers, Compose projects and backups in one window.
It is a front end for the `wsl.exe` and `wslc.exe` that come with WSL: it does not need Docker
Desktop, and it keeps no state of its own about your distributions or containers, so you can keep
using the command line alongside it.

It runs on Windows 11 x64 and ARM64. Container features need WSL 3.0 or later, which includes
WSL Containers. The current version is 0.1.0, tested with WSL 3.0.3.

## What it does

**Distributions.** Install, import and export them, start and stop them, convert between WSL 1
and 2, move one to another drive or resize its disk. WSLPilot can also shrink a virtual disk
that has grown after you deleted files, a common annoyance with WSL 2.

**Settings without hand-editing.** `.wslconfig`, each distribution's `wsl.conf` and the
container settings are shown as forms with explanations. You can still edit the files directly;
either way only the lines you change are touched, you see the changes before saving, and the old
file is kept as a backup.

**Containers.** Containers, images, networks and logs, a shell into any running container, and
restart policies, which WSL Containers doesn't have on its own.

**Compose.** Run `compose.yaml` projects on WSL Containers. Anything WSLPilot can't run is
reported clearly instead of being silently skipped.

**Volumes.** Import and export volume contents as tar files without losing Linux file owners or
permissions.

**Backups you can restore anywhere.** Back up distributions, volumes and Compose projects as
plain folders, single archives, or an incremental repository that stores only what changed and
can be password-protected. You can open any backup folder or tar file, including ones from
another PC, and restore it. Before anything is overwritten or deleted, WSLPilot makes a safety
backup first. Scheduled backups keep running in the background when the window is closed.

It also has a tray icon, light and dark themes, English and Simplified Chinese, and updates
itself.

## Install

Download `wslpilot-setup-0.1.0-amd64.exe` (Intel and AMD) or `wslpilot-setup-0.1.0-arm64.exe`
(ARM) from the [latest release](https://github.com/YuanZhe-99/WSLPilot/releases/latest) and run
it. It installs for your account only and keeps itself up to date. Portable `.zip` builds are
there too, but they don't update themselves.

The installers aren't code-signed yet, so Windows may show a SmartScreen warning the first time;
choose **More info → Run anyway**.

## Build from source

```bash
go tool mygo dev                                   # run with live reload
scripts/build.sh -skip-installer windows/arm64     # package the app, agent and CLI into build/
```

See [doc/en-us/development.md](doc/en-us/development.md) for requirements and the full check
list, and [doc/en-us](doc/en-us/README.md) ([中文](doc/zh-cn/README.md)) for how WSLPilot works.

## Acknowledgments

- [WSL](https://github.com/microsoft/WSL) and WSL Containers by Microsoft, which do the real work.
- [MyGo](https://github.com/egoist/mygo) by egoist, the Go app framework and native UI WSLPilot
  is built on.
- [Ghostty](https://github.com/ghostty-org/ghostty) (libghostty-vt), the terminal emulator inside
  WSLPilot's terminal.
- [compose-go](https://github.com/compose-spec/compose-go) from the Compose Specification
  project, for reading Compose files.
- [klauspost/compress](https://github.com/klauspost/compress) (zstd) and
  [zeebo/blake3](https://github.com/zeebo/blake3) for backups, and the Go team's
  [x/sys](https://pkg.go.dev/golang.org/x/sys) and [x/crypto](https://pkg.go.dev/golang.org/x/crypto).
- [Alpine Linux](https://alpinelinux.org) and BusyBox, whose small image carries volume
  transfers.
- The penguin in the icon is drawn after Tux by Larry Ewing (lewing@isc.tamu.edu), made with
  [The GIMP](https://www.gimp.org).

Full license details are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## License

GPL-3.0. See [LICENSE](LICENSE).

---

## 中文

WSLPilot 是一个在 Windows 11 上管理 WSL 与 WSL 容器的桌面应用，把发行版、容器、Compose 项目和备份放在同一个窗口里。
它是 WSL 自带的 `wsl.exe` 与 `wslc.exe` 的图形界面：不需要 Docker Desktop，也不另外保存发行版或容器的状态，
所以你可以照常同时使用命令行。

支持 Windows 11 x64 与 ARM64。容器功能需要 WSL 3.0 及以上（自带 WSL 容器）。当前版本 0.1.0，在 WSL 3.0.3 上测试。

**能做什么**

- **发行版**：安装、导入导出、启停、WSL 1/2 互转、迁移到其它驱动器、调整磁盘大小；还能把删除文件后依然膨胀的虚拟磁盘压缩回实际大小。
- **不用手改的设置**：`.wslconfig`、各发行版的 `wsl.conf` 和容器设置都以带说明的表单呈现。也可以直接编辑文件；
  无论哪种方式，都只改动你修改的行，保存前能看到差异，旧文件会留作备份。
- **容器**：容器、镜像、网络、日志，进入运行中容器的 Shell，以及 WSL 容器本身没有的重启策略。
- **Compose**：在 WSL 容器上运行 `compose.yaml` 项目；无法支持的配置会明确报告，而不是悄悄跳过。
- **Volume**：以 tar 文件导入导出 Volume 内容，不丢失 Linux 文件的属主与权限。
- **随处可恢复的备份**：发行版、Volume 和 Compose 项目可以备份为普通目录、单个归档，或只保存变化部分、可设密码的增量仓库。
  任何备份目录或 tar 文件（包括来自其它电脑的）都能打开并恢复。覆盖或删除任何东西之前，WSLPilot 都会先做一次安全备份。
  定时备份在窗口关闭后也会在后台继续运行。
- 另有托盘图标、浅色与深色主题、中英文界面和自动更新。

**安装**：从 [最新 Release](https://github.com/YuanZhe-99/WSLPilot/releases/latest) 下载
`wslpilot-setup-0.1.0-amd64.exe`（Intel、AMD）或 `wslpilot-setup-0.1.0-arm64.exe`（ARM）运行即可。
它只为当前账户安装，并会自动更新。也提供便携版 `.zip`，但不会自动更新。安装包尚未进行代码签名，首次运行时 Windows 可能显示
SmartScreen 警告，选择“更多信息 → 仍要运行”即可。

**鸣谢**：微软的 WSL 与 WSL 容器；egoist 的 MyGo；Ghostty（libghostty-vt）；Compose Specification 的 compose-go；
klauspost/compress、zeebo/blake3 与 Go 团队的 x/sys、x/crypto；Alpine Linux 与 BusyBox；图标中的企鹅仿照 Larry Ewing
（lewing@isc.tamu.edu）用 The GIMP 绘制的 Tux。完整许可信息见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

以 GPL-3.0 授权。
