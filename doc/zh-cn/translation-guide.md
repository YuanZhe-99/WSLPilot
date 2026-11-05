# 翻译指南

## 镜像规则

- `doc/en-us/` 为权威版本。其它语言目录必须有相同的文件、相同数量的标题与表格行，以及逐字节一致的代码块。
- 由 `go run ./tools/checkdocs` 强制检查。
- 代码标识符、命令、路径与参数在所有语言中保持英文。
- 界面字符串在 `locales/*.json` 中翻译，不在这里。

## 术语表

| English | 简体中文 |
|---|---|
| distribution | 发行版 |
| container | 容器 |
| image | 镜像 |
| volume | Volume |
| bind mount | Bind Mount |
| backend | Backend |
| backup | 备份 |
| snapshot | 快照 |
| restore | 恢复 |
| retention policy | 保留策略 |
| repository (backup) | 仓库 |
| chunk | 数据块 |
| pack (backup) | 包 |
| index (backup) | 索引 |
| prune | 清理 |
| blob | blob |
| compaction (disk) | 压缩 |
| sparse VHD | 稀疏 VHD |
| job | 任务 |
| audit log | 审计日志 |
| encryption | 加密 |
| password | 密码 |
| master key | 主密钥 |
| auto-update | 自动更新 |
| fixture | fixture |
| adapter | 适配层 |
| verified | 已验证 |
