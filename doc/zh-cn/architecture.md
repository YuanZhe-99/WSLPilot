# 架构

## 进程

| 进程 | 状态 | 职责 |
|---|---|---|
| `wslpilot`（`cmd/wslpilot`） | 已实现（外壳） | GUI。以当前登录用户运行，从不提权。单实例。 |
| `wslpilot-agent` | 计划中 | GUI 关闭时执行计划备份、重启策略与 Compose 自启。 |
| `wslpilot-cli` | 计划中 | 基于同一组包的脚本与调试入口。 |

## 分层

```
gui  →  domain packages (compose, volume, backup, …)  →  adapters (wsl, wslc)  →  procexec
                       ↘  capability  ↗
```

- 包只能引用其右侧的包。视图从不拼接命令参数。
- **适配层**（`internal/wsl`、`internal/wslc`）把上游命令转换为 Go 类型。解析器是作用于已解码文本的纯函数，
  使用真实运行录制的 fixture（`testdata/<上游版本>/`）测试。Client 接收 `procexec.Runner`，测试可替换为录制输出。
- **`internal/procexec`** 以参数向量启动子进程（从不使用 shell 字符串），设置 `WSL_UTF8=1`、不弹出控制台窗口，
  并将输出从 UTF-16LE 或 UTF-8 解码。
- **`internal/capability`** 一次性探测 WSL、WSLc 与原生 Compose，把 `Report` 交给所有需要决定功能可用性的地方。

## 横切规则

- **状态归上游所有。** 发行版、容器、镜像、网络与 Volume 始终从 CLI 读回。WSLPilot 只持久化自身元数据。
- **UI 线程。** 视图是 `gui.App` 状态的函数。探测与命令在 goroutine 中运行，通过窗口的 `Update` 发布结果；
  进行中的探测不会重复启动。
- **本地化。** 界面字符串来自 `locales/*.json`（ARB），经生成的 `locales` 包调用；从不按本地化标签匹配上游输出。

## 仓库结构

| 路径 | 内容 |
|---|---|
| `cmd/wslpilot` | GUI 入口 |
| `internal/` | 全部应用包（见 `functions/INDEX.md`） |
| `locales/` | ARB 目录与生成的 `catalog_gen.go` |
| `design/` | 界面的静态 HTML 原型，由设计画布导出（见 `design/README.md`） |
| `resources/` | 应用图标（`icon.svg` 为源文件，`icon.png` 由其渲染）与打包资源 |
| `tools/checkdocs` | 文档镜像校验 |
| `tools/icon` | 将 `resources/icon.svg` 渲染为 1024×1024 的 `resources/icon.png` |
| `tools/deps` | 上游版本检查（见 `ci-cd.md`） |
| `scripts/build.sh` | windows/amd64 与 windows/arm64 的发布打包 |
| `.github/workflows` | CI、发布与每周上游检查 |
| `.agents/skills/` | Agent 技能（MyGo 维护） |
