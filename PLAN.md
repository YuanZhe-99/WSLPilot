# PLAN：WSLPilot — 剩余工作

> 更新日期：2026-11-05
> 本文只放**尚未完成的工作与待决定事项**。已实现的行为、格式与契约都在 `doc/en-us/`（`architecture.md`、`backups.md`、
> `backup-format.md`、`compose.md`、`app-data.md`、`upstream.md`），这里不再重复。全部完成后删除本文件。

## 0. 已完成（摘要）

M0–M6 的范围均已实现并有测试：WSL 发行版全生命周期（安装、导入导出、迁移、调整大小、稀疏、压缩、转换、注销、默认用户、包更新）、
`.wslconfig` / `wsl.conf` / `settings.yaml` 表单与高级编辑、WSLc 容器 / 镜像 / 网络 / 日志 / 终端 / 编辑配置（安全导出后重建）、
WSLc 数据位置迁移、自研 Compose（builtin Backend、校验、规划、迁移接口、Volume native/bind 选择）、Volume TAR 导入导出、
三种备份（目录、归档、wpbk 增量仓库）及以恢复为中心的目录、计划与保留策略、`wslpilot-agent`（计划任务、重启策略、Compose 自启）、
`wslpilot-cli`、托盘、登录自启、通知、主题、Windows Terminal fragment、CI / 发布 / 上游检查工作流。

## 1. 待实机验证

只有实际运行后才能在 `platform-notes.md` 中标为已验证。

| 项目 | 说明 |
|---|---|
| diskpart 回退压缩（UAC 路径） | 需要一台没有 `--manage --compact` 的 WSL，或对 WSLc `vhd` Volume 手动执行一次 |
| `wsl --export --format vhd` | 一次中断的导出后报 `ERROR_SHARING_VIOLATION`；需在 `wsl --shutdown` 后重测（会停止用户的发行版，需用户同意） |
| WSLc 数据位置迁移 | 会影响用户真实的 WSLc session，只能在用户同意后或在专用测试机上运行 |
| 终端插件、托盘、通知、计划任务实际触发 | 需要人工在原生窗口中操作一次；打包后的应用已能启动 |
| Windows 11 x64 | CI 会在 `windows-latest` 上运行测试与打包；实机安装与升级尚未验证 |
| 多 WSLc session 的对象可见性 | WSLPilot 固定使用默认 CLI session |
| 已设为 sparse 的 VHDX 再压缩的效果 | 对照 `--compact` 前后大小 |

## 2. M7 剩余：发布

1. **自动更新**：MyGo 的签名更新需要 `mygo keygen` 生成的密钥对（私钥放 GitHub secret `MYGO_UPDATER_PRIVATE_KEY`，公钥写入
   `mygo.json` 的 `updates`），以及应用内的检查 / 安装入口。密钥属于用户，**待用户生成并决定更新源**（GitHub Releases）。
2. **代码签名**：Windows 安装包与 exe 的 Authenticode 签名（证书待定）。
3. **16px 托盘小图标**：当前由 1024px 图标缩小得到，细节可能需要专门简化。
4. **发布流程演练**：用户确认版本号后打 tag，观察 `release.yml` 产出两个架构的安装包。

## 3. M8：原生 Compose

上游 `wsl compose` / `wslc compose` 发布后：实现 `compose.Native` 的各方法、在兼容矩阵中跑通 contract 测试、
在项目页面提供“迁移 Backend”向导（`compose.Migrate` 已实现，确认后才执行，失败回滚）。在此之前项目始终使用 builtin。

## 4. 待用户决定

1. 正式产品名（目前为 WSLPilot）。
2. 许可证（暂定 GPL-3.0）。
3. wpbk 是否需要加密（格式中 `encryption` 字段已预留）。
4. Volume 辅助容器镜像的来源：目前首次使用时拉取 `alpine:3.20`；可改为自带的最小 rootfs（`wslc image import`）以离线可用、固定摘要。
5. 自动更新密钥与签名证书（见 §2）。
