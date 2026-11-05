# 平台说明

已实现的内容，以及在何处验证。「已验证」指在所列机器上真实运行过。

## Windows 11 ARM64

| 功能 | 状态 | 依据 |
|---|---|---|
| 单元测试与视图测试 | 已验证 | build 26300 上运行 `go test ./...` |
| WSL 版本与发行版检测 | 已验证 | WSL 3.0.3.0 上录制的 fixture |
| WSLc 检测、原生 Compose 探测 | 已验证 | WSLc 3.0.3.0 上录制的 fixture |
| WSL 发行版列表、生命周期、压缩、稀疏、迁移 | 已验证 | 在 `wslpilot-test-d1` 上实机运行（WSL 3.0.3.0），fixture |
| WSLc 列表、inspect、事件、exec 数据流 | 已验证 | 在 `wslpilot-test-*` 对象上实机运行（WSLc 3.0.3.0），fixture |
| 配置编辑（`.wslconfig`、`wsl.conf`、`settings.yaml`） | 已实现 | 单元测试 |
| diskpart 压缩（提权） | 已实现 | 单元测试；尚未实际走过 UAC |
| `wpbk` 仓库 | 已验证 | 单元、故障注入与规范测试 |
| GUI 外壳（侧边栏、概览） | 已实现 | 视图测试；尚未检查原生窗口 |

## Windows 11 x64

| 功能 | 状态 | 依据 |
|---|---|---|
| 全部 | 尚未验证 | — |
