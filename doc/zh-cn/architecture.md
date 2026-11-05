# 架构

## 进程

| 进程 | 状态 | 职责 |
|---|---|---|
| `wslpilot`（`cmd/wslpilot`） | 已实现 | GUI。以当前登录用户运行，从不提权。单实例；关闭窗口后保留在托盘。打开期间每分钟运行到期的备份计划。 |
| `wslpilot-agent`（`cmd/wslpilot-agent`） | 已实现 | `run-due`（计划任务，每 15 分钟）与 `watch`（登录时：重启策略、Compose 自启、到期计划）。编译为无控制台窗口程序。 |
| `wslpilot-cli`（`cmd/wslpilot-cli`） | 已实现 | 脚本与调试：状态、列表、备份、目录、校验、恢复为新对象、压缩、计划。 |
| 提权子进程 | 已实现 | 仅用于 WSL 自身无法完成的压缩，运行 `diskpart`，通过 UAC 提示启动（`internal/elevate`）。 |

## 分层

```
gui, cmd  →  core  →  domain packages  →  adapters (wsl, wslc)  →  procexec
                       compose, backup, volume, scheduler, restart,
                       configdoc, vhdx, wtprofile, jobs, store
```

- 包只能引用其右侧（或业务层中同层）的包，不能向上引用。视图从不拼接命令参数；所有参数向量都由适配层构造。
- **适配层**（`internal/wsl`、`internal/wslc`）把上游命令转换为 Go 类型。解析器是作用于已解码文本的纯函数，
  使用真实运行录制的 fixture（`testdata/<上游版本>/`）测试。Client 接收 `procexec.Runner` 与 `procexec.Streamer`，测试可替换为录制输出。
- **`internal/procexec`** 以参数向量启动子进程（从不使用 shell 字符串），设置 `WSL_UTF8=1`、不弹出控制台窗口，
  将输出从 UTF-16LE 或 UTF-8 解码，并为 tar 管道提供原始数据流。
- **`internal/core`** 为每个进程一次性构建各项服务（适配层、存储、任务、备份执行器、调度器、Compose 注册表）。
- **`internal/capability`** 探测 WSL、WSLc 与原生 Compose，把 `Report` 交给所有需要决定功能可用性的地方。

## 横切规则

- **状态归上游所有。** 发行版、容器、镜像、网络与 Volume 始终从 CLI 读回。WSLPilot 只持久化自身元数据（[app-data.md](app-data.md)）。
- **UI 线程。** 视图是 `gui.App` 状态的函数。命令在 goroutine（`App.async`）或任务（`internal/jobs`）中运行，
  通过窗口的 `Update` 发布结果；视图不做 I/O。
- **危险操作**统一经过一个确认对话框：提供“先备份”（默认勾选），可能要求输入对象名称，并写入审计日志。
- **长操作即任务**：进度、取消、日志、最终状态，耗时较长时发送通知。
- **本地化。** 界面字符串来自 `locales/*.json`（ARB），经生成的 `locales` 包调用；其中大部分以 `locales/messages.tsv`
  为源（`go run ./tools/catalog`）。从不按本地化标签匹配上游输出。

## 仓库结构

| 路径 | 内容 |
|---|---|
| `cmd/wslpilot`、`cmd/wslpilot-agent`、`cmd/wslpilot-cli` | 三个程序 |
| `internal/` | 全部应用包（见 `functions/INDEX.md`） |
| `locales/` | ARB 目录、其 TSV 源文件与生成的 `catalog_gen.go` |
| `design/` | 界面的静态 HTML 原型，由设计画布导出（见 `design/README.md`） |
| `resources/` | 应用图标（`icon.svg` 为源文件，`icon.png` 由其渲染）与打包资源 |
| `tools/checkdocs` | 文档镜像校验 |
| `tools/icon` | 将 `resources/icon.svg` 渲染为 1024×1024 的 `resources/icon.png` |
| `tools/catalog` | 将 `locales/messages.tsv` 合并进 ARB 目录 |
| `tools/deps` | 上游版本检查（见 `ci-cd.md`） |
| `scripts/build.sh` | windows/amd64 与 windows/arm64 的发布打包 |
| `.github/workflows` | CI、发布与每周上游检查 |
| `.agents/skills/` | Agent 技能（MyGo 维护） |
