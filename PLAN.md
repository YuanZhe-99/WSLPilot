# PLAN：WSLPilot — 剩余工作

> 更新日期：2026-11-05（0.1.0 发布前）
> 本文只放**尚未完成的工作与待决定事项**。已实现的行为、格式与契约都在 `doc/en-us/`，这里不再重复。全部完成后删除本文件。

## 0. 已决定（2026-11-05）

产品名 WSLPilot；许可证 GPL-3.0；首个版本 0.1.0；图标定稿；wpbk 加密为可选项（已实现）；Volume 辅助镜像默认 `alpine:3.20`、
可在设置中更换（已实现）；自动更新密钥已生成（私钥在 GitHub secret 与 MySeriesData/WSLPilotKeys.txt，公钥在 `mygo.json`）。

## 1. 待实机验证

| 项目 | 说明 |
|---|---|
| `wsl --export --format vhd` | 一次中断的导出后报 `ERROR_SHARING_VIOLATION`；需在 `wsl --shutdown` 后重测（会停止所有发行版） |
| 终端插件、托盘、通知、计划任务实际触发、更新窗口 | 需要人工在原生窗口中操作一次；更新检查要等 0.1.0 发布后才有对象 |
| Windows 11 x64 真机 | CI 已在 x64 上测试与打包；安装、升级尚未在真机验证 |
| 多 WSLc session 的对象可见性 | WSLPilot 固定使用默认 CLI session |
| 已设为 sparse 的 VHDX 再压缩的效果 | 对照 `--compact` 前后大小 |

## 2. 代码签名（Authenticode）

目前 exe 与安装包未签名：首次运行时 Windows SmartScreen 会提示“未知发布者”，需要用户点“仍要运行”。消除提示需要用可信 CA 颁发的
代码签名证书签名（OV/EV 证书，或 Azure Trusted Signing 之类的云签名服务）；自签名证书不被 SmartScreen 信任，没有意义。
**待用户决定**是否购买证书或使用云签名服务；确定后在 `release.yml` 中加入签名步骤（证书或凭据放 GitHub secret）。
自动更新的完整性已由 MyGo 的 Ed25519 签名保证，不依赖 Authenticode。

## 3. 后续功能

- wpbk 仓库在界面中修改密码（`wpbk.Repo.ChangePassword` 已实现，界面入口未做）。
- M8 原生 Compose：上游发布 `wsl compose` / `wslc compose` 后实现 `compose.Native`，并提供确认后才执行、失败回滚的“迁移 Backend”向导
  （`compose.Migrate` 已实现）。在此之前项目始终使用 builtin。
