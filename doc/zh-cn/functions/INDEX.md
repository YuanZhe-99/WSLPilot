# 包索引

| 包 | 职责 | 关键声明 |
|---|---|---|
| `cmd/wslpilot` | GUI 入口：单实例、主题、主窗口 | `main` |
| `internal/version` | 发布版本号，与 `mygo.json` 保持一致 | `Version` |
| `internal/upstream` | WSLPilot 测试过的上游版本（`tested.json`） | `Tested`、`Manifest`、`Compare`、`NewerThanTested` |
| `internal/procexec` | 以参数向量启动子进程；输出解码 | `Runner`、`Run`、`Decode`、`ExitError` |
| `internal/wsl` | `wsl.exe` 适配层 | `Client`、`Version`、`ParseVersion`、`ParseNames`、`ErrNoVersion` |
| `internal/wslc` | `wslc.exe` 适配层 | `Client`、`Info`、`ParseInfo`、`HasNativeCompose` |
| `internal/capability` | 对 WSL、WSLc 与原生 Compose 的一次合并探测 | `Prober`、`Report` |
| `internal/gui` | 原生 UI：侧边栏、页面、概览 | `App`、`New`、`App.View`、`App.Refresh` |
| `locales` | 由 `locales/*.json` 生成的消息访问函数 | `catalog_gen.go` |
| `tools/checkdocs` | 校验 `doc/` 语言镜像 | `main` |
| `tools/icon` | 使用 MyGo 渲染器将应用图标 SVG 渲染为 PNG | `render`、`matte` |
| `tools/deps` | 比较 MyGo 与 WSL 的固定版本和最新发布 | `check` |
