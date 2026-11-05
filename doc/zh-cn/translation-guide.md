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
| fixture | fixture |
| adapter | 适配层 |
| verified | 已验证 |
