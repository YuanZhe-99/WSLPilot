# 上游契约

这里只记录真实运行中观察到或从官方源码中读到的行为，并注明观察时的版本。未经验证的假设放在 `PLAN.md`。

## 已测试版本

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
| `--help` 列出 `--manage <Distro> --move/--resize/--set-sparse`、`--export --format`、`--import --vhd`、`--import-in-place`、`--update`、`--unregister`。 | 供 WSL 页面使用（计划中）。 |

Fixture：`internal/wsl/testdata/3.0.3.0/`（真实录制）、`internal/wsl/testdata/synthetic/`（手写的本地化输出，已标明）。

## wslc.exe

| 行为 | 处理方式 |
|---|---|
| `wslc info --format json` 返回 `Client`（Version、KernelVersion、WindowsVersion、SettingsFile 等）与 `Server`（SessionManagerVersion、Sessions）。 | `wslc.ParseInfo`；主要的检测入口。 |
| 未知命令退出码为 1。3.0.3.0 中不存在 `wslc compose`。 | `wslc.HasNativeCompose` 将 `wslc compose --help` 退出码 0 视为存在。 |
| 设置文件为 `%LOCALAPPDATA%\wslc\settings.yaml`，可能不存在。 | 路径取自 `wslc info` 的输出。 |
| `wslc run` 没有 `--restart` 选项。 | 重启策略须由 WSLPilot 自行实现（计划中）。 |
| `wslc volume create` 驱动：`guest`（默认）与 `vhd`。 | — |
| `wslc container cp` 可从 `-`（stdin）读取数据流；`-a` 仅为 Docker 兼容而接受。 | 不假定 `cp` 会保留属主。 |

Fixture：`internal/wslc/testdata/3.0.3.0/`（用户名已替换为 `user`）。

## MyGo

| 行为 | 处理方式 |
|---|---|
| 原生 UI 不需要 WebView2；在 Windows 10/11 x64 与 arm64 上用 Direct3D 11 绘制。 | WSLPilot 只使用原生 UI。 |
| `mygo generate` 根据 ARB 目录生成 `locales/catalog_gen.go`。 | 编辑目录后运行；该文件需提交。 |
