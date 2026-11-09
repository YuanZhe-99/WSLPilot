# Translation guide

## Mirror rules

- `doc/en-us/` is authoritative. Every other language directory has the same files, the same
  number of headings and table rows, and byte-identical fenced code blocks.
- `go run ./tools/checkdocs` enforces this.
- Code identifiers, commands, paths and flags stay in English in every language.
- Interface strings are translated in `locales/*.json`, not here.

## Glossary

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
| full (backup version) | 全量 |
| incremental | 增量 |
| restore | 恢复 |
| retention policy | 保留策略 |
| Tasks (page) | 任务 |
| settings sheet | 设置面板 |
| Data (project card) | 数据 |
| Folder (Windows, bind) | 目录 |
| External volume | 外部 Volume |
| Storage (of a named volume) | 存放位置 |
| Remove containers (compose down) | 移除容器 |
| pane | 面板 |
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
