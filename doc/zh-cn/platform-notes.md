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
| diskpart 压缩（提权） | 已验证 | 实机测试 `internal/core`（已通过 UAC）：WSLc vhd Volume 308 MB → 14 MB |
| `wpbk` 仓库 | 已验证 | 单元、故障注入与规范测试 |
| `wpbk` 加密、计划密码（DPAPI） | 已验证 | 单元、规范与泄露测试；加密计划端到端测试 |
| 自动更新 | 已实现 | 由发布工作流签名；0.1.0 发布后才能进行首次更新检查 |
| Volume tar 导入导出（属主、权限、链接） | 已验证 | 实机测试 `internal/volume` |
| 发行版备份（目录、归档、仓库）、校验、恢复为新发行版、部分恢复 | 已验证 | 在 `wslpilot-test-d1` 上运行实机测试 `internal/backup` |
| Compose builtin Backend 的 up / 更新 / down | 已验证 | 实机测试 `internal/compose`（`wslpilot-test-compose`） |
| 调度、保留策略、重试 | 已实现 | 单元测试；已手动验证用户级 `schtasks /Create /SC MINUTE` |
| 重启监视 | 已实现 | 基于录制事件的单元测试 |
| GUI 页面 | 已实现 | 视图测试，页面渲染已目检；打包后的应用可启动（10 秒冒烟运行） |
| 终端插件、托盘、通知、UAC 提示 | 已实现 | 尚未手动操作验证 |
| 容器编辑配置（安全导出、校验后重建） | 已验证 | 实机测试 `internal/gui`（`wslpilot-test-edit`） |
| WSLc 数据位置迁移 | 已验证 | 在空 session 上运行实机测试 `internal/core`：镜像与 Volume 已迁移、内容已校验、settings.yaml 已恢复 |

## Windows 11 x64

| 功能 | 状态 | 依据 |
|---|---|---|
| 全部 | 尚未验证 | — |
