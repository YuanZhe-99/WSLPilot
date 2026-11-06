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
| 自动更新（应用内流程） | 已实现 | 自动安装时机的单元测试；已在开发构建中确认“此构建不提供更新”；实际更新需要比最新版本旧的已安装版本 |
| Volume tar 导入导出（属主、权限、链接） | 已验证 | 实机测试 `internal/volume` |
| 发行版备份（目录、归档、仓库）、校验、恢复为新发行版、部分恢复 | 已验证 | 在 `wslpilot-test-d1` 上运行实机测试 `internal/backup` |
| 删除备份版本（仓库：合并到下一个版本，删除最后一个时删除仓库） | 已验证 | 针对仓库的单元测试（其余版本逐字节还原并通过检查）；通过确认对话框的视图测试 |
| Compose builtin Backend 的 up / 更新 / down | 已验证 | 实机测试 `internal/compose`（`wslpilot-test-compose`） |
| WSLc 清理（prune） | 已验证 | 在 `wslpilot-test-prune-*` 上实机运行 `container` 与 `network prune --filter label=…`（WSLc 3.0.3.0）；Volume 规则的单元测试；所执行命令的视图测试 |
| 整帧绘制而非局部重绘 | 已验证 | `GODEBUG=inittrace=1`：`internal/earlyenv` 先于 MyGo 渲染包初始化；`MYGO_FRAME_STATS=all`：没有局部帧（`MYGO_PARTIAL=1` 时 14 帧中有 10 帧为局部） |
| Compose 项目备份（文件、bind 目录、命名 Volume）与还原 | 已验证 | 实机测试 `internal/core`（`wslpilot-test-pbk`）：停止后再次启动，提取出 bind 文件，还原的 Volume 保留属主；单元测试覆盖目录、归档与仓库 |
| 发行版导出为 VHD 并导入副本 | 已验证 | 在 `wslpilot-test-vhd` 上于 `wsl --shutdown` 后手动验证 |
| WSLc session：隔离、容器页切换 | 已验证 | 用 `session enter` 挂载第二个 session 手动验证；视图测试 |
| 调度、保留策略、重试 | 已实现 | 单元测试；已手动验证用户级 `schtasks /Create /SC MINUTE` |
| 重启监视 | 已实现 | 基于录制事件的单元测试 |
| GUI 页面（0.1.1 重新设计） | 已验证 | 视图测试；全部页面以浅色与深色渲染并与 `design/` 对照；在 26300 版本上检查原生窗口（`go build` 构建，非安装包）：全部页面、设置面板、外观切换、最大化与还原、关闭到托盘、有数据的容器与 Volume 表格（`wslpilot-test-ui-*`）；0.1.2 用 `mygo-automation` 驱动：带“数据”卡片的 Compose 页面、在 Volume 页面中打开、Volume 页面显示所属项目、通过窗口完成项目备份与 Volume 还原、计划编辑器以及切换语言 |
| 终端插件、托盘菜单、通知 | 已实现 | 尚未手动操作验证（关闭到托盘已验证） |
| 容器编辑配置（安全导出、校验后重建） | 已验证 | 实机测试 `internal/gui`（`wslpilot-test-edit`） |
| WSLc 数据位置迁移 | 已验证 | 在空 session 上运行实机测试 `internal/core`：镜像与 Volume 已迁移、内容已校验、settings.yaml 已恢复 |

## Windows 11 x64

| 功能 | 状态 | 依据 |
|---|---|---|
| 全部 | 尚未验证 | — |
