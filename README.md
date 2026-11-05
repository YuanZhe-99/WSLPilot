# WSLPilot

A native Windows 11 (x64 / ARM64) desktop manager for **WSL** distributions and **WSL Containers
(WSLc)**: containers, images, networks, volumes (with TAR import/export), Compose projects and
backups — built with Go and [MyGo](https://github.com/egoist/mygo)'s native UI. No Docker Desktop
or Docker Engine required; everything goes through the official `wsl.exe` and `wslc.exe`, and
stays compatible with using those tools directly.

**Status:** early development (M0). The app shell and WSL / WSLc detection work; see
[`PLAN.md`](PLAN.md) for the roadmap.

Windows 11 原生（x64 / ARM64）桌面工具，统一管理 WSL 发行版与 WSL 容器（WSLc）：容器、镜像、网络、
Volume（含 TAR 导入导出）、Compose 项目与备份。基于 Go 与 MyGo 原生 UI，不依赖 Docker Desktop。
当前处于早期开发阶段（M0），路线图见 [`PLAN.md`](PLAN.md)。

## Build

```bash
go tool mygo dev     # run with live reload
go tool mygo build   # package into build/
```

Requirements and the full check list: [doc/en-us/development.md](doc/en-us/development.md).

## Documentation

- [doc/en-us](doc/en-us/README.md) · [doc/zh-cn](doc/zh-cn/README.md)
- [AGENTS.md](AGENTS.md) — rules for coding agents working on this repository

## License

GPL-3.0 — see [LICENSE](LICENSE).
